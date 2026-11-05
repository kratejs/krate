// The ISR page cache on a shared cache adapter (see cache-adapter.ts).

import { MemoryIsrStore, variantKey, type CacheEntry, type IsrStore } from './ssr-core.js';
import { installCacheAdapter, loadCacheAdapter, tagName, type CacheAdapter, type CacheGuard, type CacheSpec } from './cache-adapter.js';

const ISR_PREFIX = 'isr:';

/**
 * The ISR page cache on a shared adapter. Entries live in the adapter, so every
 * instance serves and invalidates the same pages. When the adapter is down the
 * store falls back to process memory, and the page is rendered rather than failed.
 */
export class SharedIsrStore implements IsrStore {
  private readonly memory: MemoryIsrStore;

  constructor(
    private readonly guard: CacheGuard,
    private readonly ttlSeconds = 7 * 24 * 3600,
    maxMemory = 512,
  ) {
    this.memory = new MemoryIsrStore(maxMemory);
  }

  async get(key: string): Promise<CacheEntry | undefined> {
    const r = await this.guard.call((a) => a.get(ISR_PREFIX + key));
    if (r.ok) {
      if (r.value === null) return undefined;
      try {
        return JSON.parse(r.value) as CacheEntry;
      } catch {
        return undefined;
      }
    }
    return this.memory.get(key);
  }

  async set(key: string, entry: CacheEntry): Promise<void> {
    const route = routeOfKey(key);
    const tags = (entry.tags || []).map(tagName).concat(routeTag(route));
    const r = await this.guard.call((a) => a.set(ISR_PREFIX + key, JSON.stringify(entry), { ttlSeconds: this.ttlSeconds, tags }));
    // Without the shared store this instance still caches for itself.
    if (!r.ok) this.memory.set(key, entry);
  }

  async deleteRoute(route: string): Promise<number> {
    let n = this.memory.deleteRoute(route);
    const r = await this.guard.call((a) => a.deleteByTags([routeTag(route)]));
    if (r.ok) n += r.value;
    return n;
  }

  async deleteVariant(route: string, params: Record<string, string>): Promise<number> {
    let n = this.memory.deleteVariant(route, params);
    const base = variantKey({ route, params });
    const keys = await this.guard.call((a) => a.keysByTag(routeTag(route)));
    if (keys.ok) {
      const stale = keys.value.filter((k) => {
        const bare = k.startsWith(ISR_PREFIX) ? k.slice(ISR_PREFIX.length) : k;
        return bare === base || bare.startsWith(base + '#');
      });
      for (const k of stale) {
        const d = await this.guard.call((a) => a.delete(k));
        if (d.ok) n++;
      }
    }
    return n;
  }

  async deleteTags(tags: string[]): Promise<number> {
    let n = this.memory.deleteTags(tags);
    const r = await this.guard.call((a) => a.deleteByTags(tags.map(tagName)));
    if (r.ok) n += r.value;
    return n;
  }

  async entriesForRoute(route: string): Promise<Array<[string, CacheEntry]>> {
    const keys = await this.guard.call((a) => a.keysByTag(routeTag(route)));
    if (!keys.ok) return this.memory.entriesForRoute(route);
    const out: Array<[string, CacheEntry]> = [];
    for (const k of keys.value) {
      if (!k.startsWith(ISR_PREFIX)) continue;
      const r = await this.guard.call((a) => a.get(k));
      if (!r.ok || r.value === null) continue;
      try {
        out.push([k.slice(ISR_PREFIX.length), JSON.parse(r.value) as CacheEntry]);
      } catch {
        /* skip an unreadable entry */
      }
    }
    return out;
  }
}

function routeOfKey(key: string): string {
  return key.split('?')[0].split('#')[0];
}

function routeTag(route: string): string {
  return 'route:' + route;
}


/** Where an ISR store came from: the shared cache once its adapter is up. */
export interface IsrStoreSetup {
  store: IsrStore;
  /** Resolves with the adapter once it started (null when none is configured or it failed). */
  ready: Promise<CacheAdapter | null>;
}

/**
 * The ISR store for a cache spec: `fallback` (process memory) without an
 * adapter or while it starts; the shared store after. A spec that fails to load
 * logs and keeps `fallback`, so a bad cache setting never takes the site down.
 */
export function isrStoreFor(specOrAdapter: CacheSpec | CacheAdapter | Promise<CacheAdapter> | undefined, fallback: IsrStore): IsrStoreSetup {
  const promised = specOrAdapter && typeof (specOrAdapter as Promise<CacheAdapter>).then === 'function' ? (specOrAdapter as Promise<CacheAdapter>) : undefined;
  if (promised) {
    let active: IsrStore = fallback;
    const ready = promised.then(
      (adapter) => {
        const installed = installCacheAdapter(adapter, {});
        active = new SharedIsrStore(installed.guard);
        return adapter;
      },
      (err) => {
        console.error('[krate] the cache adapter failed to start; using the in-process caches:', err);
        return null;
      },
    );
    return { store: delegating(() => active, ready), ready };
  }
  const given = specOrAdapter && typeof (specOrAdapter as CacheAdapter).get === 'function' ? (specOrAdapter as CacheAdapter) : undefined;
  const spec = given ? ({} as CacheSpec) : (specOrAdapter as CacheSpec | undefined);
  if (!given && (!spec || !spec.adapter || spec.adapter === 'memory')) return { store: fallback, ready: Promise.resolve(null) };
  let active: IsrStore = fallback;
  const load = given ? Promise.resolve(given) : loadCacheAdapter(spec);
  const ready = load.then(
    (adapter) => {
      if (!adapter) return null;
      const installed = installCacheAdapter(adapter, spec || {});
      active = new SharedIsrStore(installed.guard, spec?.ttlSeconds, 512);
      return adapter;
    },
    (err) => {
      console.error('[krate] the cache adapter failed to start; using the in-process caches:', err);
      return null;
    },
  );
  return { store: delegating(() => active, ready), ready };
}

function delegating(current: () => IsrStore, ready: Promise<unknown>): IsrStore {
  return {
    get: async (key) => (await ready, current().get(key)),
    set: async (key, entry) => (await ready, current().set(key, entry)),
    deleteRoute: async (route) => (await ready, current().deleteRoute(route)),
    deleteVariant: async (route, params) => (await ready, current().deleteVariant(route, params)),
    deleteTags: async (tags) => (await ready, current().deleteTags(tags)),
    entriesForRoute: async (route) => (await ready, current().entriesForRoute(route)),
  };
}
