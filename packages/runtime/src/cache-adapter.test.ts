// @vitest-environment node
import { AsyncLocalStorage } from 'node:async_hooks';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { CacheGuard } from './cache-adapter';
import { RedisAdapter } from './cache-redis';
import { SharedIsrStore } from './shared-isr';
import { createSSRCore, type ManifestPage } from './ssr-core';
import { cacheRuntime, installRenderStore, markNoStore, type CacheScope } from './cache-scope';
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

describe('a cache that fails never fails a request', () => {
  const page = (route: string, extra: Partial<ManifestPage> = {}): ManifestPage => ({ route, source: route + '.tsx', mode: 'isr', bundlePath: route, revalidate: 60, ...extra } as ManifestPage);

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
