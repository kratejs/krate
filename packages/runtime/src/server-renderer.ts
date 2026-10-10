// @krate/runtime/server-renderer - Node.js HTTP server for SSR/ISR/Streaming
// Receives render requests from the Go server and returns HTML.

import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { pathToFileURL } from "node:url";
import { renderToString } from "./server.js";

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
    if (this.cache.size >= this.maxSize) {
      const oldest = this.cache.keys().next().value;
      if (oldest) this.cache.delete(oldest);
    }
    this.cache.set(key, entry);
  }

  isStale(key: string): boolean {
    const entry = this.cache.get(key);
    const route = key.split("?")[0].split("#")[0];
    const interval = this.revalidationIntervals.get(route);
    if (!entry || !interval) return false;
    return (Date.now() - entry.timestamp) / 1000 > interval;
  }

  // deleteRoute removes every variant of a route (used by revalidation/invalidation).
  deleteRoute(route: string) {
    for (const key of [...this.cache.keys()]) {
      if (key === route || key.startsWith(route + "?") || key.startsWith(route + "#")) {
        this.cache.delete(key);
      }
    }
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
        this.cache.set(e.key, {
          html: e.html,
          timestamp: typeof e.timestamp === "number" ? e.timestamp : Date.now(),
          params: e.params && typeof e.params === "object" ? e.params : undefined,
          query: e.query && typeof e.query === "object" ? e.query : undefined,
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

function scheduleIsrPersist() {
  if (persistTimer) clearTimeout(persistTimer);
  persistTimer = setTimeout(() => {
    persistTimer = null;
    try {
      const data = isrCache.serialize();
      const file = isrCachePath();
      fs.mkdirSync(path.dirname(file), { recursive: true });
      fs.writeFileSync(file, JSON.stringify(data));
    } catch (err: any) {
      console.error("[krate-ssr] ISR cache persist failed:", err.message);
    }
  }, 1000);
}

function loadIsrCache() {
  try {
    const file = isrCachePath();
    if (fs.existsSync(file)) {
      const data = JSON.parse(fs.readFileSync(file, "utf-8"));
      isrCache.hydrate(data);
      const n = (isrCache as any).cache.size;
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

    const jsxNode = Component(buildProps(req));
    // Bound the render. renderToString is synchronous, so a page that blocks
    // would block the sidecar's event loop regardless; the timeout still
    // protects the request path when rendering is async (suspense/resource).
    const html = await withRenderTimeout(Promise.resolve(renderToString(jsxNode)));

    const response: RenderResponse = {
      html,
      status: 200,
    };

    if (page.mode === "isr") {
      isrCache.set(variantKey(req), {
        html,
        timestamp: Date.now(),
        params: req.params,
        query: req.query,
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
const regionCache = new Map<string, { html: string; timestamp: number }>();
const regionRevalidation = new Map<string, number>();
const inFlightRegionRevalidations = new Map<string, Promise<void>>();

// Region cache is bounded (FIFO eviction) so a long-lived sidecar cannot grow
// without limit across many dynamic region variants.
const REGION_CACHE_MAX = parseInt(process.env.KRATE_SSR_MAX_CACHE || "", 10) || 512;

function regionCacheSet(key: string, html: string): void {
  if (regionCache.size >= REGION_CACHE_MAX && !regionCache.has(key)) {
    const oldest = regionCache.keys().next().value;
    if (oldest !== undefined) regionCache.delete(oldest);
  }
  regionCache.set(key, { html, timestamp: Date.now() });
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

function renderRegionFresh(region: RegionMeta, req: RenderRequest): Promise<string> {
  const render = loadRuntimeRenderer(region.bundlePath!);
  return withRenderTimeout(Promise.resolve().then(() => render(JSON.stringify(regionProps(region, req)))));
}

function revalidateRegionInBackground(page: ManifestPage, region: RegionMeta, req: RenderRequest, key: string) {
  if (inFlightRegionRevalidations.has(key)) return;
  const p = (async () => {
    try {
      const html = await renderRegionFresh(region, req);
      regionCacheSet(key, html);
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
    const html = await renderRegionFresh(region, req);
    if (cacheable) {
      regionCacheSet(key, html);
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

  // ISR revalidation endpoint (called by Go server in background)
  if (route === "/__krate/ssr/revalidate" && req.method === "POST") {
    const body = await parseBody(req);
    try {
      const { route: targetRoute } = JSON.parse(body);
      isrCache.deleteRoute(targetRoute);
      clearRegionCacheForRoute(targetRoute);
      // Trigger re-render of the base variant.
      const page = findPage(targetRoute);
      if (page) {
        await renderFresh(page, { route: targetRoute, url: targetRoute, method: "GET", headers: {} });
      }
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ ok: true }));
    } catch (err: any) {
      res.writeHead(500, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: err.message }));
    }
    return;
  }

  // ISR background refresh endpoint (called by the Go server on each route's
  // revalidation cadence). Unlike /__krate/ssr/revalidate this is
  // non-destructive: it re-renders every cached variant in place, so cached
  // dynamic variants stay fresh instead of being evicted (which would make the
  // next request a MISS rather than a HIT).
  if (route === "/__krate/ssr/refresh" && req.method === "POST") {
    const body = await parseBody(req);
    try {
      const { route: targetRoute } = JSON.parse(body);
      const page = findPage(targetRoute);
      const refreshed = page && page.mode === "isr" ? await refreshRouteVariants(page) : 0;
      clearRegionCacheForRoute(targetRoute);
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ ok: true, refreshed }));
    } catch (err: any) {
      res.writeHead(500, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: err.message }));
    }
    return;
  }

  // Module cache invalidation (called on file change in dev mode)
  if (route === "/__krate/ssr/invalidate" && req.method === "POST") {
    const body = await parseBody(req);
    try {
      const { route: targetRoute, source, bundlePath } = JSON.parse(body);
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
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ ok: true }));
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

server.listen(PORT, () => {
  console.log(`[krate-ssr] Renderer server listening on port ${PORT}`);
});