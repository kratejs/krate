// Shared SSR rendering core. The Node sidecar (server-renderer.ts) and the
// portable server handler (server-handler.ts) both render pages and regions
// through it, so ISR/region caching, query tracking and render signals behave
// the same wherever Krate runs. It has no node:fs/node:vm/node:http imports:
// module loading is injected by the host.

import { AsyncLocalStorage } from "node:async_hooks";
import { jsx, renderToStringAsync } from "./server.js";
import { sha256Hex as portableSha256Hex } from "./sha256.js";
import {
  isNotFoundSignal,
  accessDeniedStatus,
  parseCookieHeader,
  redirectTarget,
  thenable,
  thenableRecord,
  type RequestContext,
} from "./request-context.js";
import { takeHead } from "./head-tags.js";
import {
  cacheRuntime,
  routeDataTag,
  withCacheScope,
  type CacheScope,
} from "./cache-scope.js";

// Types

export interface ManifestPage {
  route: string;
  source: string;
  mode: string;
  revalidate?: number;
  bundlePath?: string;
  /** The page renders async server components: a failed render is a 500,
   *  never the baked placeholder. */
  serverComponents?: boolean;
}

export interface RegionMeta {
  id: string;
  component?: string;
  sourcePath?: string;
  bundlePath?: string;
  suspense?: boolean;
  props?: Record<string, any>;
  /** Per-region revalidate seconds (PPR cache directive). 0/undefined falls
   *  back to the page's ISR cadence, or no caching for non-ISR pages. */
  revalidate?: number;
  /** Export of the page server bundle that renders this region (an async
   *  server component). Takes precedence over bundlePath. */
  serverExport?: string;
}

export interface ServerManifest {
  pages: ManifestPage[];
  stylesheet?: string;
  runtimeJS?: string;
  regions?: Record<string, RegionMeta[]>;
}

export interface CacheEntry {
  html: string;
  timestamp: number;
  // params/query record the variant this entry was generated for so a
  // time-based background refresh can regenerate it without evicting it.
  params?: Record<string, string>;
  query?: Record<string, string>;
  // tags the render read (fetch next.tags / unstable_cache tags), so
  // revalidateTag can find the entry.
  tags?: string[];
  // revalidate is the smallest window a read declared; it shortens the
  // page's ISR interval for this entry.
  revalidate?: number;
}

export interface RenderRequest {
  route: string;
  url: string;
  method: string;
  headers: Record<string, string>;
  params?: Record<string, string>;
  query?: Record<string, string>;
  // regions is the explicit splice-marker list the Go server found in the
  // baked shell, in document order. kind is "component" (default) for a
  // runtime/Suspense region or "page" for a coarse whole-page region (SSR/ISR).
  regions?: { id: string; kind?: "component" | "page" }[];
}

export type CacheStatus = "hit" | "stale" | "miss";

export interface RenderResponse {
  html: string;
  status: number;
  redirect?: string;
  notFound?: boolean;
  /** 401 or 403 when the render called unauthorized() or forbidden(). */
  denied?: 401 | 403;
  cached?: boolean;
  cacheStatus?: CacheStatus;
  error?: string;
  /** Short id of a failed render; the full error is logged with it. */
  digest?: string;
  stack?: string;
  noStore?: boolean;
}
// ISR Cache (variant-aware, SWR, persisted)

// A page's cache key is the route plus its per-request variant (params +
// query). Two dynamic-route requests (e.g. /video/a and /video/b) render
// different HTML, so they must never share a cache slot.
export function variantKey(req: { route: string; params?: Record<string, string>; query?: Record<string, string> }): string {
  const enc = (v: Record<string, string>) =>
    Object.keys(v).sort().map((k) => `${encodeURIComponent(k)}=${encodeURIComponent(v[k])}`).join("&");
  const params = req.params ? enc(req.params) : "";
  const query = req.query ? enc(req.query) : "";
  let key = req.route;
  if (params) key += "?" + params;
  if (query) key += "#" + query;
  return key;
}

// Query tracking for the page ISR cache. A page is keyed by route + params
// only; query keys join the key only when the render actually read them, so
// /isr/1?utm_source=x and a random ?zz= share the cached page unless the page
// looks at searchParams. A render that enumerates searchParams (spread,
// Object.keys, JSON) reads "everything" and is keyed by the full query.
export interface QueryTracker {
  keys: Set<string>;
  all: boolean;
}

export function newQueryTracker(): QueryTracker {
  return { keys: new Set(), all: false };
}

export function trackedQuery(query: Record<string, string> | undefined, t: QueryTracker): Record<string, string> {
  const target: Record<string, string> = { ...(query || {}) };
  const note = (prop: string | symbol) => {
    if (typeof prop === "string" && prop !== "then" && prop !== "toJSON" && prop !== "constructor") t.keys.add(prop);
  };
  return new Proxy(target, {
    get(obj, prop, recv) {
      note(prop);
      return Reflect.get(obj, prop, recv);
    },
    has(obj, prop) {
      note(prop);
      return Reflect.has(obj, prop);
    },
    ownKeys(obj) {
      t.all = true;
      return Reflect.ownKeys(obj);
    },
    getOwnPropertyDescriptor(obj, prop) {
      note(prop);
      return Reflect.getOwnPropertyDescriptor(obj, prop);
    },
  });
}

export class ISRCache {
  private cache = new Map<string, CacheEntry>();
  private byTag = new Map<string, Set<string>>();
  private maxSize: number;
  private revalidationIntervals = new Map<string, number>(); // route -> seconds

  constructor(maxSize = 512) {
    this.maxSize = maxSize;
  }

  setRevalidation(route: string, seconds: number) {
    this.revalidationIntervals.set(route, seconds);
  }

  // get marks the entry most recently used, so eviction drops the coldest
  // page and one client cannot flush the hot ones by requesting new variants.
  get(key: string): CacheEntry | undefined {
    const entry = this.cache.get(key);
    if (entry) {
      this.cache.delete(key);
      this.cache.set(key, entry);
    }
    return entry;
  }

  set(key: string, entry: CacheEntry) {
    if (this.cache.has(key)) this.delete(key);
    if (this.cache.size >= this.maxSize) {
      const oldest = this.cache.keys().next().value;
      if (oldest) this.delete(oldest);
    }
    this.cache.set(key, entry);
    for (const t of entry.tags || []) {
      let keys = this.byTag.get(t);
      if (!keys) this.byTag.set(t, (keys = new Set()));
      keys.add(key);
    }
  }

  delete(key: string): boolean {
    const entry = this.cache.get(key);
    if (!entry) return false;
    this.cache.delete(key);
    for (const t of entry.tags || []) {
      const keys = this.byTag.get(t);
      if (keys) {
        keys.delete(key);
        if (keys.size === 0) this.byTag.delete(t);
      }
    }
    return true;
  }

  get size(): number {
    return this.cache.size;
  }

  isStale(key: string): boolean {
    const entry = this.cache.get(key);
    const route = key.split("?")[0].split("#")[0];
    let interval = this.revalidationIntervals.get(route) || 0;
    if (entry && entry.revalidate && entry.revalidate > 0 && (!interval || entry.revalidate < interval)) {
      interval = entry.revalidate;
    }
    if (!entry || !interval) return false;
    return (Date.now() - entry.timestamp) / 1000 > interval;
  }

  // deleteRoute removes every variant of a route (used by revalidation/invalidation).
  deleteRoute(route: string): number {
    let n = 0;
    for (const key of [...this.cache.keys()]) {
      if (key === route || key.startsWith(route + "?") || key.startsWith(route + "#")) {
        if (this.delete(key)) n++;
      }
    }
    return n;
  }

  // deleteVariant removes one params variant of a route (every query variant
  // of it), e.g. /blog/[slug] with {slug: "a"} for revalidatePath("/blog/a").
  deleteVariant(route: string, params: Record<string, string>): number {
    const base = variantKey({ route, params });
    let n = 0;
    for (const key of [...this.cache.keys()]) {
      if (key === base || key.startsWith(base + "#")) {
        if (this.delete(key)) n++;
      }
    }
    return n;
  }

  // deleteTags removes every entry whose render read one of tags.
  deleteTags(tags: string[]): number {
    let n = 0;
    for (const t of tags) {
      const keys = this.byTag.get(t);
      if (!keys) continue;
      for (const key of [...keys]) {
        if (this.delete(key)) n++;
      }
    }
    return n;
  }

  // entriesForRoute returns a snapshot of the cached variants for a route so a
  // background refresh can iterate them without mutating during iteration.
  entriesForRoute(route: string): Array<[string, CacheEntry]> {
    const out: Array<[string, CacheEntry]> = [];
    for (const [key, entry] of this.cache) {
      if (key === route || key.startsWith(route + "?") || key.startsWith(route + "#")) {
        out.push([key, entry]);
      }
    }
    return out;
  }

  serialize(): Record<string, unknown> {
    return {
      entries: [...this.cache.entries()].map(([key, entry]) => ({ key, ...entry })),
    };
  }

  hydrate(data: any) {
    if (!data || !Array.isArray(data.entries)) return;
    for (const e of data.entries) {
      if (e && typeof e.key === "string" && typeof e.html === "string") {
        this.set(e.key, {
          html: e.html,
          timestamp: typeof e.timestamp === "number" ? e.timestamp : Date.now(),
          params: e.params && typeof e.params === "object" ? e.params : undefined,
          query: e.query && typeof e.query === "object" ? e.query : undefined,
          tags: Array.isArray(e.tags) ? e.tags.filter((t: unknown) => typeof t === "string") : undefined,
          revalidate: typeof e.revalidate === "number" ? e.revalidate : undefined,
        });
      }
    }
  }
}
export type CoreInternalOp = "revalidate-route" | "refresh-route" | "cache-invalidate";

/** Host-provided pieces of the render core. */
export interface SSRCoreOptions {
  /** Page ISR cache storage (default: MemoryIsrStore of maxCacheSize entries). */
  isrStore?: IsrStore;
  manifest: ServerManifest | null;
  /** Loads the server bundle of a page (cached by the host). */
  loadPageModule(page: ManifestPage): Promise<any>;
  /** Returns the __krate_render(propsJSON) function of a runtime component bundle. */
  loadRuntimeRenderer(relPath: string): (propsJSON: string) => string | Promise<string>;
  /** ISR/region cache entry bound (default 512). */
  maxCacheSize?: number;
  /** Per-render timeout in ms (default 5000, 0 disables). */
  timeoutMs?: number;
  ppr?: boolean;
  pprRevalidate?: number;
  /**
   * Hex sha256 of a string (UTF-8). Hosts inject node:crypto where they have
   * it; the default is the portable synchronous implementation, so the error
   * digest is the same value on every host.
   */
  sha256Hex?(input: string): string;
  /** Registers work that must outlive the response (stale-while-revalidate refreshes). */
  background?(work: Promise<unknown>): void;
  /** Called when the ISR cache changed; immediate is true for explicit invalidations. */
  onIsrChange?(immediate: boolean): void;
}

/**
 * Storage behind the page ISR cache. The default keeps entries in process
 * memory (MemoryIsrStore); a host can pass its own (a KV or Redis store) so
 * serverless instances share rendered pages. Methods may be async.
 */
export interface IsrStore {
  get(key: string): CacheEntry | undefined | Promise<CacheEntry | undefined>;
  set(key: string, entry: CacheEntry): void | Promise<void>;
  /** Removes every variant of a route; returns how many entries went. */
  deleteRoute(route: string): number | Promise<number>;
  /** Removes one params variant of a route (every query variant of it). */
  deleteVariant(route: string, params: Record<string, string>): number | Promise<number>;
  /** Removes every entry whose render read one of tags. */
  deleteTags(tags: string[]): number | Promise<number>;
  /** The cached variants of a route, for background refresh. */
  entriesForRoute(route: string): Array<[string, CacheEntry]> | Promise<Array<[string, CacheEntry]>>;
}

/** The in-process IsrStore: a bounded map with a tag index. */
export class MemoryIsrStore implements IsrStore {
  readonly cache: ISRCache;
  constructor(maxSize = 512) {
    this.cache = new ISRCache(maxSize);
  }
  get(key: string) {
    return this.cache.get(key);
  }
  set(key: string, entry: CacheEntry) {
    this.cache.set(key, entry);
  }
  deleteRoute(route: string) {
    return this.cache.deleteRoute(route);
  }
  deleteVariant(route: string, params: Record<string, string>) {
    return this.cache.deleteVariant(route, params);
  }
  deleteTags(tags: string[]) {
    return this.cache.deleteTags(tags);
  }
  entriesForRoute(route: string) {
    return this.cache.entriesForRoute(route);
  }
}

export interface SSRCore {
  isrStore: IsrStore;
  findPage(route: string): ManifestPage | undefined;
  renderPage(req: RenderRequest): Promise<RenderResponse>;
  renderRegion(page: ManifestPage, region: RegionMeta, req: RenderRequest): Promise<Record<string, any>>;
  renderPageRegion(page: ManifestPage, id: string, req: RenderRequest): Promise<Record<string, any>>;
  applyInternalOp(op: CoreInternalOp, payload: any): Promise<Record<string, any>>;
  /** Drops every cached variant of a route and all region caches (dev reload). */
  devInvalidate(route: string): void;
}

/**
 * Splits the document <title> out of rendered page html (see takeHead for the
 * head tags that come with it).
 */
export function takeHeadTitle(html: string): { title: string; html: string } {
  const split = takeHead(html);
  return { title: split.title, html: split.html };
}

export function createSSRCore(opts: SSRCoreOptions): SSRCore {
  const sha256Hex = opts.sha256Hex || portableSha256Hex;

  // errorDigest is the short id tying the sanitized error a visitor sees to the
  // full error in the server log. The Go server computes the same hash
  // (build/segmentserve.go errorDigest).
  function errorDigest(message: string, stack: string): string {
    return sha256Hex(message + "\n" + stack).slice(0, 10);
  }

  function describeFailure(err: any): { error: string; stack: string; digest: string } {
    const message = (err && err.message) || String(err);
    const stack = (err && typeof err.stack === "string" && err.stack) || "";
    return { error: message, stack, digest: errorDigest(message, stack) };
  }

  // pageQueryReads remembers, per route+params variant, which query keys renders
  // have read ("all" when a render enumerated the whole query).
  const pageQueryReads = new Map<string, Set<string> | "all">();

  function pageBaseKey(req: { route: string; params?: Record<string, string> }): string {
    return variantKey({ route: req.route, params: req.params });
  }

  // pageKey is the ISR cache key for a request: params, plus only the query keys
  // previous renders of this variant read.
  function pageKey(req: { route: string; params?: Record<string, string>; query?: Record<string, string> }): string {
    const reads = pageQueryReads.get(pageBaseKey(req));
    if (!reads) return pageBaseKey(req);
    if (reads === "all") return variantKey(req);
    const picked: Record<string, string> = {};
    for (const k of [...reads].sort()) {
      if (req.query && Object.prototype.hasOwnProperty.call(req.query, k)) picked[k] = req.query[k];
    }
    return variantKey({ route: req.route, params: req.params, query: picked });
  }

  // The map is keyed by params, which a client chooses, so it is bounded like
  // the cache it describes. Losing an entry only costs one extra render, which
  // records the reads again.
  const PAGE_QUERY_READS_MAX = Math.max(4 * (opts.maxCacheSize && opts.maxCacheSize > 0 ? opts.maxCacheSize : 512), 1024);

  function recordQueryReads(req: { route: string; params?: Record<string, string> }, t: QueryTracker): void {
    const base = pageBaseKey(req);
    let cur = pageQueryReads.get(base);
    if (t.all) cur = "all";
    else if (t.keys.size > 0 && cur !== "all") cur = new Set([...(cur || []), ...t.keys]);
    if (cur) {
      pageQueryReads.delete(base);
      pageQueryReads.set(base, cur);
      if (pageQueryReads.size > PAGE_QUERY_READS_MAX) {
        const oldest = pageQueryReads.keys().next().value;
        if (oldest !== undefined) pageQueryReads.delete(oldest);
      }
    }
  }

  // ISR cache size and the render timeout are configurable via the compiler
  // (ssr.maxCacheSize / ssr.timeout), which exports them to the sidecar env.
  const isrStore: IsrStore = opts.isrStore || new MemoryIsrStore(opts.maxCacheSize && opts.maxCacheSize > 0 ? opts.maxCacheSize : 512);
  // pageIntervals remembers each ISR page's revalidate window (seconds).
  const pageIntervals = new Map<string, number>();

  // entryStale: the entry is older than its page window, or than the smaller
  // window one of its data reads declared.
  function entryStale(key: string, entry: CacheEntry): boolean {
    const route = key.split("?")[0].split("#")[0];
    let interval = pageIntervals.get(route) || 0;
    if (entry.revalidate && entry.revalidate > 0 && (!interval || entry.revalidate < interval)) interval = entry.revalidate;
    if (!interval) return false;
    return (Date.now() - entry.timestamp) / 1000 > interval;
  }

  // renderTimeoutMs bounds a single renderToString call so one pathological page
  // cannot pin the sidecar. 0 disables the bound.
  const renderTimeoutMs = opts.timeoutMs !== undefined ? opts.timeoutMs : 5000;

  // Partial Prerendering: cache dynamic regions on non-ISR pages with this
  // window (overridable per region via `export const revalidate`).
  const pprEnabled = !!opts.ppr;
  const pprRevalidate = opts.pprRevalidate && opts.pprRevalidate > 0 ? opts.pprRevalidate : 60;

  // withRenderTimeout rejects if p does not settle within renderTimeoutMs. The
  // underlying render keeps running (JS cannot cancel it), but the request fails
  // fast rather than hanging.
  function withRenderTimeout<T>(p: Promise<T>): Promise<T> {
    if (renderTimeoutMs <= 0) return p;
    return new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error(`render timed out after ${renderTimeoutMs}ms`)), renderTimeoutMs);
      p.then(
        (v) => { clearTimeout(timer); resolve(v); },
        (e) => { clearTimeout(timer); reject(e); },
      );
    });
  }
  // Renderer

  const manifest: ServerManifest | null = opts.manifest;

  function findPage(route: string): ManifestPage | undefined {
    if (!manifest) return undefined;
    // Try exact match first, then strip trailing slash
    let page = manifest.pages.find((p) => p.route === route);
    if (!page && route !== "/") {
      page = manifest.pages.find((p) => p.route === route + "/");
    }
    if (!page && route.endsWith("/")) {
      page = manifest.pages.find((p) => p.route === route.slice(0, -1));
    }
    return page;
  }

  // Request scope for server components: headers()/cookies() from
  // @krate/runtime/server read it through the global store symbol, so it works
  // no matter which copy of the runtime a page bundle resolved.
  const requestStore: AsyncLocalStorage<RequestContext> = (() => {
    const key = Symbol.for("krate.request.store");
    const g = globalThis as Record<symbol, unknown>;
    if (!g[key]) g[key] = new AsyncLocalStorage<RequestContext>();
    return g[key] as AsyncLocalStorage<RequestContext>;
  })();

  function requestContextFor(req: RenderRequest, tracker?: QueryTracker): RequestContext {
    const h = new Headers();
    for (const [k, v] of Object.entries(req.headers || {})) {
      if (typeof v === "string") {
        try {
          h.set(k, v);
        } catch {
          // skip header names the WHATWG Headers class rejects
        }
      }
    }
    return {
      url: req.url || req.route,
      method: req.method || "GET",
      headers: h,
      cookies: parseCookieHeader(h.get("cookie")),
      params: { ...(req.params || {}) },
      searchParams: tracker ? trackedQuery(req.query, tracker) : { ...(req.query || {}) },
    };
  }

  function withRequest<T>(req: RenderRequest, fn: () => Promise<T>, tracker?: QueryTracker): Promise<T> {
    return requestStore.run(requestContextFor(req, tracker), fn);
  }

  // renderSignal maps notFound()/redirect() thrown during a render to a response.
  function renderSignal(err: unknown): RenderResponse | null {
    if (isNotFoundSignal(err)) return { html: "", status: 404, notFound: true };
    const denied = accessDeniedStatus(err);
    if (denied) return { html: "", status: denied, denied };
    const to = redirectTarget(err);
    if (to) return { html: "", status: 307, redirect: to };
    return null;
  }

  function buildProps(req: RenderRequest, tracker?: QueryTracker): Record<string, any> {
    // Page-level data fetching (getStaticProps/getServerSideProps) has been
    // removed; per-request data is provided by server components (@server),
    // runtime components (@runtime), and middleware instead. Dynamic-route
    // params and query parameters extracted by the Go server are forwarded so
    // `({ params }) => ...` pages receive their real values at render time.
    // params and searchParams are plain objects that can also be awaited, so
    // both `params.slug` and Next.js 15's `const { slug } = await params` work.
    // The Go server always forwards params (empty for static routes); a direct
    // request without them keeps params undefined, as before.
    const props: Record<string, any> = {};
    if (req.params) props.params = thenableRecord(req.params);
    if (tracker) {
      // Reads through the view are recorded; awaiting it resolves to another
      // tracked view so `const { page } = await searchParams` records "page".
      const view = trackedQuery(req.query, tracker);
      props.searchParams = thenable(view, () => trackedQuery(req.query, tracker));
      if (req.query) props.query = trackedQuery(req.query, tracker);
      return props;
    }
    props.searchParams = thenableRecord(req.query);
    if (req.query) props.query = req.query;
    return props;
  }

  // renderFresh renders a page component and - for ISR pages - stores the result.
  // Extracted so background revalidation shares exactly the same render path.
  async function renderFresh(page: ManifestPage, req: RenderRequest): Promise<RenderResponse> {
    try {
      const mod = await opts.loadPageModule(page);
      const Component = mod.default;
      if (!Component) {
        return { html: "", status: 500 };
      }

      // The render runs in a cache scope that records the tags / revalidate
      // windows its data reads declared (fetch next options, unstable_cache).
      // Async server components (and plain sync pages) render through
      // renderToStringAsync inside the request scope. The timeout bounds slow
      // awaits (fetch); a page that blocks synchronously still blocks the loop.
      const tracker = newQueryTracker();
      const { result: html, scope, invalidated } = await withRenderTimeout(
        withCacheScope(page.route, () =>
          withRequest(req, () => renderToStringAsync(jsx(Component, buildProps(req, tracker))), tracker),
        ),
      );

      const response: RenderResponse = {
        html,
        status: 200,
        noStore: scope.noStore || invalidated,
      };

      // An uncacheable read (no-store) keeps the render out of the ISR cache, and
      // a revalidation that landed mid-render means the HTML may be stale.
      if (page.mode === "isr" && !scope.noStore && !invalidated) {
        recordQueryReads(req, tracker);
        await isrStore.set(pageKey(req), {
          html,
          timestamp: Date.now(),
          params: req.params,
          query: req.query,
          tags: scope.tags.size ? [...scope.tags] : undefined,
          revalidate: scope.revalidate || undefined,
        });
        opts.onIsrChange?.(false);
      }

      return response;
    } catch (err: any) {
      const signal = renderSignal(err);
      if (signal) return signal;
      const failure = describeFailure(err);
      console.error(`[krate] Error rendering ${req.route} (digest ${failure.digest}):`, err);
      return { html: "", status: 500, ...failure };
    }
  }

  // Single-flight background revalidation for stale-while-revalidate. While a
  // revalidation for a given variant is in flight, concurrent requests share the
  // same promise instead of stampeding the renderer.
  const inFlightRevalidations = new Map<string, Promise<void>>();

  function revalidateInBackground(page: ManifestPage, req: RenderRequest) {
    const key = pageKey(req);
    if (inFlightRevalidations.has(key)) return;

    const p = (async () => {
      try {
        const rendered = await renderFresh(page, req);
        if (rendered.status === 200) {
          console.log(`[krate-ssr] Revalidated ${key} (SWR)`);
        }
      } catch (err: any) {
        console.error(`[krate-ssr] SWR revalidation failed ${key}:`, err.message);
      } finally {
        inFlightRevalidations.delete(key);
      }
    })();

    inFlightRevalidations.set(key, p);
    opts.background?.(p);
  }

  // Time-based ISR regeneration: re-render every cached variant of a route while
  // keeping the entries in place. This is the non-destructive counterpart of
  // /__krate/ssr/revalidate (which clears a route). The Go server's periodic ISR
  // timer calls this so cached pages stay fresh without evicting dynamic
  // variants - eviction would turn a request that should be a HIT into a MISS.
  const inFlightRouteRefreshes = new Map<string, Promise<number>>();

  function refreshRouteVariants(page: ManifestPage): Promise<number> {
    const running = inFlightRouteRefreshes.get(page.route);
    if (running) return running;

    const p = (async () => {
      try {
        const variants = await isrStore.entriesForRoute(page.route);
        let refreshed = 0;
        for (const [key, entry] of variants) {
          // A variant rendered within its own window is still fresh; re-rendering
          // it on every tick would multiply the render load by the variant count
          // and replace an entry a request has only just produced.
          if (!entryStale(key, entry)) continue;
          const req: RenderRequest = {
            route: page.route,
            url: page.route,
            method: "GET",
            headers: {},
            params: entry.params,
            query: entry.query,
          };
          const rendered = await renderFresh(page, req);
          if (rendered.status === 200) refreshed++;
        }
        return refreshed;
      } finally {
        inFlightRouteRefreshes.delete(page.route);
      }
    })();

    inFlightRouteRefreshes.set(page.route, p);
    return p;
  }

  async function renderPage(req: RenderRequest): Promise<RenderResponse> {
    const page = findPage(req.route);
    if (!page) {
      return { html: "", status: 404, notFound: true };
    }

    // ISR: serve from cache with stale-while-revalidate.
    if (page.mode === "isr") {
      const interval = page.revalidate || 60;
      pageIntervals.set(page.route, interval);
      const key = pageKey(req);
      const cached = await isrStore.get(key);

      if (cached && !entryStale(key, cached)) {
        return {
          html: cached.html,
          status: 200,
          cached: true,
          cacheStatus: "hit",
        };
      }

      if (cached) {
        // Entry is stale: serve it immediately and refresh in the background so
        // the next request (and any concurrent ones) get fresh HTML. No stampede:
        // revalidateInBackground is single-flight per variant key.
        revalidateInBackground(page, req);
        return {
          html: cached.html,
          status: 200,
          cached: true,
          cacheStatus: "stale",
        };
      }
    }

    const response = await renderFresh(page, req);
    if (page.mode === "isr") response.cacheStatus = "miss";
    return response;
  }

  // Region rendering (static-first)
  // The Go server owns the page: it serves the build-time static shell with the
  // <!-suspense:ID-> markers and baked fallbacks, then asks the sidecar to render
  // ONLY the page's dynamic regions. Each region is rendered by its compiled
  // runtime component bundle (server-components/<Name>.runtime.js - an IIFE that
  // defines globalThis.__krate_render(propsJSON)) with the build-time baked props,
  // so nothing is re-derived at request time.

  function regionProps(region: RegionMeta, req: RenderRequest): Record<string, any> {
    const props: Record<string, any> = { ...(region.props || {}) };
    if (req.params && Object.keys(req.params).length > 0) props.params = req.params;
    if (req.query && Object.keys(req.query).length > 0) props.query = req.query;
    return props;
  }

  // Region ISR cache: region-level stale-while-revalidate, separate from the
  // page ISR cache. Region HTML only depends on the baked props, so it is fully
  // determined by (route, region id, variant) and cacheable like a page variant.
  interface RegionEntry {
    html: string;
    timestamp: number;
    route: string;
    // paramsKey identifies the params variant (encoded like variantKey).
    paramsKey: string;
    tags?: string[];
  }
  const regionCache = new Map<string, RegionEntry>();
  const inFlightRegionRevalidations = new Map<string, Promise<void>>();

  // Region cache is bounded (least recently used goes first) so a long-lived
  // sidecar cannot grow without limit across many dynamic region variants.
  const REGION_CACHE_MAX = opts.maxCacheSize && opts.maxCacheSize > 0 ? opts.maxCacheSize : 512;

  function paramsKeyOf(params?: Record<string, string>): string {
    return variantKey({ route: "", params });
  }

  function regionCacheGet(key: string): RegionEntry | undefined {
    const entry = regionCache.get(key);
    if (entry) {
      regionCache.delete(key);
      regionCache.set(key, entry);
    }
    return entry;
  }

  function regionCacheSet(key: string, html: string, page: ManifestPage, req: RenderRequest, scope?: CacheScope): void {
    regionCache.delete(key);
    if (regionCache.size >= REGION_CACHE_MAX) {
      const oldest = regionCache.keys().next().value;
      if (oldest !== undefined) regionCache.delete(oldest);
    }
    regionCache.set(key, {
      html,
      timestamp: Date.now(),
      route: page.route,
      paramsKey: paramsKeyOf(req.params),
      tags: scope && scope.tags.size ? [...scope.tags] : undefined,
    });
  }

  // invalidateRegions removes region entries for a route (optionally one params
  // variant) and/or entries whose render read one of tags.
  function invalidateRegions(route: string | null, params: Record<string, string> | null, tags: string[]): number {
    const pkey = params ? paramsKeyOf(params) : null;
    let n = 0;
    for (const [key, e] of Array.from(regionCache.entries())) {
      const routeHit = route !== null && e.route === route && (pkey === null || e.paramsKey === pkey);
      const tagHit = tags.length > 0 && !!e.tags && e.tags.some((t) => tags.includes(t));
      if (routeHit || tagHit) {
        regionCache.delete(key);
        n++;
      }
    }
    return n;
  }

  function clearRegionCache(): void {
    regionCache.clear();
  }

  function clearRegionCacheForRoute(route: string): void {
    const prefix = route + "::";
    for (const key of Array.from(regionCache.keys())) {
      if (key.startsWith(prefix)) regionCache.delete(key);
    }
  }

  // regionQueryReads is pageQueryReads for regions: which query keys renders of
  // a server component region have read. Only those regions are tracked: a
  // runtime-bundle region receives the whole query serialized into its props,
  // so what it reads cannot be observed and it stays keyed by the full query.
  const regionQueryReads = new Map<string, Set<string> | "all">();

  function regionBaseKey(page: ManifestPage, regionId: string, req: RenderRequest): string {
    return variantKey({ route: page.route + "::" + regionId, params: req.params });
  }

  function regionCacheKey(page: ManifestPage, region: RegionMeta, req: RenderRequest): string {
    if (!region.serverExport) {
      return variantKey({ route: page.route + "::" + region.id, params: req.params, query: req.query });
    }
    const reads = regionQueryReads.get(regionBaseKey(page, region.id, req));
    if (!reads) return regionBaseKey(page, region.id, req);
    if (reads === "all") return variantKey({ route: page.route + "::" + region.id, params: req.params, query: req.query });
    const picked: Record<string, string> = {};
    for (const k of [...reads].sort()) {
      if (req.query && Object.prototype.hasOwnProperty.call(req.query, k)) picked[k] = req.query[k];
    }
    return variantKey({ route: page.route + "::" + region.id, params: req.params, query: picked });
  }

  function recordRegionQueryReads(page: ManifestPage, regionId: string, req: RenderRequest, t: QueryTracker): void {
    const base = regionBaseKey(page, regionId, req);
    let cur = regionQueryReads.get(base);
    if (t.all) cur = "all";
    else if (t.keys.size > 0 && cur !== "all") cur = new Set([...(cur || []), ...t.keys]);
    if (cur) {
      regionQueryReads.delete(base);
      regionQueryReads.set(base, cur);
      if (regionQueryReads.size > PAGE_QUERY_READS_MAX) {
        const oldest = regionQueryReads.keys().next().value;
        if (oldest !== undefined) regionQueryReads.delete(oldest);
      }
    }
  }

  // regionInterval resolves a region's cache window: its own revalidate directive
  // wins, else the page's ISR cadence; non-ISR pages with no directive are 0.
  function regionInterval(page: ManifestPage, region: RegionMeta): number {
    if (region.revalidate && region.revalidate > 0) return region.revalidate;
    if (page.mode === "isr") return page.revalidate || 60;
    if (pprEnabled) return pprRevalidate;
    return 0;
  }

  async function renderRegionFresh(
    page: ManifestPage,
    region: RegionMeta,
    req: RenderRequest,
    tracker?: QueryTracker,
  ): Promise<{ html: string; scope: CacheScope; invalidated: boolean }> {
    if (region.serverExport) {
      const out = await withRenderTimeout(
        withCacheScope(page.route, () => withRequest(req, () => renderServerExport(page, region, req, tracker), tracker)),
      );
      return { html: out.result, scope: out.scope, invalidated: out.invalidated };
    }
    const render = opts.loadRuntimeRenderer(region.bundlePath!);
    const out = await withRenderTimeout(
      withCacheScope(page.route, () => render(JSON.stringify(regionProps(region, req)))),
    );
    return { html: out.result, scope: out.scope, invalidated: out.invalidated };
  }

  function regionCacheable(r: { scope: CacheScope; invalidated: boolean }): boolean {
    return !r.scope.noStore && !r.invalidated;
  }

  // renderServerExport renders an async server component region with the page's
  // own server bundle, which exports the component under region.serverExport.
  async function renderServerExport(page: ManifestPage, region: RegionMeta, req: RenderRequest, tracker?: QueryTracker): Promise<string> {
    const mod = await opts.loadPageModule(page);
    const Comp = mod[region.serverExport!];
    if (typeof Comp !== "function") {
      throw new Error(`server bundle for ${page.route} does not export ${region.serverExport}`);
    }
    const props = regionProps(region, req);
    if (props.params && typeof props.params === "object") props.params = thenableRecord(props.params);
    if (tracker && props.query) props.query = trackedQuery(req.query, tracker);
    return renderToStringAsync(jsx(Comp, props));
  }

  function revalidateRegionInBackground(page: ManifestPage, region: RegionMeta, req: RenderRequest, key: string) {
    if (inFlightRegionRevalidations.has(key)) return;
    const p = (async () => {
      try {
        const tracker = region.serverExport ? newQueryTracker() : undefined;
        const r = await renderRegionFresh(page, region, req, tracker);
        if (regionCacheable(r)) {
          if (tracker) recordRegionQueryReads(page, region.id, req, tracker);
          regionCacheSet(regionCacheKey(page, region, req), r.html, page, req, r.scope);
        }
        console.log(`[krate-ssr] Revalidated region ${key} (SWR)`);
      } catch (err: any) {
        console.error(`[krate-ssr] Region SWR revalidation failed ${key}:`, err.message);
      } finally {
        inFlightRegionRevalidations.delete(key);
      }
    })();
    inFlightRegionRevalidations.set(key, p);
    opts.background?.(p);
  }

  // renderRegion renders one region, applying region-level ISR caching for ISR
  // pages. Returns an NDJSON frame.
  async function renderRegion(page: ManifestPage, region: RegionMeta, req: RenderRequest): Promise<Record<string, any>> {
    const frame = await renderRegionFrame(page, region, req);
    // A server component region may render <Head> children: they move to the
    // head of the response, like the page region's. The caches keep the html
    // as rendered, so a hit is split the same way.
    if (region.serverExport && frame.type === "region" && typeof frame.html === "string" && frame.html !== "") {
      applyHead(frame, frame.html);
    }
    return frame;
  }

  async function renderRegionFrame(page: ManifestPage, region: RegionMeta, req: RenderRequest): Promise<Record<string, any>> {
    if (!region.bundlePath && !region.serverExport) {
      return { type: "error", id: region.id, status: 400, error: "region has no runtime bundle", fatal: !!page.serverComponents };
    }

    const key = regionCacheKey(page, region, req);
    const interval = regionInterval(page, region);
    const cacheable = interval > 0;

    if (cacheable) {
      const cached = regionCacheGet(key);
      const stale = cached && (Date.now() - cached.timestamp) / 1000 > interval;
      if (cached && !stale) {
        return { type: "region", id: region.id, status: 200, html: cached.html, cached: true, cacheStatus: "hit" };
      }
      if (cached) {
        revalidateRegionInBackground(page, region, req, key);
        return { type: "region", id: region.id, status: 200, html: cached.html, cached: true, cacheStatus: "stale" };
      }
    }

    try {
      const tracker = region.serverExport ? newQueryTracker() : undefined;
      const r = await renderRegionFresh(page, region, req, tracker);
      const html = r.html;
      if (cacheable && regionCacheable(r)) {
        // The key is computed again: this render may have shown which query
        // keys the region reads.
        if (tracker) recordRegionQueryReads(page, region.id, req, tracker);
        regionCacheSet(tracker ? regionCacheKey(page, region, req) : key, html, page, req, r.scope);
      }
      const frame: Record<string, any> = { type: "region", id: region.id, status: 200, html };
      if (cacheable) frame.cacheStatus = "miss";
      if (r.scope.noStore || r.invalidated) frame.noStore = true;
      return frame;
    } catch (err: any) {
      const signal = renderSignal(err);
      if (signal) {
        const frame: Record<string, any> = { type: "region", id: region.id, status: signal.status, html: "" };
        if (signal.notFound) frame.notFound = true;
        if (signal.denied) frame.denied = signal.denied;
        if (signal.redirect) frame.redirect = signal.redirect;
        return frame;
      }
      const failure = describeFailure(err);
      console.error(`[krate-ssr] Region render failed ${page.route}::${region.id} (digest ${failure.digest}):`, err);
      // A server component region has no meaningful baked content, so its
      // failure is fatal for the response (the Go server answers 500).
      return { type: "error", id: region.id, status: 500, ...failure, fatal: !!(region.serverExport || page.serverComponents) };
    }
  }

  // applyHead sets frame.html to html without its <Head> content and carries
  // the title and head tags on the frame.
  function applyHead(frame: Record<string, any>, html: string): void {
    const split = takeHead(html);
    frame.html = split.html;
    if (split.title !== "") frame.title = split.title;
    if (split.head !== "") frame.head = split.head;
  }

  // renderPageRegion renders a coarse whole-page region (SSR/ISR pages whose
  // entire body is request-time dynamic). It reuses renderPage so the page-level
  // variant-aware ISR cache, stale-while-revalidate, and revalidation all apply.
  // Returns an NDJSON region frame; the Go server splices html into the shell at
  // the "page" marker and keeps the baked body when the render fails.
  async function renderPageRegion(page: ManifestPage, id: string, req: RenderRequest): Promise<Record<string, any>> {
    const result = await renderPage(req);
    const frame: Record<string, any> = {
      type: "region",
      id,
      kind: "page",
      status: result.status || 200,
      html: result.html || "",
    };
    if (result.cacheStatus) frame.cacheStatus = result.cacheStatus;
    if (result.notFound) frame.notFound = true;
    if (result.denied) frame.denied = result.denied;
    if (result.redirect) frame.redirect = result.redirect;
    if (result.error) frame.error = result.error;
    if (result.digest) frame.digest = result.digest;
    if (result.stack) frame.stack = result.stack;
    if (result.noStore) frame.noStore = true;
    if (page.serverComponents && frame.status >= 500) frame.fatal = true;
    // The page's <Head> children (title, meta, link, script) render in place;
    // they move to the head of the response, so they are taken out of the body
    // html rather than shown twice. The host replaces/merges them into the
    // baked head (Go: build/headmerge.go, portable handler: head-tags.ts).
    applyHead(frame, result.html || "");
    return frame;
  }

  interface InvalidateTarget {
    route: string;
    params?: Record<string, string> | null;
  }

  function stringList(v: unknown): string[] {
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string" && x !== "") : [];
  }

  // applyInternalOp applies one internal operation to this process's caches.
  async function applyInternalOp(op: CoreInternalOp, payload: any): Promise<Record<string, any>> {
    switch (op) {
      case "revalidate-route": {
        const targetRoute = String(payload.route || "");
        await isrStore.deleteRoute(targetRoute);
        clearRegionCacheForRoute(targetRoute);
        cacheRuntime().invalidate({});
        opts.onIsrChange?.(true);
        const page = findPage(targetRoute);
        if (page) {
          await renderFresh(page, { route: targetRoute, url: targetRoute, method: "GET", headers: {} });
        }
        return { ok: true };
      }
      case "refresh-route": {
        const targetRoute = String(payload.route || "");
        const page = findPage(targetRoute);
        const refreshed = page && page.mode === "isr" ? await refreshRouteVariants(page) : 0;
        clearRegionCacheForRoute(targetRoute);
        return { ok: true, refreshed };
      }
      case "cache-invalidate": {
        const tags = stringList(payload.tags);
        const targets: InvalidateTarget[] = Array.isArray(payload.targets)
          ? payload.targets.filter((t: any) => t && typeof t.route === "string")
          : [];
        // Path targets also drop the data their renders cached (implicit tag).
        const dataTags = tags.concat(targets.map((t) => routeDataTag(t.route)));
        const { dataEntries } = cacheRuntime().invalidate({ tags: dataTags });
        let isrEntries = 0;
        let regionEntries = 0;
        for (const t of targets) {
          const params = t.params && typeof t.params === "object" ? t.params : null;
          isrEntries += params ? await isrStore.deleteVariant(t.route, params) : await isrStore.deleteRoute(t.route);
          regionEntries += invalidateRegions(t.route, params, []);
        }
        if (tags.length) {
          isrEntries += await isrStore.deleteTags(tags);
          regionEntries += invalidateRegions(null, null, tags);
        }
        if (isrEntries > 0) opts.onIsrChange?.(true);
        return { ok: true, isrEntries, regionEntries, dataEntries };
      }
    }
  }

  return {
    isrStore,
    findPage,
    renderPage,
    renderRegion,
    renderPageRegion,
    applyInternalOp,
    devInvalidate(route: string) {
      void isrStore.deleteRoute(route);
      clearRegionCache();
    },
  };
}
