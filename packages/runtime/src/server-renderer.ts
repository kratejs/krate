// @krate/runtime/server-renderer - Node.js HTTP server for SSR/ISR/Streaming
// Receives render requests from the Go server and returns HTML.

import http from "node:http";
import cluster from "node:cluster";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { pathToFileURL } from "node:url";
import { AsyncLocalStorage } from "node:async_hooks";
import { timingSafeEqual } from "node:crypto";
import { renderToString } from "./server.js";
import { cacheRuntime, installRenderStore, patchFetch, withCacheScope, type CacheScope } from "./cache-scope.js";

// Render cache scopes (tags / revalidate windows read during a render) and the
// Next-style fetch data cache. Installed once per process; page bundles share
// the state through globalThis.
installRenderStore(new AsyncLocalStorage<CacheScope>());
patchFetch();

// Shared secret for the mutating internal endpoints (/__krate/ssr/revalidate,
// /refresh, /invalidate, /cache-invalidate). Krate passes it from the Go host;
// a renderer started by hand without it keeps the endpoints open.
const INTERNAL_SECRET = process.env.KRATE_INTERNAL_SECRET || "";

function internalAuthorized(req: http.IncomingMessage): boolean {
  if (!INTERNAL_SECRET) return true;
  const got = req.headers["x-krate-internal"];
  if (typeof got !== "string") return false;
  const a = Buffer.from(got);
  const b = Buffer.from(INTERNAL_SECRET);
  return a.length === b.length && timingSafeEqual(a, b);
}

// Cluster across cores: page/region rendering is independent per request, so N
// workers scale it past a single core. Krate sets KRATE_SSR_WORKERS from
// ssr.rendererWorkers. Region/ISR caches are per-worker and ISR disk persistence
// is disabled when clustered (the primary never serves).
const SSR_WORKERS = Math.max(1, parseInt(process.env.KRATE_SSR_WORKERS || "1", 10) || 1);
const isClusterPrimary = SSR_WORKERS > 1 && cluster.isPrimary;
if (isClusterPrimary) {
  for (let i = 0; i < SSR_WORKERS; i++) cluster.fork();
  // Restart a worker that dies, after a short delay, but give up when they
  // keep dying (a port that stays unbindable, a broken bundle) instead of
  // forking forever. The delay also gives a briefly busy port time to free up.
  const exits: number[] = [];
  cluster.on("exit", (worker) => {
    console.log(`[krate-ssr] worker ${worker.process.pid} exited; restarting`);
    const now = Date.now();
    exits.push(now);
    while (exits.length && now - exits[0] > 60000) exits.shift();
    if (exits.length > 100) {
      console.error("[krate-ssr] workers keep exiting; giving up");
      process.exit(1);
    }
    setTimeout(() => cluster.fork(), 100);
  });
  console.log(`[krate-ssr] primary supervising ${SSR_WORKERS} workers`);

  // Internal-op fan-out: a worker that receives a revalidation asks the
  // primary to apply it on every live worker (each has its own caches) and
  // answers its HTTP request only once all of them acknowledged. A worker that
  // exits mid-broadcast counts as done: its caches died with it.
  interface Broadcast {
    origin: { send(msg: unknown): boolean; isConnected(): boolean };
    remaining: Set<number>;
    results: unknown[];
  }
  const broadcasts = new Map<string, Broadcast>();
  const finish = (id: string) => {
    const b = broadcasts.get(id);
    if (!b || b.remaining.size > 0) return;
    broadcasts.delete(id);
    if (b.origin.isConnected()) b.origin.send({ type: "krate:bcast-done", id, results: b.results });
  };
  cluster.on("message", (worker, msg: any) => {
    if (!msg || typeof msg !== "object") return;
    if (msg.type === "krate:bcast") {
      const live = Object.values(cluster.workers || {}).filter((w) => w && w.isConnected()) as any[];
      broadcasts.set(msg.id, { origin: worker, remaining: new Set(live.map((w) => w.id)), results: [] });
      for (const w of live) w.send({ type: "krate:apply", id: msg.id, op: msg.op, payload: msg.payload });
      finish(msg.id);
    } else if (msg.type === "krate:applied") {
      const b = broadcasts.get(msg.id);
      if (!b) return;
      b.results.push(msg.result);
      b.remaining.delete(worker.id);
      finish(msg.id);
    }
  });
  cluster.on("exit", (worker) => {
    for (const [id, b] of broadcasts) {
      if (b.remaining.delete(worker.id)) finish(id);
    }
  });
}

// Force TCP_NODELAY on socket to prevent Nagle buffering chunks together.
// Must be called BEFORE the first write for it to take effect.
function setupStream(res: http.ServerResponse) {
  const socket = (res as any).socket;
  if (socket && typeof socket.setNoDelay === 'function') {
    socket.setNoDelay(true);
  }
}

// Types

interface ManifestPage {
  route: string;
  source: string;
  mode: string;
  revalidate?: number;
  bundlePath?: string;
}

interface RegionMeta {
  id: string;
  component?: string;
  sourcePath?: string;
  bundlePath?: string;
  suspense?: boolean;
  props?: Record<string, any>;
  /** Per-region revalidate seconds (PPR cache directive). 0/undefined falls
   *  back to the page's ISR cadence, or no caching for non-ISR pages. */
  revalidate?: number;
}

interface ServerManifest {
  pages: ManifestPage[];
  stylesheet?: string;
  runtimeJS?: string;
  regions?: Record<string, RegionMeta[]>;
}

interface CacheEntry {
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

interface RenderRequest {
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

type CacheStatus = "hit" | "stale" | "miss";

interface RenderResponse {
  html: string;
  status: number;
  redirect?: string;
  notFound?: boolean;
  cached?: boolean;
  cacheStatus?: CacheStatus;
}

// ISR Cache (variant-aware, SWR, persisted)

// A page's cache key is the route plus its per-request variant (params +
// query). Two dynamic-route requests (e.g. /video/a and /video/b) render
// different HTML, so they must never share a cache slot.
function variantKey(req: { route: string; params?: Record<string, string>; query?: Record<string, string> }): string {
  const enc = (v: Record<string, string>) =>
    Object.keys(v).sort().map((k) => `${encodeURIComponent(k)}=${encodeURIComponent(v[k])}`).join("&");
  const params = req.params ? enc(req.params) : "";
  const query = req.query ? enc(req.query) : "";
  let key = req.route;
  if (params) key += "?" + params;
  if (query) key += "#" + query;
  return key;
}

class ISRCache {
  private cache = new Map<string, CacheEntry>();
  private byTag = new Map<string, Set<string>>();
  private maxSize: number;
  private revalidationIntervals = new Map<string, number>(); // route → seconds

  constructor(maxSize = 512) {
    this.maxSize = maxSize;
  }

  setRevalidation(route: string, seconds: number) {
    this.revalidationIntervals.set(route, seconds);
  }

  get(key: string): CacheEntry | undefined {
    return this.cache.get(key);
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

// ISR cache size and the render timeout are configurable via the compiler
// (ssr.maxCacheSize / ssr.timeout), which exports them to the sidecar env.
const isrCache = new ISRCache(parseInt(process.env.KRATE_SSR_MAX_CACHE || "", 10) || 512);

// renderTimeoutMs bounds a single renderToString call so one pathological page
// cannot pin the sidecar. 0 disables the bound.
const renderTimeoutMs = parseInt(process.env.KRATE_SSR_TIMEOUT || "", 10) || 5000;

// Partial Prerendering: cache dynamic regions on non-ISR pages with this
// window (overridable per region via `export const revalidate`).
const pprEnabled = process.env.KRATE_PPR === "1";
const pprRevalidate = parseInt(process.env.KRATE_PPR_REVALIDATE || "", 10) || 60;

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

// ISR cache persistence - survives renderer restarts so a bounce doesn't
// cold-render every ISR variant. Written debounced (coalesced) to the build
// output dir (.krate/isr-cache.json), overridable via KRATE_ISR_CACHE.
let isrCacheFile = process.env.KRATE_ISR_CACHE || "";
let persistTimer: NodeJS.Timeout | null = null;

function isrCachePath(): string {
  return isrCacheFile || path.join(root, "dist", ".krate", "isr-cache.json");
}

function persistIsrNow() {
  if (SSR_WORKERS > 1) return;
  if (persistTimer) {
    clearTimeout(persistTimer);
    persistTimer = null;
  }
  try {
    const data = isrCache.serialize();
    const file = isrCachePath();
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, JSON.stringify(data));
  } catch (err: any) {
    console.error("[krate-ssr] ISR cache persist failed:", err.message);
  }
}

function scheduleIsrPersist() {
  // Clustered: multiple workers would race on the same cache file; keep the ISR
  // cache in memory only.
  if (SSR_WORKERS > 1) return;
  if (persistTimer) clearTimeout(persistTimer);
  persistTimer = setTimeout(persistIsrNow, 1000);
}

function loadIsrCache() {
  try {
    const file = isrCachePath();
    if (fs.existsSync(file)) {
      const data = JSON.parse(fs.readFileSync(file, "utf-8"));
      isrCache.hydrate(data);
      const n = isrCache.size;
      if (n > 0) console.log(`[krate-ssr] Loaded ${n} persisted ISR entries`);
    }
  } catch (err: any) {
    console.error("[krate-ssr] ISR cache load failed:", err.message);
  }
}

// Page Module Loader

const moduleCache = new Map<string, any>();

async function loadPageModule(page: ManifestPage): Promise<any> {
  // Prefer pre-compiled server bundle over raw source
  const key = page.bundlePath || page.source;
  if (moduleCache.has(key)) {
    return moduleCache.get(key);
  }

  let fileUrl: string;

  if (page.bundlePath) {
    // Pre-compiled bundle: bundlePath is relative to outDir (dist/)
    const bundleAbs = path.resolve(root, "dist", page.bundlePath);
    fileUrl = pathToFileURL(bundleAbs).href;
  } else {
    // Fallback: raw source (legacy mode)
    fileUrl = pathToFileURL(path.resolve(page.source)).href;
  }

  try {
    const mod = await import(fileUrl + "?t=" + Date.now());
    moduleCache.set(key, mod);
    return mod;
  } catch (err: any) {
    console.error(`[krate-ssr] Failed to load module for ${page.route}:`, err.message);
    throw err;
  }
}

// Renderer

let manifest: ServerManifest | null = null;
let projectRoot = "";

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

function buildProps(req: RenderRequest): Record<string, any> {
  // Page-level data fetching (getStaticProps/getServerSideProps) has been
  // removed; per-request data is provided by server components (@server),
  // runtime components (@runtime), and middleware instead. Dynamic-route
  // params and query parameters extracted by the Go server are forwarded so
  // `({ params }) => ...` pages receive their real values at render time.
  const props: Record<string, any> = {};
  if (req.params) props.params = req.params;
  if (req.query) props.query = req.query;
  return props;
}

// renderFresh renders a page component and - for ISR pages - stores the result.
// Extracted so background revalidation shares exactly the same render path.
async function renderFresh(page: ManifestPage, req: RenderRequest): Promise<RenderResponse> {
  try {
    const mod = await loadPageModule(page);
    const Component = mod.default;
    if (!Component) {
      return { html: "", status: 500 };
    }

    // The render runs in a cache scope that records the tags / revalidate
    // windows its data reads declared (fetch next options, unstable_cache).
    // Bound the render. renderToString is synchronous, so a page that blocks
    // would block the sidecar's event loop regardless; the timeout still
    // protects the request path when rendering is async (suspense/resource).
    const { result: html, scope, invalidated } = await withRenderTimeout(
      withCacheScope(page.route, async () => renderToString(await Component(buildProps(req)))),
    );

    const response: RenderResponse = {
      html,
      status: 200,
    };

    // An uncacheable read (no-store) keeps the render out of the ISR cache, and
    // a revalidation that landed mid-render means the HTML may be stale.
    if (page.mode === "isr" && !scope.noStore && !invalidated) {
      isrCache.set(variantKey(req), {
        html,
        timestamp: Date.now(),
        params: req.params,
        query: req.query,
        tags: scope.tags.size ? [...scope.tags] : undefined,
        revalidate: scope.revalidate || undefined,
      });
      scheduleIsrPersist();
    }

    return response;
  } catch (err: any) {
    console.error(`[krate] Error rendering ${req.route}:`, err);
    return { html: "", status: 500 };
  }
}

// Single-flight background revalidation for stale-while-revalidate. While a
// revalidation for a given variant is in flight, concurrent requests share the
// same promise instead of stampeding the renderer.
const inFlightRevalidations = new Map<string, Promise<void>>();

function revalidateInBackground(page: ManifestPage, req: RenderRequest) {
  const key = variantKey(req);
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
      const variants = isrCache.entriesForRoute(page.route);
      let refreshed = 0;
      for (const [, entry] of variants) {
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
    isrCache.setRevalidation(page.route, interval);
    const key = variantKey(req);
    const cached = isrCache.get(key);

    if (cached && !isrCache.isStale(key)) {
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

const runtimeBundleCache = new Map<string, (propsJSON: string) => string>();

// loadRuntimeRenderer loads a compiled runtime component bundle and returns its
// __krate_render(propsJSON) function. Bundles are cached by bundle path.
function loadRuntimeRenderer(relPath: string): (propsJSON: string) => string {
  const cached = runtimeBundleCache.get(relPath);
  if (cached) return cached;

  const abs = path.resolve(root, "dist", relPath);
  const code = fs.readFileSync(abs, "utf-8");
  const ctx = vm.createContext({ console });
  vm.runInContext(code, ctx, { filename: relPath });
  const render = vm.runInContext(
    "typeof globalThis.__krate_render === 'function' ? globalThis.__krate_render : typeof __krate_render === 'function' ? __krate_render : null",
    ctx,
  );
  if (typeof render !== "function") {
    throw new Error(`Runtime bundle ${relPath} does not expose __krate_render`);
  }
  runtimeBundleCache.set(relPath, render);
  return render;
}

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
const regionRevalidation = new Map<string, number>();
const inFlightRegionRevalidations = new Map<string, Promise<void>>();

// Region cache is bounded (FIFO eviction) so a long-lived sidecar cannot grow
// without limit across many dynamic region variants.
const REGION_CACHE_MAX = parseInt(process.env.KRATE_SSR_MAX_CACHE || "", 10) || 512;

function paramsKeyOf(params?: Record<string, string>): string {
  return variantKey({ route: "", params });
}

function regionCacheSet(key: string, html: string, page: ManifestPage, req: RenderRequest, scope?: CacheScope): void {
  if (regionCache.size >= REGION_CACHE_MAX && !regionCache.has(key)) {
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
  regionRevalidation.clear();
}

function clearRegionCacheForRoute(route: string): void {
  const prefix = route + "::";
  for (const key of Array.from(regionCache.keys())) {
    if (key.startsWith(prefix)) regionCache.delete(key);
  }
}

function regionCacheKey(page: ManifestPage, regionId: string, req: RenderRequest): string {
  return variantKey({ route: page.route + "::" + regionId, params: req.params, query: req.query });
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
): Promise<{ html: string; scope: CacheScope; invalidated: boolean }> {
  const render = loadRuntimeRenderer(region.bundlePath!);
  const out = await withRenderTimeout(
    withCacheScope(page.route, () => render(JSON.stringify(regionProps(region, req)))),
  );
  return { html: out.result, scope: out.scope, invalidated: out.invalidated };
}

function regionCacheable(r: { scope: CacheScope; invalidated: boolean }): boolean {
  return !r.scope.noStore && !r.invalidated;
}

function revalidateRegionInBackground(page: ManifestPage, region: RegionMeta, req: RenderRequest, key: string) {
  if (inFlightRegionRevalidations.has(key)) return;
  const p = (async () => {
    try {
      const r = await renderRegionFresh(page, region, req);
      if (regionCacheable(r)) regionCacheSet(key, r.html, page, req, r.scope);
      console.log(`[krate-ssr] Revalidated region ${key} (SWR)`);
    } catch (err: any) {
      console.error(`[krate-ssr] Region SWR revalidation failed ${key}:`, err.message);
    } finally {
      inFlightRegionRevalidations.delete(key);
    }
  })();
  inFlightRegionRevalidations.set(key, p);
}

// renderRegion renders one region, applying region-level ISR caching for ISR
// pages. Returns an NDJSON frame.
async function renderRegion(page: ManifestPage, region: RegionMeta, req: RenderRequest): Promise<Record<string, any>> {
  if (!region.bundlePath) {
    return { type: "error", id: region.id, status: 400, error: "region has no runtime bundle" };
  }

  const key = regionCacheKey(page, region.id, req);
  const interval = regionInterval(page, region);
  const cacheable = interval > 0;

  if (cacheable) {
    regionRevalidation.set(key, interval);
    const cached = regionCache.get(key);
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
    const r = await renderRegionFresh(page, region, req);
    const html = r.html;
    if (cacheable && regionCacheable(r)) {
      regionCacheSet(key, html, page, req, r.scope);
    }
    const frame: Record<string, any> = { type: "region", id: region.id, status: 200, html };
    if (cacheable) frame.cacheStatus = "miss";
    return frame;
  } catch (err: any) {
    console.error(`[krate-ssr] Region render failed ${page.route}::${region.id}:`, err.message);
    return { type: "error", id: region.id, status: 500, error: err.message };
  }
}

function writeJsonLine(res: http.ServerResponse, obj: unknown) {
  res.write(JSON.stringify(obj) + "\n");
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
  if (result.redirect) frame.redirect = result.redirect;
  // Title swap: the Go server replaces the baked <title> with the freshly
  // rendered one so params-driven pages update the browser tab.
  const title = extractTitle(result.html);
  if (title !== "") frame.title = title;
  return frame;
}

function extractTitle(html: string): string {
  const match = html.match(/<title[^>]*>([\s\S]*?)<\/title>/i);
  return match ? match[1].trim() : "";
}

// Internal operations (revalidation, refresh, dev invalidation)

type InternalOp = "revalidate-route" | "refresh-route" | "dev-invalidate" | "cache-invalidate";

const INTERNAL_OPS: Record<string, InternalOp> = {
  "/__krate/ssr/revalidate": "revalidate-route",
  "/__krate/ssr/refresh": "refresh-route",
  "/__krate/ssr/invalidate": "dev-invalidate",
  "/__krate/ssr/cache-invalidate": "cache-invalidate",
};

interface InvalidateTarget {
  route: string;
  params?: Record<string, string> | null;
}

function stringList(v: unknown): string[] {
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string" && x !== "") : [];
}

// applyInternalOp applies one internal operation to this process's caches.
async function applyInternalOp(op: InternalOp, payload: any): Promise<Record<string, any>> {
  switch (op) {
    case "revalidate-route": {
      const targetRoute = String(payload.route || "");
      isrCache.deleteRoute(targetRoute);
      clearRegionCacheForRoute(targetRoute);
      cacheRuntime().invalidate({});
      persistIsrNow();
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
    case "dev-invalidate": {
      const { route: targetRoute, source, bundlePath } = payload;
      // Invalidate ISR cache for this route (all variants)
      isrCache.deleteRoute(targetRoute);
      // Invalidate module cache for the source file or bundle
      const key = bundlePath || source;
      if (key && moduleCache.has(key)) {
        moduleCache.delete(key);
        console.log(`[krate-ssr] Invalidated cache for ${targetRoute} (${key})`);
      }
      // Runtime component bundles are recompiled in place on a dev rebuild, but
      // their render functions are cached for the process lifetime. Clear them
      // so the next region render picks up the new code. Bundles are tiny and
      // re-read on demand, so clearing all of them is cheap and avoids tracking
      // which component maps to which route here.
      runtimeBundleCache.clear();
      clearRegionCache();
      return { ok: true };
    }
    case "cache-invalidate": {
      const tags = stringList(payload.tags);
      const targets: InvalidateTarget[] = Array.isArray(payload.targets)
        ? payload.targets.filter((t: any) => t && typeof t.route === "string")
        : [];
      const { dataEntries } = cacheRuntime().invalidate({ tags });
      let isrEntries = 0;
      let regionEntries = 0;
      for (const t of targets) {
        const params = t.params && typeof t.params === "object" ? t.params : null;
        isrEntries += params ? isrCache.deleteVariant(t.route, params) : isrCache.deleteRoute(t.route);
        regionEntries += invalidateRegions(t.route, params, []);
      }
      if (tags.length) {
        isrEntries += isrCache.deleteTags(tags);
        regionEntries += invalidateRegions(null, null, tags);
      }
      if (isrEntries > 0) persistIsrNow();
      return { ok: true, isrEntries, regionEntries, dataEntries };
    }
  }
}

// Cluster fan-out (worker side). broadcastInternalOp resolves with one result
// per live worker once every worker applied the operation.
let broadcastSeq = 0;
const broadcastWaiters = new Map<string, (results: Record<string, any>[]) => void>();

if (cluster.isWorker) {
  process.on("message", async (msg: any) => {
    if (!msg || typeof msg !== "object") return;
    if (msg.type === "krate:apply") {
      let result: Record<string, any>;
      try {
        result = await applyInternalOp(msg.op, msg.payload || {});
      } catch (err: any) {
        result = { ok: false, error: err && err.message ? err.message : String(err) };
      }
      process.send!({ type: "krate:applied", id: msg.id, result });
    } else if (msg.type === "krate:bcast-done") {
      const done = broadcastWaiters.get(msg.id);
      if (done) {
        broadcastWaiters.delete(msg.id);
        done(Array.isArray(msg.results) ? msg.results : []);
      }
    }
  });
}

function broadcastInternalOp(op: InternalOp, payload: any): Promise<Record<string, any>[]> {
  if (!cluster.isWorker || typeof process.send !== "function") {
    return applyInternalOp(op, payload).then((r) => [r]);
  }
  const id = `${process.pid}-${++broadcastSeq}`;
  return new Promise((resolve) => {
    broadcastWaiters.set(id, resolve);
    process.send!({ type: "krate:bcast", id, op, payload });
  });
}

function mergeOpResults(op: InternalOp, results: Record<string, any>[]): Record<string, any> {
  const failed = results.filter((r) => r && r.ok === false);
  if (failed.length > 0) {
    throw new Error(failed.map((r) => r.error).join("; "));
  }
  const out: Record<string, any> = { ok: true, workers: results.length };
  if (op === "refresh-route") out.refreshed = results.reduce((n, r) => n + (r.refreshed || 0), 0);
  if (op === "cache-invalidate") {
    for (const k of ["isrEntries", "regionEntries", "dataEntries"]) {
      out[k] = results.reduce((n, r) => n + (r[k] || 0), 0);
    }
  }
  return out;
}

// HTTP Server

const PORT = parseInt(process.env.KRATE_SSR_PORT || "3100", 10);
const manifestPath = process.env.KRATE_MANIFEST || "";
const root = process.env.KRATE_ROOT || process.cwd();
projectRoot = root;

// Load manifest
if (manifestPath && fs.existsSync(manifestPath)) {
  const data = fs.readFileSync(manifestPath, "utf-8");
  manifest = JSON.parse(data);
  console.log(`[krate-ssr] Loaded manifest: ${manifest!.pages.length} SSR/ISR/streaming pages`);
} else {
  // Try default location
  const defaultPath = path.join(root, "dist", "server-manifest.json");
  if (fs.existsSync(defaultPath)) {
    const data = fs.readFileSync(defaultPath, "utf-8");
    manifest = JSON.parse(data);
    console.log(`[krate-ssr] Loaded manifest: ${manifest!.pages.length} SSR/ISR/streaming pages`);
  } else {
    console.error("[krate-ssr] No manifest found. SSR pages will not be served.");
  }
}

loadIsrCache();

function parseBody(req: http.IncomingMessage): Promise<string> {
  return new Promise((resolve) => {
    let body = "";
    req.on("data", (chunk) => (body += chunk));
    req.on("end", () => resolve(body));
  });
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url || "/", `http://${req.headers.host || "localhost"}`);
  const route = url.pathname;

  // Health check
  if (route === "/__krate/ssr/health") {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ ok: true, pages: manifest?.pages?.length || 0 }));
    return;
  }

  // Mutating internal endpoints. Each requires the host's shared secret and is
  // applied on every clustered worker before the response is sent.
  //   /__krate/ssr/revalidate        {route}  clear a route, re-render its base variant
  //   /__krate/ssr/refresh           {route}  re-render cached variants in place (ISR timer)
  //   /__krate/ssr/invalidate        {route, source, bundlePath}  dev module reload
  //   /__krate/ssr/cache-invalidate  {targets:[{route, params?}], tags:[]}  on-demand revalidation
  const internalOp = INTERNAL_OPS[route];
  if (internalOp) {
    if (req.method !== "POST") {
      res.writeHead(405, { "Content-Type": "application/json", Allow: "POST" });
      res.end(JSON.stringify({ error: "method not allowed" }));
      return;
    }
    if (!internalAuthorized(req)) {
      res.writeHead(403, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "forbidden" }));
      return;
    }
    try {
      const payload = JSON.parse((await parseBody(req)) || "{}");
      const results = await broadcastInternalOp(internalOp, payload);
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify(mergeOpResults(internalOp, results)));
    } catch (err: any) {
      res.writeHead(500, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: err.message }));
    }
    return;
  }

  // /__krate/regions - render ONLY a page's dynamic regions
  // One NDJSON frame per region, written as its own chunk so the Go server
  // forwards each region progressively and splices it into the static shell.
  // The Go server sends the splice markers it parsed from the shell; the
  // sidecar renders each one (a component region from the manifest, or a
  // coarse "page" region that renders the whole page component for SSR/ISR).
  if (route === "/__krate/regions" && req.method === "POST") {
    res.writeHead(200, {
      "Content-Type": "application/x-ndjson; charset=utf-8",
      "Cache-Control": "no-store",
      "X-Content-Type-Options": "nosniff",
    });
    setupStream(res);

    try {
      const body = await parseBody(req);
      const parsed = JSON.parse(body) as RenderRequest;
      const renderReq: RenderRequest = {
        route: parsed.route,
        url: parsed.url || "/",
        method: parsed.method || "GET",
        headers: parsed.headers || {},
        params: parsed.params,
        query: parsed.query,
        regions: parsed.regions,
      };

      const page = findPage(renderReq.route);
      if (!page) {
        writeJsonLine(res, { type: "error", status: 404, error: "no such page" });
        writeJsonLine(res, { type: "end", count: 0 });
        res.end();
        return;
      }

      // Explicit splice list from Go wins. When absent (older Go server or
      // direct calls), fall back to the manifest's component regions.
      let regions = renderReq.regions || [];
      if (regions.length === 0) {
        regions = (manifest?.regions?.[page.route] || []).map((r) => ({ id: r.id, kind: "component" as const }));
      }

      for (const r of regions) {
        if (r.kind === "page") {
          writeJsonLine(res, await renderPageRegion(page, r.id, renderReq));
        } else {
          const region = (manifest?.regions?.[page.route] || []).find((m) => m.id === r.id);
          if (region) {
            writeJsonLine(res, await renderRegion(page, region, renderReq));
          } else {
            // Unknown region id - keep the baked content by sending no frame.
            writeJsonLine(res, { type: "skip", id: r.id });
          }
        }
      }
      writeJsonLine(res, { type: "end", count: regions.length });
    } catch (err: any) {
      console.error("[krate-ssr] Error in /__krate/regions:", err.message);
      writeJsonLine(res, { type: "error", status: 500, error: err.message });
    }
    res.end();
    return;
  }

  // Direct page requests (non-proxied)
  // Collect request headers
  const headers: Record<string, string> = {};
  for (const [key, value] of Object.entries(req.headers)) {
    if (typeof value === "string") headers[key] = value;
  }

  const renderReq: RenderRequest = {
    route,
    url: req.url || "/",
    method: req.method || "GET",
    headers,
  };

  const result = await renderPage(renderReq);

  if (result.notFound) {
    // Try to serve 404.html from dist
    const notFoundPath = path.join(root, "dist", "404.html");
    if (fs.existsSync(notFoundPath)) {
      const html = fs.readFileSync(notFoundPath, "utf-8");
      res.writeHead(404, { "Content-Type": "text/html; charset=utf-8" });
      res.end(html);
    } else {
      res.writeHead(404, { "Content-Type": "text/html; charset=utf-8" });
      res.end("<html><body><h1>404 Not Found</h1></body></html>");
    }
    return;
  }

  if (result.redirect) {
    res.writeHead(302, { Location: result.redirect });
    res.end();
    return;
  }

  if (result.status === 500) {
    // Try to serve 500.html from dist
    const errorPath = path.join(root, "dist", "500.html");
    if (fs.existsSync(errorPath)) {
      const html = fs.readFileSync(errorPath, "utf-8");
      res.writeHead(500, { "Content-Type": "text/html; charset=utf-8" });
      res.end(html);
    } else {
      res.writeHead(500, { "Content-Type": "text/html; charset=utf-8" });
      res.end("<html><body><h1>Internal Server Error</h1></body></html>");
    }
    return;
  }

  // Regular SSR/ISR response
  const headers_extra: Record<string, string> = {
    "Content-Type": "text/html; charset=utf-8",
  };
  if (result.cached) {
    headers_extra["X-Krate-Cache"] = result.cacheStatus === "stale" ? "STALE" : "HIT";
  }
  res.writeHead(result.status, headers_extra);
  res.end(result.html);
});

if (isClusterPrimary) {
  // The primary only supervises; workers own the listening socket.
} else {
  // The port was free when Go chose it, but a bind can still fail with
  // EADDRINUSE: any socket holding that local port blocks the wildcard bind,
  // including a short-lived outgoing connection. A clustered worker exits
  // quietly so the primary respawns it after a delay (listening again after a
  // failed cluster listen trips a Node assertion on Windows); a standalone
  // server retries the bind for a few seconds.
  let bindRetries = 100;
  server.on("error", (err: NodeJS.ErrnoException) => {
    if (err.code === "EADDRINUSE") {
      if (cluster.isWorker) process.exit(1);
      if (bindRetries-- > 0) {
        setTimeout(() => {
          try {
            server.close();
          } catch {
            // not listening yet
          }
          server.listen(PORT);
        }, 100);
        return;
      }
    }
    throw err;
  });
  server.listen(PORT, () => {
    console.log(`[krate-ssr] Renderer server listening on port ${PORT}`);
  });
}