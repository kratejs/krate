import { AsyncLocalStorage } from 'node:async_hooks';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  DataCache,
  cacheRuntime,
  currentRenderScope,
  installRenderStore,
  patchFetch,
  withCacheScope,
  type CacheScope,
  type RevalidateScope,
} from './cache-scope.js';
import { revalidatePath, revalidateTag, unstable_cache, unstable_noStore } from './cache.js';

const RUNTIME_KEY = Symbol.for('krate.cache.runtime');
const REVALIDATE_KEY = Symbol.for('krate.revalidate.store');

function resetRuntime() {
  delete (globalThis as Record<symbol, unknown>)[RUNTIME_KEY];
  delete (globalThis as Record<symbol, unknown>)[REVALIDATE_KEY];
  installRenderStore(new AsyncLocalStorage<CacheScope>());
}

beforeEach(() => {
  resetRuntime();
  vi.unstubAllEnvs();
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  delete (globalThis as Record<string, unknown>)._go_revalidate;
});

describe('DataCache', () => {
  it('indexes entries by tag and expires them', () => {
    vi.useFakeTimers();
    try {
      const c = new DataCache(10);
      c.set('a', 1, 10, ['t1']);
      c.set('b', 2, Infinity, ['t1', 't2']);
      c.set('c', 3, 1, []);
      expect(c.get('a')).toBe(1);
      vi.advanceTimersByTime(1500);
      expect(c.get('c')).toBeUndefined();
      expect(c.invalidateTags(['t2'])).toBe(1);
      expect(c.get('b')).toBeUndefined();
      expect(c.get('a')).toBe(1);
      expect(c.invalidateTags(['t1'])).toBe(1);
      expect(c.size).toBe(0);
    } finally {
      vi.useRealTimers();
    }
  });

  it('evicts the oldest entry when full', () => {
    const c = new DataCache(2);
    c.set('a', 1, 10, ['x']);
    c.set('b', 2, 10, []);
    c.set('c', 3, 10, []);
    expect(c.get('a')).toBeUndefined();
    expect(c.invalidateTags(['x'])).toBe(0);
    expect(c.size).toBe(2);
  });
});

describe('withCacheScope', () => {
  it('records tags and the smallest revalidate window across awaits', async () => {
    const cached = unstable_cache(async (n: number) => n * 2, ['double'], { tags: ['math'], revalidate: 30 });
    const other = unstable_cache(async () => 'x', ['other'], { tags: ['letters'], revalidate: 10 });
    const { result, scope, invalidated } = await withCacheScope('/p', async () => {
      const a = await cached(2);
      await new Promise((r) => setTimeout(r, 1));
      const b = await other();
      expect(currentRenderScope()?.route).toBe('/p');
      return `${a}${b}`;
    });
    expect(result).toBe('4x');
    expect([...scope.tags].sort()).toEqual(['letters', 'math']);
    expect(scope.revalidate).toBe(10);
    expect(scope.noStore).toBe(false);
    expect(invalidated).toBe(false);
    expect(currentRenderScope()).toBeUndefined();
  });

  it('keeps concurrent scopes apart', async () => {
    const tagged = (tag: string) => unstable_cache(async () => tag, [tag], { tags: [tag] });
    const [a, b] = await Promise.all([
      withCacheScope('/a', async () => {
        await new Promise((r) => setTimeout(r, 2));
        return tagged('a')();
      }),
      withCacheScope('/b', () => tagged('b')()),
    ]);
    expect([...a.scope.tags]).toEqual(['a']);
    expect([...b.scope.tags]).toEqual(['b']);
  });

  it('marks unstable_noStore renders and invalidations that land mid-render', async () => {
    const r1 = await withCacheScope('/n', () => {
      unstable_noStore();
      return 1;
    });
    expect(r1.scope.noStore).toBe(true);
    const r2 = await withCacheScope('/i', async () => {
      cacheRuntime().invalidate({ tags: ['z'] });
      return 2;
    });
    expect(r2.invalidated).toBe(true);
  });
});

describe('unstable_cache', () => {
  it('caches by key parts and arguments until the tag is revalidated', async () => {
    let calls = 0;
    const getPost = unstable_cache(async (id: string) => ({ id, n: ++calls }), ['post'], { tags: ['posts'] });
    expect(await getPost('a')).toEqual({ id: 'a', n: 1 });
    expect(await getPost('a')).toEqual({ id: 'a', n: 1 });
    expect(await getPost('b')).toEqual({ id: 'b', n: 2 });

    const scope: RevalidateScope = { kind: 'action', paths: [], tags: [] };
    const store = new AsyncLocalStorage<RevalidateScope>();
    (globalThis as Record<symbol, unknown>)[REVALIDATE_KEY] = store;
    store.run(scope, () => revalidateTag('posts'));
    expect(scope.tags).toEqual(['posts']);
    expect(await getPost('a')).toEqual({ id: 'a', n: 3 });
  });

  it('dedupes concurrent misses', async () => {
    let calls = 0;
    const slow = unstable_cache(async () => {
      calls++;
      await new Promise((r) => setTimeout(r, 2));
      return calls;
    }, ['slow']);
    const [a, b] = await Promise.all([slow(), slow()]);
    expect(a).toBe(1);
    expect(b).toBe(1);
    expect(calls).toBe(1);
  });

  it('does not cache with revalidate 0 and rejects non-JSON results', async () => {
    let calls = 0;
    const fresh = unstable_cache(async () => ++calls, ['fresh'], { revalidate: 0 });
    await fresh();
    await fresh();
    expect(calls).toBe(2);
    const bad = unstable_cache(async () => undefined, ['bad']);
    await expect(bad()).rejects.toThrow(/JSON-serializable/);
    expect(() => unstable_cache(async () => 1, [], { revalidate: -1 })).toThrow(/revalidate/);
  });
});

describe('revalidatePath / revalidateTag', () => {
  it('queue on the request scope of an action or API route', () => {
    const scope: RevalidateScope = { kind: 'action', paths: [], tags: [] };
    const store = new AsyncLocalStorage<RevalidateScope>();
    (globalThis as Record<symbol, unknown>)[REVALIDATE_KEY] = store;
    store.run(scope, () => {
      revalidatePath('/blog/[slug]');
      revalidatePath('/blog', 'layout');
      revalidateTag('posts');
    });
    expect(scope.paths).toEqual([
      { path: '/blog/[slug]', type: 'page' },
      { path: '/blog', type: 'layout' },
    ]);
    expect(scope.tags).toEqual(['posts']);
  });

  it('throw while a page renders', async () => {
    await expect(withCacheScope('/p', () => revalidateTag('x'))).rejects.toThrow(/while a page renders/);
  });

  it('throw outside the Krate server instead of doing nothing', () => {
    expect(() => revalidatePath('/x')).toThrow(/KRATE_HOST_URL is not set/);
    expect(() => revalidateTag('x')).toThrow(/KRATE_HOST_URL is not set/);
  });

  it('validate their arguments', () => {
    expect(() => revalidatePath('relative')).toThrow(TypeError);
    expect(() => revalidatePath('/x', 'tree' as never)).toThrow(TypeError);
    expect(() => revalidateTag('')).toThrow(TypeError);
  });

  it('call the Go host synchronously in the embedded runtime', () => {
    const calls: string[] = [];
    (globalThis as Record<string, unknown>)._go_revalidate = (json: string) => {
      calls.push(json);
      return JSON.stringify({ revalidated: [], skipped: [{ target: '/about', reason: 'static page' }] });
    };
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    try {
      revalidatePath('/about');
      expect(JSON.parse(calls[0])).toEqual({ paths: [{ path: '/about', type: 'page' }], tags: [] });
      expect(warn).toHaveBeenCalledWith(expect.stringContaining('/about: static page'));
    } finally {
      warn.mockRestore();
    }
    (globalThis as Record<string, unknown>)._go_revalidate = () => JSON.stringify({ error: 'boom' });
    expect(() => revalidateTag('t')).toThrow(/boom/);
  });

  it('post to the host outside a request scope when the host is configured', async () => {
    vi.stubEnv('KRATE_HOST_URL', 'http://127.0.0.1:9');
    vi.stubEnv('KRATE_INTERNAL_SECRET', 's3cret');
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ revalidated: [], skipped: [] }), { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);
    revalidateTag('posts');
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('http://127.0.0.1:9/__krate/internal/revalidate');
    expect((init.headers as Record<string, string>)['x-krate-internal']).toBe('s3cret');
    expect(JSON.parse(String(init.body))).toEqual({ paths: [], tags: ['posts'] });
  });
});

describe('patchFetch', () => {
  it('caches tagged GET responses, records tags, and bypasses no-store', async () => {
    let n = 0;
    const base = vi.fn(async () => new Response(`body-${++n}`, { status: 200, headers: { 'x-n': String(n) } }));
    vi.stubGlobal('fetch', base);
    cacheRuntime().fetchPatched = false;
    patchFetch();

    const read = () => fetch('http://data/posts', { next: { tags: ['posts'], revalidate: 60 } } as RequestInit);
    const { result, scope } = await withCacheScope('/p', async () => (await read()).text());
    expect(result).toBe('body-1');
    expect([...scope.tags]).toEqual(['posts']);
    expect(scope.revalidate).toBe(60);
    expect(await (await read()).text()).toBe('body-1');
    expect(base).toHaveBeenCalledTimes(1);

    cacheRuntime().invalidate({ tags: ['posts'] });
    expect(await (await read()).text()).toBe('body-2');

    const ns = await withCacheScope('/q', async () =>
      (await fetch('http://data/posts', { cache: 'no-store' })).text(),
    );
    expect(ns.result).toBe('body-3');
    expect(ns.scope.noStore).toBe(true);

    // Plain requests and non-GET requests are passed through uncached.
    await fetch('http://data/posts');
    await fetch('http://data/posts', { method: 'POST', next: { revalidate: 60 } } as RequestInit);
    expect(base).toHaveBeenCalledTimes(5);
    const plainInit = (base.mock.calls[4] as unknown as [string, Record<string, unknown>])[1];
    expect(plainInit.next).toBeUndefined();
  });
});
