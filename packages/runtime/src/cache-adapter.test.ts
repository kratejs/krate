// @vitest-environment node
import { AsyncLocalStorage } from 'node:async_hooks';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { CacheGuard, MemoryAdapter, SharedDataCache, cacheSpecFromConfig, cacheSpecFromEnv, installCacheAdapter, loadCacheAdapter, type CacheAdapter } from './cache-adapter';
import { RedisAdapter } from './cache-redis';
import { SharedIsrStore, isrStoreFor } from './shared-isr';
import { createSSRCore, MemoryIsrStore, type ManifestPage } from './ssr-core';
import { cacheRuntime, installRenderStore, markNoStore, patchFetch, type CacheScope } from './cache-scope';
import { parseRedisURL } from './resp';
import { startFakeRedis, type FakeRedis } from './test-redis';
import { jsx } from './server';

installRenderStore(new AsyncLocalStorage<CacheScope>());

let redis: FakeRedis;
beforeEach(async () => {
  redis = await startFakeRedis();
  vi.spyOn(console, 'warn').mockImplementation(() => {});
  vi.spyOn(console, 'error').mockImplementation(() => {});
});
afterEach(async () => {
  vi.restoreAllMocks();
  await redis.kill();
  cacheRuntime().shared = undefined;
});

const url = () => `redis://127.0.0.1:${redis.port}/0`;
const adapter = () => new RedisAdapter({ url: url(), timeoutMs: 200 });

describe('parseRedisURL', () => {
  it('reads host, port, credentials, db and TLS', () => {
    expect(parseRedisURL('redis://u:p%40ss@cache.example:6380/2')).toMatchObject({ host: 'cache.example', port: 6380, username: 'u', password: 'p@ss', db: 2, tls: false });
    expect(parseRedisURL('rediss://cache.example')).toMatchObject({ port: 6379, tls: true });
    expect(() => parseRedisURL('http://x')).toThrow(/protocol/);
  });
});

describe('RedisAdapter', () => {
  it('stores values with a ttl and tags, and deletes by tag', async () => {
    const a = adapter();
    await a.set('one', '1', { ttlSeconds: 30, tags: ['t:a', 't:b'] });
    await a.set('two', '2', { tags: ['t:a'] });
    expect(await a.get('one')).toBe('1');
    expect(await a.get('missing')).toBeNull();
    expect((await a.keysByTag('t:a')).sort()).toEqual(['one', 'two']);
    expect(await a.deleteByTags(['t:a'])).toBe(2);
    expect(await a.get('one')).toBeNull();
    expect(await a.get('two')).toBeNull();
    expect(redis.commands.some((c) => c[0] === 'SET' && c[3] === 'EX' && c[4] === '30')).toBe(true);
    await a.close();
  });

  it('keeps apps apart with a prefix', async () => {
    const a = new RedisAdapter({ url: url(), prefix: 'one:' });
    const b = new RedisAdapter({ url: url(), prefix: 'two:' });
    await a.set('k', 'A');
    expect(await b.get('k')).toBeNull();
    expect(redis.keys()).toEqual(['one:k:k']);
    await a.close();
    await b.close();
  });

  it('authenticates', async () => {
    const guarded = await startFakeRedis({ password: 's3cret' });
    try {
      const ok = new RedisAdapter({ url: `redis://:s3cret@127.0.0.1:${guarded.port}` });
      await ok.set('k', 'v');
      expect(await ok.get('k')).toBe('v');
      const bad = new RedisAdapter({ url: `redis://:nope@127.0.0.1:${guarded.port}` });
      await expect(bad.get('k')).rejects.toThrow(/WRONGPASS/);
      await ok.close();
      await bad.close();
    } finally {
      await guarded.kill();
    }
  });

  it('delivers published messages to subscribers on other connections', async () => {
    const a = adapter();
    const b = adapter();
    const got: string[] = [];
    const stop = await b.subscribe('invalidate', (m) => got.push(m));
    await a.publish('invalidate', '{"tags":["x"]}');
    await vi.waitFor(() => expect(got).toEqual(['{"tags":["x"]}']));
    stop();
    await a.close();
    await b.close();
  });

  it('tells a subscriber to flush everything after its connection was lost', async () => {
    const a = adapter();
    const got: string[] = [];
    await a.subscribe('invalidate', (m) => got.push(m));
    await redis.kill();
    await redis.revive();
    await vi.waitFor(() => expect(got).toContain('*'), { timeout: 4000 });
    await a.close();
  });
});

describe('SharedIsrStore across instances', () => {
  const entry = (html: string, tags?: string[]) => ({ html, timestamp: Date.now(), tags });

  it('serves what another instance stored and invalidates for both', async () => {
    const a = new SharedIsrStore(new CacheGuard(adapter()));
    const b = new SharedIsrStore(new CacheGuard(adapter()));
    await a.set('/p/[id]?id=1', entry('<p>1</p>', ['posts']));
    await a.set('/p/[id]?id=2', entry('<p>2</p>', ['users']));
    expect((await b.get('/p/[id]?id=1'))?.html).toBe('<p>1</p>');
    expect(await b.deleteTags(['posts'])).toBeGreaterThan(0);
    expect(await a.get('/p/[id]?id=1')).toBeUndefined();
    expect((await a.get('/p/[id]?id=2'))?.html).toBe('<p>2</p>');
    expect(await b.deleteRoute('/p/[id]')).toBeGreaterThan(0);
    expect(await a.get('/p/[id]?id=2')).toBeUndefined();
  });

  it('lists the variants of a route and deletes one params variant', async () => {
    const s = new SharedIsrStore(new CacheGuard(adapter()));
    await s.set('/p/[id]?id=1', entry('1'));
    await s.set('/p/[id]?id=1#q=a', entry('1a'));
    await s.set('/p/[id]?id=2', entry('2'));
    expect((await s.entriesForRoute('/p/[id]')).map(([k]) => k).sort()).toEqual(['/p/[id]?id=1', '/p/[id]?id=1#q=a', '/p/[id]?id=2']);
    await s.deleteVariant('/p/[id]', { id: '1' });
    expect((await s.entriesForRoute('/p/[id]')).map(([k]) => k)).toEqual(['/p/[id]?id=2']);
  });
});

describe('a cache that fails never fails a request', () => {
  const page = (route: string, extra: Partial<ManifestPage> = {}): ManifestPage => ({ route, source: route + '.tsx', mode: 'isr', bundlePath: route, revalidate: 60, ...extra } as ManifestPage);
  const core = (store: SharedIsrStore, render: () => string) =>
    createSSRCore({
      manifest: { pages: [page('/p')], regions: {} },
      isrStore: store,
      loadPageModule: async () => ({ default: () => jsx('p', { children: render() }) }),
      loadRuntimeRenderer: () => () => '',
    });

  it('renders when Redis is down, falls back to process memory, and recovers', async () => {
    let n = 0;
    const guard = new CacheGuard(adapter(), 100, 2, 300);
    const c = core(new SharedIsrStore(guard), () => `render ${++n}`);
    const req = { route: '/p', url: '/p', method: 'GET', headers: {} } as any;
    expect((await c.renderPage(req)).cacheStatus).toBe('miss');
    expect((await c.renderPage(req)).cacheStatus).toBe('hit');
    await redis.kill();
    // Down: the page still renders (and is cached in memory for this instance).
    const down = await c.renderPage(req);
    expect(down.status).toBe(200);
    expect(down.html).toContain('<p>');
    for (let i = 0; i < 5; i++) expect((await c.renderPage(req)).status).toBe(200);
    expect(guard.healthy).toBe(false);
    await redis.revive();
    await vi.waitFor(async () => expect((await c.renderPage(req)).status).toBe(200));
    await new Promise((r) => setTimeout(r, 350));
    expect((await c.renderPage(req)).status).toBe(200);
    expect(guard.healthy).toBe(true);
  });

  it('gives up on a cache that hangs after the timeout instead of waiting', async () => {
    const never = <T>() => new Promise<T>(() => {});
    const hang: CacheAdapter = {
      name: 'hang',
      get: () => never<string | null>(),
      set: () => never<void>(),
      delete: () => never<void>(),
      keysByTag: () => never<string[]>(),
      deleteByTags: () => never<number>(),
      publish: () => never<void>(),
      subscribe: () => never<() => void>(),
      close: async () => {},
    };
    const c = core(new SharedIsrStore(new CacheGuard(hang, 50)), () => 'x');
    const started = Date.now();
    const r = await c.renderPage({ route: '/p', url: '/p', method: 'GET', headers: {} } as any);
    expect(r.status).toBe(200);
    expect(Date.now() - started).toBeLessThan(1500);
  });

  it('never stores a render that read per-user state', async () => {
    const store = new SharedIsrStore(new CacheGuard(adapter()));
    const c = createSSRCore({
      manifest: { pages: [page('/me')], regions: {} },
      isrStore: store,
      loadPageModule: async () => ({
        default: () => {
          markNoStore(); // cookies(), headers() and no-store fetches do this
          return jsx('p', { children: 'Hello Ada' });
        },
      }),
      loadRuntimeRenderer: () => () => '',
    });
    const r = await c.renderPage({ route: '/me', url: '/me', method: 'GET', headers: { cookie: 'sid=ada' } } as any);
    expect(r.noStore).toBe(true);
    expect(redis.keys().filter((k) => k.includes('isr:'))).toEqual([]);
    expect(await store.get('/me')).toBeUndefined();
  });
});

describe('the shared data cache', () => {
  it('shares fetch responses between instances and invalidates them by tag', async () => {
    const shared = new SharedDataCache(new CacheGuard(adapter()));
    cacheRuntime().shared = shared;
    let upstream = 0;
    vi.stubGlobal('fetch', vi.fn(async () => new Response(`body-${++upstream}`)));
    cacheRuntime().fetchPatched = false;
    patchFetch();
    const read = async () => (await fetch('http://data/x', { next: { tags: ['x'], revalidate: 60 } } as RequestInit)).text();
    expect(await read()).toBe('body-1');
    // Another instance (a fresh data store on the same Redis) sees the entry.
    const other = new SharedDataCache(new CacheGuard(adapter()));
    const hit = await other.get('fetch:' + JSON.stringify(['GET', 'http://data/x', []]));
    expect(hit.ok && hit.value).toBeTruthy();
    expect(await read()).toBe('body-1');
    expect(await other.deleteTags(['x'])).toBeGreaterThan(0);
    expect(await read()).toBe('body-2');
    vi.unstubAllGlobals();
    cacheRuntime().fetchPatched = false;
  });
});

describe('configuration', () => {
  it('loads the default (no adapter), redis, and a custom module', async () => {
    expect(await loadCacheAdapter(undefined)).toBeNull();
    expect(await loadCacheAdapter({ adapter: 'memory' })).toBeNull();
    const r = await loadCacheAdapter({ adapter: 'redis', url: url() });
    expect(r?.name).toBe('redis');
    await r?.close();
    await expect(loadCacheAdapter({ adapter: 'redis' })).rejects.toThrow(/cache\.url/);
  });

  it('reads the spec from KRATE_CACHE and from the bundled config', () => {
    expect(cacheSpecFromEnv({ KRATE_CACHE: '{"adapter":"redis","url":"redis://h"}' })).toEqual({ adapter: 'redis', url: 'redis://h' });
    expect(cacheSpecFromEnv({ KRATE_CACHE: 'not json' })).toBeUndefined();
    expect(cacheSpecFromEnv({})).toBeUndefined();
    const saved = process.env.MY_REDIS;
    process.env.MY_REDIS = 'redis://from-env';
    expect(cacheSpecFromConfig({ adapter: 'redis', urlEnv: 'MY_REDIS' })?.url).toBe('redis://from-env');
    expect(cacheSpecFromConfig({ adapter: 'memory' })).toBeUndefined();
    if (saved === undefined) delete process.env.MY_REDIS;
    else process.env.MY_REDIS = saved;
  });

  it('keeps the fallback store for a spec that fails to start', async () => {
    const fallback = new MemoryIsrStore(4);
    const setup = isrStoreFor({ adapter: 'redis' }, fallback);
    expect(await setup.ready).toBeNull();
    await setup.store.set('/a', { html: 'a', timestamp: 1 });
    expect((await setup.store.get('/a'))?.html).toBe('a');
  });

  it('accepts an adapter instance (serverless targets plug in their own)', async () => {
    const mem = new MemoryAdapter();
    const setup = isrStoreFor(mem, new MemoryIsrStore(4));
    expect(await setup.ready).toBe(mem);
    await setup.store.set('/a', { html: 'a', timestamp: 1, tags: ['t'] });
    expect(await mem.get('isr:/a')).toContain('"html":"a"');
    expect(await setup.store.deleteTags(['t'])).toBeGreaterThan(0);
    expect(await mem.get('isr:/a')).toBeNull();
    installCacheAdapter(mem);
    expect(cacheRuntime().shared).toBeTruthy();
  });
});
