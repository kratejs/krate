// @vitest-environment node
import { AsyncLocalStorage } from 'node:async_hooks';
import { afterEach, describe, expect, it } from 'vitest';
import { cacheRuntime, installRenderStore, withCacheScope, type CacheScope } from './cache-scope';
import { krateHintTags, preload } from './hints';

const g = globalThis as any;

afterEach(() => {
  delete g.__krate_island_hints;
  cacheRuntime().renderStore = undefined;
});

describe('resource hints on the server', () => {
  it('puts each tag once into the head of the response', async () => {
    installRenderStore(new AsyncLocalStorage<CacheScope>());
    const { result } = await withCacheScope('/p', async () => {
      preload('/x.wasm', { as: 'fetch', crossOrigin: 'anonymous' });
      preload('/x.wasm', { as: 'fetch', crossOrigin: 'anonymous' });
      krateHintTags(['<link rel="preload" href="/x.wasm" as="fetch" crossorigin="">', '<link rel="preconnect" href="https://a.example">']);
      return '<main></main>';
    });
    expect(result).toBe(
      '<!--krate-head--><link rel="preload" href="/x.wasm" as="fetch" crossorigin=""><link rel="preconnect" href="https://a.example"><!--/krate-head--><main></main>',
    );
  });

  it('records into the build collector outside a request', () => {
    g.__krate_island_hints = new Set<string>();
    preload('/x.wasm', { as: 'fetch', crossOrigin: '' });
    preload('/x.wasm', { as: 'fetch', crossOrigin: '' });
    expect([...g.__krate_island_hints]).toEqual(['<link rel="preload" href="/x.wasm" as="fetch" crossorigin="">']);
  });

  it('drops hints with nowhere to go', () => {
    expect(() => preload('/x.wasm', { as: 'fetch' })).not.toThrow();
  });
});
