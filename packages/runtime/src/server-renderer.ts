// @krate/runtime/server-renderer — Node.js HTTP server for SSR/ISR/Streaming
// Receives render requests from the Go server and returns HTML.

import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { renderToString, setStreamingResolved, resetBoundaryCounter } from "./server.js";

// Force TCP_NODELAY on socket to prevent Nagle buffering chunks together.
// Must be called BEFORE the first write for it to take effect.
function setupStream(res: http.ServerResponse) {
  const socket = (res as any).socket;
  if (socket && typeof socket.setNoDelay === 'function') {
    socket.setNoDelay(true);
  }
}

// ── Types ────────────────────────────────────────────────────────────────────

interface ManifestPage {
  route: string;
  source: string;
  mode: string;
  revalidate?: number;
  bundlePath?: string;
}

interface ServerManifest {
  pages: ManifestPage[];
  stylesheet?: string;
  runtimeJS?: string;
}

interface CacheEntry {
  html: string;
  timestamp: number;
  headHTML?: string;
  scriptHTML?: string;
}

interface RenderRequest {
  route: string;
  url: string;
  method: string;
  headers: Record<string, string>;
  params?: Record<string, string>;
  query?: Record<string, string>;
}

interface RenderResponse {
  html: string;
  status: number;
  headHTML?: string;
  scriptHTML?: string;
  redirect?: string;
  notFound?: boolean;
  cached?: boolean;
}

// ── ISR Cache ────────────────────────────────────────────────────────────────

class ISRCache {
  private cache = new Map<string, CacheEntry>();
  private maxSize: number;
  private revalidationIntervals = new Map<string, number>(); // route → seconds

  constructor(maxSize = 128) {
    this.maxSize = maxSize;
  }

  setRevalidation(route: string, seconds: number) {
    this.revalidationIntervals.set(route, seconds);
  }

  get(route: string): CacheEntry | undefined {
    return this.cache.get(route);
  }

  set(route: string, entry: CacheEntry) {
    if (this.cache.size >= this.maxSize) {
      const oldest = this.cache.keys().next().value;
      if (oldest) this.cache.delete(oldest);
    }
    this.cache.set(route, entry);
  }

  isStale(route: string): boolean {
    const entry = this.cache.get(route);
    const interval = this.revalidationIntervals.get(route);
    if (!entry || !interval) return false;
    return (Date.now() - entry.timestamp) / 1000 > interval;
  }

  delete(route: string) {
    this.cache.delete(route);
  }
}

// ── Page Module Loader ───────────────────────────────────────────────────────

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

// ── Renderer ─────────────────────────────────────────────────────────────────

const isrCache = new ISRCache();
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

async function renderPage(req: RenderRequest): Promise<RenderResponse> {
  const page = findPage(req.route);
  if (!page) {
    return { html: "", status: 404, notFound: true };
  }

  // ISR: serve from cache if available and not stale
  if (page.mode === "isr") {
    isrCache.setRevalidation(page.route, page.revalidate || 60);
    const cached = isrCache.get(page.route);
    if (cached && !isrCache.isStale(page.route)) {
      return {
        html: cached.html,
        status: 200,
        headHTML: cached.headHTML,
        scriptHTML: cached.scriptHTML,
        cached: true,
      };
    }
  }

  try {
    const mod = await loadPageModule(page);

    // Page-level data fetching (getStaticProps/getServerSideProps) has been
    // removed; per-request data is provided by server components (@server),
    // runtime components (@runtime), and middleware instead. The default export
    // is rendered here with no injected page props.
    const props: Record<string, any> = {};

    // Get the default export (the component)
    const Component = mod.default;
    if (!Component) {
      return { html: "", status: 500 };
    }

    // Render the component to HTML
    const jsxNode = Component(props);
    const html = renderToString(jsxNode);

    // Extract head/script content from the render
    const headHTML = extractHeadHTML(html);
    const scriptHTML = extractScriptHTML(html);

    const response: RenderResponse = {
      html,
      status: 200,
      headHTML,
      scriptHTML,
    };

    // ISR: cache the result
    if (page.mode === "isr") {
      isrCache.set(page.route, {
        html,
        timestamp: Date.now(),
        headHTML,
        scriptHTML,
      });
    }

    return response;
  } catch (err: any) {
    console.error(`[krate] Error rendering ${req.route}:`, err);
    return { html: "", status: 500 };
  }
}

function extractHeadHTML(html: string): string {
  const match = html.match(/<!--head-start-->(.*?)<!--head-end-->/s);
  return match ? match[1] : "";
}

function extractScriptHTML(html: string): string {
  const match = html.match(/<!--script-start-->(.*?)<!--script-end-->/s);
  return match ? match[1] : "";
}

// ── HTTP Server ──────────────────────────────────────────────────────────────

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
    let body = "";
    for await (const chunk of req) body += chunk;
    try {
      const { route: targetRoute } = JSON.parse(body);
      isrCache.delete(targetRoute);
      // Trigger re-render
      const page = findPage(targetRoute);
      if (page) {
        await renderPage({
          route: targetRoute,
          url: targetRoute,
          method: "GET",
          headers: {},
        });
      }
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ ok: true }));
    } catch (err: any) {
      res.writeHead(500, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: err.message }));
    }
    return;
  }

  // Module cache invalidation (called on file change in dev mode)
  if (route === "/__krate/ssr/invalidate" && req.method === "POST") {
    let body = "";
    for await (const chunk of req) body += chunk;
    try {
      const { route: targetRoute, source, bundlePath } = JSON.parse(body);
      // Invalidate ISR cache for this route
      isrCache.delete(targetRoute);
      // Invalidate module cache for the source file or bundle
      const key = bundlePath || source;
      if (key && moduleCache.has(key)) {
        moduleCache.delete(key);
        console.log(`[krate-ssr] Invalidated cache for ${targetRoute} (${key})`);
      }
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ ok: true }));
    } catch (err: any) {
      res.writeHead(500, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: err.message }));
    }
    return;
  }

  // ── /__krate/render — called by Go server for SSR/ISR/Streaming ──────────
  if (route === "/__krate/render" && req.method === "POST") {
    let body = "";
    for await (const chunk of req) body += chunk;
    try {
      const parsed = JSON.parse(body) as RenderRequest;
      const renderReq: RenderRequest = {
        route: parsed.route,
        url: parsed.url || "/",
        method: parsed.method || "GET",
        headers: parsed.headers || {},
        params: parsed.params,
        query: parsed.query,
      };

      const page = findPage(renderReq.route);

      // Streaming mode: two-phase render for real Suspense fallback
      if (page?.mode === "streaming") {
        res.writeHead(200, {
          "Content-Type": "text/html; charset=utf-8",
          "Transfer-Encoding": "chunked",
          "X-Content-Type-Options": "nosniff",
        });
        setupStream(res);

        const mod = await loadPageModule(page);
        const Component = mod.default;
        if (!Component) {
          res.end();
          return;
        }

        // Phase 1: Render with empty props → Suspense shows fallback
        resetBoundaryCounter();
        setStreamingResolved(false);
        const fallbackJsx = Component({});
        const fallbackHtml = renderToString(fallbackJsx);
        res.write(fallbackHtml);

        // Phase 2: Re-render with resolved props (no data-fetching needed)
        resetBoundaryCounter();
        setStreamingResolved(true);
        const resolvedJsx = Component({});
        const resolvedHtml = renderToString(resolvedJsx);
        setStreamingResolved(false);

        // Extract resolved content per Suspense boundary from markers
        const resolvedMap: Record<string, string> = {};
        const markerRe = /<!--suspense-resolved:(\d+)-->([\s\S]*?)<!--\/suspense-resolved:\1-->/g;
        let m: RegExpExecArray | null;
        while ((m = markerRe.exec(resolvedHtml)) !== null) {
          resolvedMap[m[1]] = m[2];
        }

        // Send targeted replacement script — only replaces fallback spans, preserves surrounding HTML
        const script = `<script>(function(){var m=${JSON.stringify(resolvedMap)};Object.keys(m).forEach(function(id){var s=document.querySelector('span[data-suspense="'+id+'"]');if(s)s.outerHTML=m[id]});var t=document.querySelectorAll('template[data-suspense]');t.forEach(function(el){el.remove()})})()</script>`;
        res.write(script);
        res.end();
        return;
      }

      // SSR/ISR: standard single-phase render
      const result = await renderPage(renderReq);

      // Return JSON response
      res.writeHead(result.status, {
        "Content-Type": "application/json; charset=utf-8",
      });
      res.end(JSON.stringify(result));
    } catch (err: any) {
      console.error("[krate-ssr] Error in /__krate/render:", err.message);
      if (res.headersSent) {
        // Headers already sent (e.g. streaming mode) — write error inline
        res.write(`<script>console.error("[krate-ssr]",${JSON.stringify(err.message)})</script>`);
        res.end();
      } else {
        res.writeHead(500, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ html: "", status: 500 }));
      }
    }
    return;
  }

  // ── Direct page requests (non-proxied) ──────────────────────────────────
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

  // Streaming SSR: two-phase render for direct requests
  const streamingPage = manifest?.pages?.find((p) => p.route === route);
  if (streamingPage?.mode === "streaming") {
    try {
      const mod = await loadPageModule(streamingPage);
      const Component = mod.default;
      if (!Component) {
        res.writeHead(500, { "Content-Type": "text/html; charset=utf-8" });
        res.end("<html><body><h1>500</h1></body></html>");
        return;
      }

      res.writeHead(200, {
        "Content-Type": "text/html; charset=utf-8",
        "Transfer-Encoding": "chunked",
        "X-Content-Type-Options": "nosniff",
      });
      setupStream(res);

      // Phase 1: fallback
      resetBoundaryCounter();
      setStreamingResolved(false);
      const fallbackHtml = renderToString(Component({}));
      res.write(fallbackHtml);

      // Phase 2: re-render with resolved props (no data-fetching needed)
      resetBoundaryCounter();
      setStreamingResolved(true);
      const resolvedHtml = renderToString(Component({}));
      setStreamingResolved(false);

      // Extract resolved content per Suspense boundary from markers
      const resolvedMap: Record<string, string> = {};
      const markerRe = /<!--suspense-resolved:(\d+)-->([\s\S]*?)<!--\/suspense-resolved:\1-->/g;
      let m: RegExpExecArray | null;
      while ((m = markerRe.exec(resolvedHtml)) !== null) {
        resolvedMap[m[1]] = m[2];
      }

      const script = `<script>(function(){var m=${JSON.stringify(resolvedMap)};Object.keys(m).forEach(function(id){var s=document.querySelector('span[data-suspense="'+id+'"]');if(s)s.outerHTML=m[id]});var t=document.querySelectorAll('template[data-suspense]');t.forEach(function(el){el.remove()})})()</script>`;
      res.write(script);
      res.end();
      return;
    } catch (err: any) {
      console.error("[krate-ssr] Error in streaming direct render:", err.message);
      if (res.headersSent) {
        res.write(`<script>console.error("[krate-ssr]",${JSON.stringify(err.message)})</script>`);
        res.end();
      } else {
        res.writeHead(500, { "Content-Type": "text/html; charset=utf-8" });
        res.end("<html><body><h1>Internal Server Error</h1></body></html>");
      }
      return;
    }
  }

  // Regular SSR/ISR response
  const headers_extra: Record<string, string> = {
    "Content-Type": "text/html; charset=utf-8",
  };
  if (result.cached) {
    headers_extra["X-Krate-Cache"] = "HIT";
  }
  res.writeHead(result.status, headers_extra);
  res.end(result.html);
});

server.listen(PORT, () => {
  console.log(`[krate-ssr] Renderer server listening on port ${PORT}`);
});
