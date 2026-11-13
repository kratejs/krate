import { AsyncLocalStorage } from 'node:async_hooks';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  cacheRuntime,
  installRenderStore,
  patchFetch,
  withCacheScope,
  type CacheScope,
} from './cache-scope.js';

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

describe('patchFetch', () => {
  it('never replays Set-Cookie from a cached response', async () => {
    const base = vi.fn(async () => new Response('body', {
      status: 200,
      headers: { 'set-cookie': 'sid=secret', 'x-test': 'yes' },
    }));
    vi.stubGlobal('fetch', base);
    cacheRuntime().fetchPatched = false;
    patchFetch();

    const read = () => fetch('http://data/session', { next: { tags: ['session'], revalidate: 60 } } as RequestInit);
    const first = await withCacheScope('/p', async () => read());
    expect(first.result.headers.get('x-test')).toBe('yes');

    const second = await withCacheScope('/p', async () => read());
    expect(second.result.headers.get('set-cookie')).toBeNull();
    expect(second.result.headers.get('x-test')).toBe('yes');
    expect(base).toHaveBeenCalledTimes(1);
  });
});
