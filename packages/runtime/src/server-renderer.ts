// @krate/runtime/server-renderer - Node.js HTTP server for SSR/ISR/Streaming
// Receives render requests from the Go server and returns HTML.

import http from "node:http";
import cluster from "node:cluster";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { pathToFileURL } from "node:url";
import { AsyncLocalStorage } from "node:async_hooks";
import { createHash, timingSafeEqual } from "node:crypto";
import {
  createSSRCore,
  MemoryIsrStore,
  type ManifestPage,
  type RegionMeta,
  type RenderRequest,
  type ServerManifest,
} from "./ssr-core.js";
import { installRenderStore, patchFetch, type CacheScope } from "./cache-scope.js";

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
  // Node leaves scheduling to the OS on Windows (SCHED_NONE), where one
  // worker tends to accept every connection. Round-robin keeps clustered
  // caches - and therefore revalidation fan-out - reachable on every platform.
  cluster.schedulingPolicy = cluster.SCHED_RR;
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


// Configuration and manifest

const PORT = parseInt(process.env.KRATE_SSR_PORT || "3100", 10);
const manifestPath = process.env.KRATE_MANIFEST || "";
const root = process.env.KRATE_ROOT || process.cwd();
const OUT_DIR = process.env.KRATE_OUT_DIR || path.join(root, "dist");

// Exit when the Krate host dies: a SIGKILL or OOM kill gives it no chance to stop its sidecars.
const PARENT_PID = parseInt(process.env.KRATE_PARENT_PID || "", 10);
if (PARENT_PID > 0) {
  const parentWatch = setInterval(() => {
    try {
      process.kill(PARENT_PID, 0);
    } catch (e: any) {
      if (!e || e.code !== "EPERM") process.exit(0);
    }
  }, 2000);
  parentWatch.unref();
}

let manifest: ServerManifest | null = null;

if (manifestPath && fs.existsSync(manifestPath)) {
  const data = fs.readFileSync(manifestPath, "utf-8");
  manifest = JSON.parse(data);
  console.log(`[krate-ssr] Loaded manifest: ${manifest!.pages.length} SSR/ISR/streaming pages`);
} else {
  // Try default location
  const krateDefault = path.join(OUT_DIR, ".krate", "server-manifest.json");
  const defaultPath = fs.existsSync(krateDefault) ? krateDefault : path.join(OUT_DIR, "server-manifest.json");
  if (fs.existsSync(defaultPath)) {
    const data = fs.readFileSync(defaultPath, "utf-8");
    manifest = JSON.parse(data);
    console.log(`[krate-ssr] Loaded manifest: ${manifest!.pages.length} SSR/ISR/streaming pages`);
  } else {
    console.error("[krate-ssr] No manifest found. SSR pages will not be served.");
  }
}


// Module loaders (node:fs / node:vm live here, not in the render core)

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
    const bundleAbs = path.resolve(OUT_DIR, page.bundlePath);
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

const runtimeBundleCache = new Map<string, (propsJSON: string) => string>();

// loadRuntimeRenderer loads a compiled runtime component bundle and returns its
// __krate_render(propsJSON) function. Bundles are cached by bundle path.
function loadRuntimeRenderer(relPath: string): (propsJSON: string) => string {
  const cached = runtimeBundleCache.get(relPath);
  if (cached) return cached;

  const abs = path.resolve(OUT_DIR, relPath);
  const code = fs.readFileSync(abs, "utf-8");
  // Runtime components get the host's fetch (looked up per call, so a cache
  // layer that wraps globalThis.fetch later still applies) and the web
  // primitives that go with it.
  const ctx = vm.createContext({
    console,
    fetch: (...args: Parameters<typeof fetch>) => globalThis.fetch(...args),
    Headers, Request, Response, URL, URLSearchParams, TextEncoder, TextDecoder,
    setTimeout, clearTimeout, queueMicrotask, structuredClone, AbortController,
  });
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

// Render core

const pprEnabledEnv = process.env.KRATE_PPR === "1";
const isrMemory = new MemoryIsrStore(parseInt(process.env.KRATE_SSR_MAX_CACHE || "", 10) || 512);
const core = createSSRCore({
  manifest,
  sha256Hex: (input) => createHash("sha256").update(input).digest("hex"),
  isrStore: isrMemory,
  loadPageModule,
  loadRuntimeRenderer,
  maxCacheSize: parseInt(process.env.KRATE_SSR_MAX_CACHE || "", 10) || 512,
  timeoutMs: parseInt(process.env.KRATE_SSR_TIMEOUT || "", 10) || 5000,
  ppr: pprEnabledEnv,
  pprRevalidate: parseInt(process.env.KRATE_PPR_REVALIDATE || "", 10) || 60,
  onIsrChange: (immediate) => (immediate ? persistIsrNow() : scheduleIsrPersist()),
});
const isrCache = isrMemory.cache;
const { findPage, renderPage, renderRegion, renderPageRegion } = core;

// ISR cache persistence - survives renderer restarts so a bounce doesn't
// cold-render every ISR variant. Written debounced (coalesced) to the build
// output dir (.krate/isr-cache.json), overridable via KRATE_ISR_CACHE.
let isrCacheFile = process.env.KRATE_ISR_CACHE || "";
let persistTimer: NodeJS.Timeout | null = null;

function isrCachePath(): string {
  return isrCacheFile || path.join(OUT_DIR, ".krate", "isr-cache.json");
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
loadIsrCache();

// Internal operations (revalidation, refresh, dev invalidation)

type InternalOp = "revalidate-route" | "refresh-route" | "dev-invalidate" | "cache-invalidate";

const INTERNAL_OPS: Record<string, InternalOp> = {
  "/__krate/ssr/revalidate": "revalidate-route",
  "/__krate/ssr/refresh": "refresh-route",
  "/__krate/ssr/invalidate": "dev-invalidate",
  "/__krate/ssr/cache-invalidate": "cache-invalidate",
};

// applyInternalOp applies one internal operation to this process's caches.
async function applyInternalOp(op: InternalOp, payload: any): Promise<Record<string, any>> {
  if (op !== "dev-invalidate") return core.applyInternalOp(op, payload);
  const { route: targetRoute, source, bundlePath } = payload;
  // Invalidate the page and region caches for this route, then the module
  // cache for the source file or bundle.
  const key = bundlePath || source;
  if (key && moduleCache.has(key)) {
    moduleCache.delete(key);
    console.log(`[krate-ssr] Invalidated cache for ${targetRoute} (${key})`);
  }
  // Runtime component bundles are recompiled in place on a dev rebuild, but
  // their render functions are cached for the process lifetime. Clear them
  // so the next region render picks up the new code.
  runtimeBundleCache.clear();
  core.devInvalidate(targetRoute);
  return { ok: true };
}

function writeJsonLine(res: http.ServerResponse, obj: unknown) {
  res.write(JSON.stringify(obj) + "\n");
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
    if (!internalAuthorized(req)) {
      res.writeHead(403, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "forbidden" }));
      return;
    }
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

      // Start every region render at once (async server components await
      // I/O, so they overlap), then write the frames in document order.
      const pending = regions.map((r): Promise<Record<string, any>> => {
        if (r.kind === "page") return renderPageRegion(page, r.id, renderReq);
        const region = (manifest?.regions?.[page.route] || []).find((m) => m.id === r.id);
        if (region) return renderRegion(page, region, renderReq);
        // Unknown region id - keep the baked content by sending no frame.
        return Promise.resolve({ type: "skip", id: r.id });
      });
      // Write each frame as soon as it resolves. Async server components finish
      // out of document order, and the Go host inserts each chunk at its marker.
      await Promise.all(pending.map(async (p) => writeJsonLine(res, await p)));
      writeJsonLine(res, { type: "end", count: regions.length });
    } catch (err: any) {
      console.error("[krate-ssr] Error in /__krate/regions:", err.message);
      writeJsonLine(res, { type: "error", status: 500, error: err.message });
    }
    res.end();
    return;
  }

  // Direct page requests (non-proxied)
  if (!internalAuthorized(req)) {
    res.writeHead(403, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: "forbidden" }));
    return;
  }
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
    const notFoundPath = path.join(OUT_DIR, "404.html");
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
    res.writeHead(result.status === 307 ? 307 : 302, { Location: result.redirect });
    res.end();
    return;
  }

  if (result.status === 500) {
    // Try to serve 500.html from dist
    const errorPath = path.join(OUT_DIR, "500.html");
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
  // A direct page response has no document to merge into: <Head> children stay
  // where they rendered, without their markers.
  res.end(result.html.replace(/<!--\/?krate-head-->/g, ""));
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
          server.listen(PORT, "127.0.0.1");
        }, 100);
        return;
      }
    }
    throw err;
  });
  server.listen(PORT, "127.0.0.1", () => {
    console.log(`[krate-ssr] Renderer server listening on port ${PORT}`);
  });
}