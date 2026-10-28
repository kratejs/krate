/**
 * Server data cache and render cache scopes.
 *
 * This module holds the process-wide state behind `@krate/runtime/cache`:
 *
 * - The render scope: the SSR renderer runs every page and region render
 *   inside `withCacheScope`, which records the cache tags, the smallest
 *   revalidate window and any no-store read the render made. The renderer
 *   stores the tags on its ISR/region cache entry so `revalidateTag` can find
 *   the entries a tag fed.
 * - The data cache: `unstable_cache` results and `fetch` responses cached via
 *   `{ next: { revalidate, tags } }` or `cache: "force-cache"`, keyed by the
 *   call, expired by revalidate seconds and indexed by tag.
 * - The `fetch` wrapper that applies those options.
 *
 * State lives on `globalThis` under `Symbol.for` keys, so the copy bundled
 * into the renderer and the copy a page bundle imports share it. The module
 * has no Node imports: the embedded QuickJS API runtime bundles it too, and
 * there the data cache is backed by the Go host (`_go_cache_get/_go_cache_set`).
 */

/** What one render read through the cache. */
export interface CacheScope {
  /** Route being rendered (diagnostics only). */
  route: string;
  /** Cache tags the render read. */
  tags: Set<string>;
  /** Smallest positive revalidate window (seconds) a read declared; 0 = none. */
  revalidate: number;
  /** True when a read opted out of caching (no-store, revalidate 0, unstable_noStore). */
  noStore: boolean;
}

/** Minimal AsyncLocalStorage shape (the Node class satisfies it). */
export interface AsyncStore<T> {
  getStore(): T | undefined;
  run<R>(store: T, fn: () => R): R;
}

/** Pending revalidations of one server action or API request. */
export interface RevalidateScope {
  kind: string;
  paths: Array<{ path: string; type: 'page' | 'layout' }>;
  tags: string[];
}

interface DataEntry {
  value: unknown;
  expires: number;
  tags: string[];
}

interface GoCacheBridge {
  _go_cache_get?: (key: string) => string;
  _go_cache_set?: (key: string, value: string, ttlSeconds: number, tagsJSON: string) => void;
}

const STATE_KEY = Symbol.for('krate.cache.runtime');
const REVALIDATE_STORE_KEY = Symbol.for('krate.revalidate.store');

/** In-process data cache with tag index and FIFO bound. */
export class DataCache {
  private map = new Map<string, DataEntry>();
  private byTag = new Map<string, Set<string>>();
  constructor(private max = 1000) {}

  get size(): number {
    return this.map.size;
  }

  get(key: string): unknown | undefined {
    const e = this.map.get(key);
    if (!e) return undefined;
    if (e.expires <= Date.now()) {
      this.delete(key);
      return undefined;
    }
    return e.value;
  }

  set(key: string, value: unknown, ttlSeconds: number, tags: string[]): void {
    if (!(ttlSeconds > 0)) return;
    if (this.map.has(key)) this.delete(key);
    while (this.map.size >= this.max) {
      const oldest = this.map.keys().next().value;
      if (oldest === undefined) break;
      this.delete(oldest);
    }
    const expires = ttlSeconds === Infinity ? Infinity : Date.now() + ttlSeconds * 1000;
    this.map.set(key, { value, expires, tags: [...tags] });
    for (const t of tags) {
      let keys = this.byTag.get(t);
      if (!keys) this.byTag.set(t, (keys = new Set()));
      keys.add(key);
    }
  }

  delete(key: string): void {
    const e = this.map.get(key);
    if (!e) return;
    this.map.delete(key);
    for (const t of e.tags) {
      const keys = this.byTag.get(t);
      if (keys) {
        keys.delete(key);
        if (keys.size === 0) this.byTag.delete(t);
      }
    }
  }

  /** Removes every entry carrying one of tags; returns how many. */
  invalidateTags(tags: string[]): number {
    let n = 0;
    for (const t of tags) {
      const keys = this.byTag.get(t);
      if (!keys) continue;
      for (const k of [...keys]) {
        if (this.map.has(k)) n++;
        this.delete(k);
      }
    }
    return n;
  }

  clear(): void {
    this.map.clear();
    this.byTag.clear();
  }
}

/** Process-wide cache runtime shared by every bundled copy of this module. */
export interface CacheRuntime {
  dataCache: DataCache;
  /** Render scope storage; installed by the SSR renderer. */
  renderStore?: AsyncStore<CacheScope>;
  /** Bumped on every invalidation so in-flight renders/reads do not store stale results. */
  epoch: number;
  /** Applies a tag invalidation to the local data cache. */
  invalidate(payload: { tags?: string[] }): { dataEntries: number };
  fetchPatched?: boolean;
}

/** The shared cache runtime, created on first use. */
export function cacheRuntime(): CacheRuntime {
  const g = globalThis as Record<symbol, unknown>;
  let rt = g[STATE_KEY] as CacheRuntime | undefined;
  if (!rt) {
    const max = envNumber('KRATE_DATA_CACHE_MAX') || 1000;
    const created: CacheRuntime = {
      dataCache: new DataCache(max),
      epoch: 0,
      invalidate(payload) {
        created.epoch++;
        const tags = Array.isArray(payload.tags) ? payload.tags.filter((t) => typeof t === 'string') : [];
        return { dataEntries: tags.length ? created.dataCache.invalidateTags(tags) : 0 };
      },
    };
    rt = created;
    g[STATE_KEY] = rt;
  }
  return rt;
}

function envNumber(name: string): number {
  const proc = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process;
  const v = proc && proc.env ? proc.env[name] : undefined;
  const n = v ? parseInt(v, 10) : NaN;
  return Number.isFinite(n) && n > 0 ? n : 0;
}

/** Installs the render scope storage (an AsyncLocalStorage). Renderer only. */
export function installRenderStore(store: AsyncStore<CacheScope>): void {
  cacheRuntime().renderStore = store;
}

/** The scope of the render currently running, if any. */
export function currentRenderScope(): CacheScope | undefined {
  const store = cacheRuntime().renderStore;
  return store ? store.getStore() : undefined;
}

/** The pending-revalidation scope of the current server action / API request. */
export function currentRevalidateScope(): RevalidateScope | undefined {
  const store = (globalThis as Record<symbol, unknown>)[REVALIDATE_STORE_KEY] as AsyncStore<RevalidateScope> | undefined;
  return store && typeof store.getStore === 'function' ? store.getStore() : undefined;
}

/**
 * Runs fn inside a fresh render scope and returns its result together with
 * the scope (tags, revalidate window, noStore) and whether an invalidation
 * happened while it ran (then the result must not be cached).
 */
export async function withCacheScope<T>(
  route: string,
  fn: () => T | Promise<T>,
): Promise<{ result: T; scope: CacheScope; invalidated: boolean }> {
  const rt = cacheRuntime();
  const scope: CacheScope = { route, tags: new Set(), revalidate: 0, noStore: false };
  const epoch = rt.epoch;
  const store = rt.renderStore;
  const result = store ? await store.run(scope, () => Promise.resolve().then(fn)) : await fn();
  return { result, scope, invalidated: rt.epoch !== epoch };
}

/** Records tags and a revalidate window on the current render scope. */
export function recordCacheRead(tags: readonly string[] | undefined, revalidate: number | false | undefined): void {
  const scope = currentRenderScope();
  if (!scope) return;
  if (tags) for (const t of tags) if (typeof t === 'string' && t) scope.tags.add(t);
  if (revalidate === 0) scope.noStore = true;
  else if (typeof revalidate === 'number' && revalidate > 0 && (scope.revalidate === 0 || revalidate < scope.revalidate)) {
    scope.revalidate = revalidate;
  }
}

/** Marks the current render as uncacheable. Outside a render it has no effect. */
export function markNoStore(): void {
  const scope = currentRenderScope();
  if (scope) scope.noStore = true;
}

// Data cache access. In QuickJS the Go host owns the cache (shared by every
// VM worker and invalidated with the rest of the host's caches); values cross
// the bridge as JSON.

function goBridge(): Required<GoCacheBridge> | null {
  const g = globalThis as GoCacheBridge;
  return typeof g._go_cache_get === 'function' && typeof g._go_cache_set === 'function'
    ? (g as Required<GoCacheBridge>)
    : null;
}

/** Reads a JSON value from the data cache; undefined on a miss. */
export function dataCacheGetJSON(key: string): string | undefined {
  const go = goBridge();
  if (go) {
    const v = go._go_cache_get(key);
    return typeof v === 'string' && v !== '' ? v : undefined;
  }
  const v = cacheRuntime().dataCache.get(key);
  return typeof v === 'string' ? v : undefined;
}

/** Stores a JSON value unless an invalidation happened since epoch. */
export function dataCacheSetJSON(key: string, json: string, ttlSeconds: number, tags: string[], epoch: number): void {
  const go = goBridge();
  if (go) {
    go._go_cache_set(key, json, ttlSeconds === Infinity ? -1 : ttlSeconds, JSON.stringify(tags));
    return;
  }
  const rt = cacheRuntime();
  if (rt.epoch !== epoch) return;
  rt.dataCache.set(key, json, ttlSeconds, tags);
}

// fetch wrapper

interface NextFetchInit extends RequestInit {
  next?: { revalidate?: number | false; tags?: string[] };
}

interface CachedResponse {
  status: number;
  statusText: string;
  headers: Array<[string, string]>;
  body: string; // base64
}

function toBase64(bytes: Uint8Array): string {
  const B = (globalThis as { Buffer?: { from(b: Uint8Array): { toString(enc: string): string } } }).Buffer;
  if (B) return B.from(bytes).toString('base64');
  let s = '';
  for (let i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i]);
  return btoa(s);
}

function fromBase64(b64: string): Uint8Array {
  const B = (globalThis as { Buffer?: { from(s: string, enc: string): Uint8Array } }).Buffer;
  if (B) return new Uint8Array(B.from(b64, 'base64'));
  const s = atob(b64);
  const out = new Uint8Array(s.length);
  for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i);
  return out;
}

function requestURL(input: unknown): string {
  if (typeof input === 'string') return input;
  if (input && typeof (input as URL).href === 'string') return (input as URL).href;
  if (input && typeof (input as Request).url === 'string') return (input as Request).url;
  return String(input);
}

function fetchCacheKey(method: string, url: string, init: RequestInit | undefined): string {
  const headers: Array<[string, string]> = [];
  if (init && init.headers) {
    new Headers(init.headers).forEach((v, k) => headers.push([k, v]));
    headers.sort((a, b) => (a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0));
  }
  return 'fetch:' + JSON.stringify([method, url, headers]);
}

/**
 * Wraps globalThis.fetch once so Next-style cache options work in server
 * code: `next.tags` are recorded on the current render, `next.revalidate: N`
 * or `cache: "force-cache"` cache successful GET/HEAD responses (by method,
 * URL and headers) for N seconds (indefinitely for force-cache or
 * `revalidate: false`) under the given tags, and `cache: "no-store"` /
 * `next.revalidate: 0` bypass the cache and make the render uncacheable.
 * Requests without these options are passed through untouched.
 */
export function patchFetch(): void {
  const rt = cacheRuntime();
  const g = globalThis as { fetch?: typeof fetch };
  if (rt.fetchPatched || typeof g.fetch !== 'function') return;
  const original = g.fetch.bind(globalThis);
  rt.fetchPatched = true;
  const wrapped = async (input: RequestInfo | URL, init?: NextFetchInit): Promise<Response> => {
    const next = init && init.next;
    const mode = init && init.cache;
    if (!next && !mode) return original(input, init);
    const { next: _drop, ...rest } = init as NextFetchInit;
    const plainInit: RequestInit = rest;
    const tags = next && Array.isArray(next.tags) ? next.tags.filter((t) => typeof t === 'string' && t !== '') : [];
    const revalidate = next ? next.revalidate : undefined;
    recordCacheRead(tags, revalidate);
    if (mode === 'no-store' || revalidate === 0) {
      markNoStore();
      return original(input, plainInit);
    }
    let ttl = 0;
    if (typeof revalidate === 'number' && revalidate > 0) ttl = revalidate;
    else if (revalidate === false || mode === 'force-cache') ttl = Infinity;
    const method = String(plainInit.method || (input instanceof Request ? input.method : 'GET')).toUpperCase();
    if (ttl === 0 || (method !== 'GET' && method !== 'HEAD')) return original(input, plainInit);

    const key = fetchCacheKey(method, requestURL(input), plainInit);
    const hit = dataCacheGetJSON(key);
    if (hit !== undefined) {
      const c = JSON.parse(hit) as CachedResponse;
      return new Response(method === 'HEAD' ? null : (fromBase64(c.body) as BodyInit), {
        status: c.status,
        statusText: c.statusText,
        headers: c.headers,
      });
    }
    const epoch = rt.epoch;
    const res = await original(input, plainInit);
    if (res.ok) {
      const bytes = new Uint8Array(await res.clone().arrayBuffer());
      const headers: Array<[string, string]> = [];
      res.headers.forEach((v, k) => headers.push([k, v]));
      const entry: CachedResponse = { status: res.status, statusText: res.statusText, headers, body: toBase64(bytes) };
      dataCacheSetJSON(key, JSON.stringify(entry), ttl, tags, epoch);
    }
    return res;
  };
  g.fetch = wrapped as typeof fetch;
}
