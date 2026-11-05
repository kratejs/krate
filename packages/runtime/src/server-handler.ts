// @krate/runtime/server-handler - the portable Krate server.
//
// createKrateHandler(options) returns a Web-standard (Request) => Promise<Response>
// that does, per request, what the Go host (`krate start`) does: config redirects,
// rewrites and headers, middleware, route matching, static-template pages,
// SSR/ISR/streaming pages with region splicing, JS/TS API routes, server actions,
// no-JS flash notices, security headers and the cache policy. It runs on Node,
// Vercel, Netlify and Cloudflare Workers: there is no Go, no node:fs and no
// node:vm in this module. Everything it needs (build data, page modules, API
// routes, actions, middleware) is handed in by the bundle entry the compiler
// generates (`krate deploy`), and static files come from a pluggable layer.

import { AsyncLocalStorage } from "node:async_hooks";
import {
  createSSRCore,
  MemoryIsrStore,
  type IsrStore,
  type ManifestPage,
  type RegionMeta,
  type RenderRequest,
  type SSRCore,
} from "./ssr-core.js";
import { cacheRuntime, installRenderStore, patchFetch, type RevalidateScope } from "./cache-scope.js";
import {
  allSuspense,
  canStreamOutOfOrder,
  FLASH_COOKIE,
  injectFlashNotice,
  parseRegionShell,
  regionReplaceScript,
  replaceTitle,
  requestFlash,
  spliceShell,
  splitBodyScripts,
  type RegionShell,
} from "./handler-html.js";
import {
  applyDynamicRouteParams,
  baseName,
  cleanPath,
  compareRouteSpecificity,
  compileMatcher,
  extName,
  goRedirect,
  headerSourceMatches,
  isDynamicPattern,
  isFileLikePath,
  isHTMLDocRequest,
  isInternalOutputPath,
  matchRedirect,
  matchRoutePattern,
  normalizeRoutePattern,
  requestRoute,
  rewriteDestination,
  rewriteTargetPath,
  staticCacheControl,
} from "./handler-routing.js";
import {
  lookupSegmentPage,
  newServerError,
  renderSegmentPage,
  type SegmentEntry,
  SEGMENT_KIND_OF_STATUS,
  type SegmentKind,
  type SegmentMatch,
} from "./handler-segments.js";
import { httpFallbackStatus } from "./request-context.js";
import { sha256Hex as portableSha256Hex } from "./sha256.js";
import { createMemoryStaticLayer } from "./static-memory.js";

// Public types

export type DeployTarget = "node" | "vercel" | "netlify" | "cloudflare";

export interface HandlerConfig {
  /** URL prefix the site is mounted under ("" or "/prefix"). */
  basePath: string;
  trustProxy: boolean;
  /** Request body cap for pages and middleware, bytes. */
  maxBodyBytes: number;
  /** Request body cap for API routes and actions, bytes. */
  maxAPIBodyBytes: number;
  redirects: Array<{ source: string; destination: string; permanent?: boolean }>;
  rewrites: Array<{ source: string; destination: string }>;
  headers: Array<{ source: string; headers: Record<string, string> }>;
  /** Resolved security headers; an empty string means "do not send". */
  security: { nosniff: boolean; frameOptions: string; referrerPolicy: string; permissionsPolicy: string; hsts: string; csp: string };
  cors: { enabled: boolean; origins: string[]; methods: string[]; headers: string[]; credentials: boolean; maxAge: number };
  ssr: { timeout: number; maxCacheSize: number; ppr: boolean; pprRevalidate: number };
}

export interface HandlerPage {
  route: string;
  source?: string;
  mode: "ssr" | "isr" | "streaming";
  revalidate?: number;
  bundlePath: string;
  serverComponents?: boolean;
  /** Source directory relative to the pages root, route groups kept (segment page lookup). */
  segDir?: string;
}

export interface ActionEntry {
  id: string;
  export: string;
  file: string;
  source?: string;
}

/** Build data the compiler inlines into the server bundle. */
export interface KrateServerData {
  version: 1;
  config: HandlerConfig;
  /** Request-time pages (server-manifest.json). */
  pages: HandlerPage[];
  regions: Record<string, RegionMeta[]>;
  /** Every page of the build with its render mode (for revalidatePath). */
  allPages: Array<{ route: string; mode: "ssg" | "ssr" | "isr" | "streaming"; segDir?: string }>;
  /** Baked shells of the request-time pages, keyed by manifest route. */
  shells: Record<string, string>;
  /** Dynamic SSG templates; pattern is the output dir ("blog/[slug]"). */
  templates: Array<{ pattern: string; html: string }>;
  staticOnlyRoutes: string[];
  hashedAssets: string[];
  html404: string;
  html500: string;
  /** Directories with their own not-found or error page (manifest segments[]). */
  segments?: SegmentEntry[];
  /** Segment page HTML keyed by its output path ("blog/404.html"). */
  segmentPages?: Record<string, string>;
  /** Prerendered pages (dist-relative path to HTML) the function serves itself after middleware. */
  staticPages?: Record<string, string>;
  /** API route keys: file path under the API dir without extension ("items/[id]"). */
  apiRoutes: string[];
  actions: ActionEntry[];
  hasMiddleware: boolean;
}

/** Module namespaces the compiler imports statically into the bundle. */
export interface KrateServerModules {
  /** Page server bundles keyed by their manifest bundlePath. */
  pages?: Record<string, any>;
  /** Runtime component renderers keyed by region bundlePath. */
  runtime?: Record<string, (propsJSON: string) => string | Promise<string>>;
  /** API route modules keyed by route key. */
  api?: Record<string, any>;
  /** Server action modules keyed by manifest file. */
  actions?: Record<string, any>;
  middleware?: any;
}

/** Where static files come from. Absent on platforms whose CDN serves them first. */
export interface StaticLayer {
  /** A concrete file or folder index exists for the URL path. */
  exists(urlPath: string): boolean | Promise<boolean>;
  /** A non-HTML file exists for the URL path (a page's script, chunk, image). */
  isAsset(urlPath: string): boolean | Promise<boolean>;
  /** Serves the path like http.FileServer; a 404 Response (or null) when missing. */
  serve(request: Request, urlPath: string): Promise<Response | null>;
}

export interface HandlerContext {
  /** Keeps the instance alive for work that outlives the response. */
  waitUntil?(work: Promise<unknown>): void;
}

export interface HandlerOptions {
  target: DeployTarget;
  data: KrateServerData;
  modules: KrateServerModules;
  static?: StaticLayer;
  /** Page ISR cache storage. Defaults to a bounded in-memory store per instance. */
  isrStore?: IsrStore;
  /** Overrides data.config.trustProxy (the serverless platforms always set X-Forwarded-*). */
  trustProxy?: boolean;
  /** Hex sha256 for error digests; defaults to the portable implementation. */
  sha256Hex?(input: string): string;
}

export type KrateHandler = (request: Request, ctx?: HandlerContext) => Promise<Response>;

// Internals

const NO_BODY_STATUS = new Set([101, 204, 205, 304]);
const KRATE_METHODS = ["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"];
const STATIC_ASSET_EXTS = new Set([
  ".css", ".js", ".mjs", ".map", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".avif", ".ico", ".woff", ".woff2",
  ".ttf", ".otf", ".eot", ".mp4", ".webm", ".mp3", ".wav", ".ogg", ".wasm", ".txt", ".xml", ".webmanifest", ".pdf", ".zip",
]);

function globalStore<T>(name: string): AsyncLocalStorage<T> {
  const key = Symbol.for(name);
  const g = globalThis as Record<symbol, unknown>;
  if (!g[key]) g[key] = new AsyncLocalStorage<T>();
  return g[key] as AsyncLocalStorage<T>;
}

function safeDecode(p: string): string {
  try {
    return decodeURIComponent(p);
  } catch {
    return p;
  }
}

/** The mutable per-request state: the request as the Go handlers see it, plus the response headers. */
interface ReqState {
  request: Request;
  method: string;
  /** Decoded URL path, after base-path stripping and rewrites. */
  path: string;
  /** The escaped path as received (net/http.ServeMux cleans this form). */
  rawPath: string;
  search: string;
  /** Headers set on the response so far (Go's w.Header()). */
  res: Headers;
  /** Headers the handlers read (conditional headers can be dropped). */
  headers: Headers;
  origin: string;
  ctx: HandlerContext;
}

function textResponse(rs: ReqState, status: number, body: string, contentType = "text/plain; charset=utf-8"): Response {
  rs.res.set("Content-Type", contentType);
  return finish(rs, status, body);
}

/** http.Error */
function httpError(rs: ReqState, message: string, status: number): Response {
  rs.res.delete("Content-Length");
  rs.res.set("Content-Type", "text/plain; charset=utf-8");
  rs.res.set("X-Content-Type-Options", "nosniff");
  return finish(rs, status, message + "\n");
}

/** http.NotFound; after a file-server miss Go also drops the entity headers. */
function notFoundText(rs: ReqState, fileServer = false): Response {
  if (fileServer) for (const h of ["Content-Encoding", "Etag", "Last-Modified", "Cache-Control"]) rs.res.delete(h);
  return httpError(rs, "404 page not found", 404);
}

function finish(rs: ReqState, status: number, body: BodyInit | null): Response {
  const noBody = NO_BODY_STATUS.has(status) || rs.method === "HEAD";
  return new Response(noBody ? null : body, { status, headers: rs.res });
}

/** writeHeaderPairs: repeated Set-Cookie append, everything else replaces what earlier layers set. */
function writeHeaderPairs(h: Headers, pairs: Iterable<[string, string]>): void {
  const seen = new Set<string>();
  for (const [k0, v] of pairs) {
    const k = k0.toLowerCase();
    if (k === "set-cookie") {
      h.append("Set-Cookie", v);
      continue;
    }
    if (!seen.has(k)) {
      h.delete(k);
      seen.add(k);
    }
    h.append(k, v);
  }
}

function headerPairs(headers: Headers): Array<[string, string]> {
  const out: Array<[string, string]> = [];
  const anyH = headers as any;
  const cookies: string[] = typeof anyH.getSetCookie === "function" ? anyH.getSetCookie() : [];
  headers.forEach((v, k) => {
    if (k.toLowerCase() !== "set-cookie") out.push([k, v]);
  });
  for (const c of cookies) out.push(["set-cookie", c]);
  return out;
}

/** The scheme the client used, from the X-Forwarded-Proto the server sets or a trusted proxy sends. */
function isHTTPSRequest(headers: Headers): boolean {
  return (headers.get("x-forwarded-proto") || "").split(",")[0].trim().toLowerCase() === "https";
}

function isStaticAssetPath(p: string): boolean {
  return STATIC_ASSET_EXTS.has(extName(p).toLowerCase());
}

function requestCookiesOf(headers: Headers): Map<string, string> {
  const jar = new Map<string, string>();
  for (const part of String(headers.get("cookie") || "").split(";")) {
    const i = part.indexOf("=");
    if (i <= 0) continue;
    const name = part.slice(0, i).trim();
    if (!name || jar.has(name)) continue;
    // Same parsing as the action context and the Node sidecar: unquote, then percent-decode.
    let value = part.slice(i + 1).trim();
    if (value.startsWith('"') && value.endsWith('"')) value = value.slice(1, -1);
    try {
      value = decodeURIComponent(value);
    } catch {
      // keep the raw value
    }
    jar.set(name, value);
  }
  return jar;
}

function attachCookies(request: Request): Request {
  try {
    const jar = requestCookiesOf(request.headers);
    Object.defineProperty(request, "cookies", {
      value: {
        get: (n: string) => (jar.has(String(n)) ? { name: String(n), value: jar.get(String(n)) } : undefined),
        getAll: (n?: string) => [...jar].filter(([k]) => n === undefined || k === String(n)).map(([name, value]) => ({ name, value })),
        has: (n: string) => jar.has(String(n)),
      },
      enumerable: false,
    });
  } catch {
    // frozen request objects keep working without the cookies accessor
  }
  return request;
}

const sleepMicrotask = () => Promise.resolve();

// Handler factory

export function createKrateHandler(options: HandlerOptions): KrateHandler {
  const { data, modules } = options;
  const cfg = data.config;
  const target = options.target;
  // Prerendered pages carried in the bundle (platforms that route pages through the function for middleware).
  const staticLayer = options.static || (data.staticPages ? createMemoryStaticLayer(data.staticPages) : undefined);
  const trustProxy = options.trustProxy !== undefined ? options.trustProxy : cfg.trustProxy;
  const basePath = cfg.basePath || "";
  const hashed = new Set(data.hashedAssets);
  const staticOnly = new Set(data.staticOnlyRoutes.map(normalizeRoutePattern));
  const sha256Hex = options.sha256Hex || portableSha256Hex;
  const segEntries = data.segments || [];
  const segPages = data.segmentPages || {};
  const segDirByRoute = new Map<string, string>();
  for (const p of data.allPages) if (p.segDir !== undefined) segDirByRoute.set(normalizeRoutePattern(p.route), p.segDir);
  for (const p of data.pages) if (p.segDir !== undefined) segDirByRoute.set(normalizeRoutePattern(p.route), p.segDir);

  /** The page to answer a 404 or 500 with. match is null for the global page. */
  interface ErrorPage {
    html: string;
    match: SegmentMatch | null;
  }

  /**
   * errorPage is the single place that picks the error page for a request:
   * the nearest segment page (build/segments.go LookupSegmentPage), else the
   * global 404/500. segDir is the directory of the matched route; closed names
   * a dynamic route that matched the URL but rejected its params. null means
   * no custom page.
   */
  function errorPage(urlPath: string, status: 401 | 403 | 404 | 500, route?: { segDir?: string; closed?: string }): ErrorPage | null {
    const kind: SegmentKind = SEGMENT_KIND_OF_STATUS[status];
    if (segEntries.length > 0) {
      let m: SegmentMatch | null = null;
      const closedDir = route && route.closed ? segDirByRoute.get(normalizeRoutePattern(route.closed)) : undefined;
      if (closedDir !== undefined) m = lookupSegmentPage(segEntries, kind, urlPath, true, closedDir);
      else if (route && route.segDir !== undefined) m = lookupSegmentPage(segEntries, kind, urlPath, true, route.segDir);
      else m = lookupSegmentPage(segEntries, kind, urlPath, false, "");
      const html = m ? segPages[m.file] : undefined;
      if (m && html !== undefined) return { html, match: m };
    }
    const global = status === 404 ? data.html404 : status === 500 ? data.html500 : "";
    return global ? { html: global, match: null } : null;
  }
  let requestSeq = 0;
  let latestWaitUntil: ((p: Promise<unknown>) => void) | undefined;

  // Stores shared with @krate/runtime/server (headers(), cookies(), revalidatePath()).
  const actionStore = globalStore<any>("krate.action.store");
  const revalidateStore = globalStore<RevalidateScope>("krate.revalidate.store");
  if (!cacheRuntime().renderStore) installRenderStore(new AsyncLocalStorage());
  patchFetch();

  // Pages: most specific route first, like SSRServer.FindPageForRoute.
  const pagesBySpecificity = [...data.pages].sort((a, b) => compareRouteSpecificity(a.route, b.route));
  const manifest = { pages: data.pages as ManifestPage[], regions: data.regions };
  const core: SSRCore = createSSRCore({
    manifest,
    sha256Hex: options.sha256Hex,
    isrStore: options.isrStore || new MemoryIsrStore(cfg.ssr.maxCacheSize > 0 ? cfg.ssr.maxCacheSize : 512),
    loadPageModule: async (page) => {
      const mod = modules.pages && modules.pages[page.bundlePath || ""];
      if (!mod) throw new Error(`server bundle for ${page.route} is not part of this deployment (${page.bundlePath})`);
      return mod;
    },
    loadRuntimeRenderer: (relPath) => {
      const fn = modules.runtime && modules.runtime[relPath];
      if (typeof fn !== "function") throw new Error(`runtime component bundle ${relPath} is not part of this deployment`);
      return fn;
    },
    maxCacheSize: cfg.ssr.maxCacheSize,
    timeoutMs: cfg.ssr.timeout > 0 ? cfg.ssr.timeout : 5000,
    ppr: cfg.ssr.ppr,
    pprRevalidate: cfg.ssr.pprRevalidate,
    background: (p) => {
      if (latestWaitUntil) latestWaitUntil(p);
    },
  });

  const shells = new Map<string, RegionShell>();
  function loadShell(route: string): RegionShell | null {
    let e = shells.get(route);
    if (!e) {
      const html = data.shells[route];
      if (html === undefined) return null;
      e = parseRegionShell(html);
      shells.set(route, e);
    }
    return e;
  }

  function findPageForRoute(urlPath: string): { page: HandlerPage; params: Record<string, string> } | null {
    for (const p of pagesBySpecificity) {
      const params = matchRoutePattern(urlPath, p.route);
      if (params) return { page: p, params };
    }
    return null;
  }

  const templates = [...data.templates].sort((a, b) => compareRouteSpecificity(a.pattern, b.pattern));
  const corsOrigins = cfg.cors.origins.length ? cfg.cors.origins : ["*"];

  // Middleware

  let mwMatchers: RegExp[] | null | undefined;
  function middlewareMatchers(): RegExp[] | null {
    if (mwMatchers !== undefined) return mwMatchers;
    const mod = modules.middleware;
    const m = mod && (mod.matcher !== undefined ? mod.matcher : mod.config && mod.config.matcher);
    if (m === undefined || m === null) return (mwMatchers = null);
    mwMatchers = (Array.isArray(m) ? m : [m]).map((e: any) => {
      if (e && typeof e === "object") {
        if (e.has || e.missing || e.locale === false) {
          throw new Error('middleware matcher has/missing/locale conditions are not supported; use a path-only matcher');
        }
        return compileMatcher(e.source);
      }
      return compileMatcher(e);
    });
    return mwMatchers;
  }

  /**
   * A matcher-less middleware is skipped only for a path that is a real file in
   * the static layer. The extension alone is not enough: /api/files/x.pdf is a
   * dynamic route that merely looks like a file.
   */
  async function skipsMiddleware(p: string): Promise<boolean> {
    if (!staticLayer || !isStaticAssetPath(p)) return false;
    if (p === "/api" || p.startsWith("/api/") || p.startsWith("/__krate/")) return false;
    return !!(await staticLayer.exists(p));
  }

  /** Runs middleware; a Response ends the request, null continues (headers/rewrite already applied). */
  async function runMiddleware(rs: ReqState): Promise<Response | null> {
    const p = rs.path;
    if (p.startsWith("/__krate/") && !p.startsWith("/__krate/action/")) return null;
    const mod = modules.middleware;
    if (!data.hasMiddleware || !mod || typeof mod.middleware !== "function") return null;
    let matchers: RegExp[] | null;
    try {
      matchers = middlewareMatchers();
    } catch (e: any) {
      return middlewareFailed(rs, e);
    }
    if (!matchers && (await skipsMiddleware(p))) return null;
    if (matchers && !matchers.some((re) => re.test(p))) return null;
    let result: any;
    try {
      const request = attachCookies(new Request(fullURL(rs), { method: rs.method, headers: new Headers(rs.request.headers) }));
      result = await mod.middleware(request);
    } catch (e) {
      return middlewareFailed(rs, e);
    }
    if (result === undefined || result === null || typeof result.status !== "number") return null;
    let next = false;
    let rewrite = "";
    const hdrs: Array<[string, string]> = [];
    for (const [k, v] of headerPairs(result.headers)) {
      if (k === "x-middleware-next") next = true;
      else if (k === "x-middleware-rewrite") rewrite = v;
      else hdrs.push([k, v]);
    }
    if (next || rewrite) {
      writeHeaderPairs(rs.res, hdrs);
      if (rewrite) applyRewrite(rs, rewrite);
      return null;
    }
    const body = result.status === 204 ? "" : await result.text();
    writeHeaderPairs(rs.res, hdrs);
    let status = result.status;
    if (status < 100 || status > 999) status = 500;
    return finish(rs, status, rs.method === "HEAD" || !body ? null : body);
  }

  function middlewareFailed(rs: ReqState, err: unknown): Response {
    console.error(`[krate] middleware error: ${rs.method} ${rs.path}: ${(err && (err as any).stack) || err}`);
    // Fail closed: continuing would turn a broken auth check into an unauthenticated request.
    rs.res.set("Cache-Control", "no-store");
    return httpError(rs, "middleware failed", 503);
  }

  function applyRewrite(rs: ReqState, target: string): void {
    const i = target.indexOf("://");
    if (i >= 0) {
      const rest = target.slice(i + 3);
      const j = rest.indexOf("/");
      target = j >= 0 ? rest.slice(j) : "/";
    }
    let p = target;
    let q = "";
    const qi = target.indexOf("?");
    if (qi >= 0) {
      p = target.slice(0, qi);
      q = target.slice(qi + 1);
    }
    if (!p.startsWith("/")) p = "/" + p;
    rs.path = rewriteTargetPath(p);
    if (q) rs.search = "?" + q;
  }

  // Request helpers

  function requestOrigin(request: Request, url: URL): string {
    if (trustProxy) {
      const proto = (request.headers.get("x-forwarded-proto") || "").split(",")[0].trim().toLowerCase();
      const host = (request.headers.get("x-forwarded-host") || "").split(",")[0].trim();
      const scheme = proto === "http" || proto === "https" ? proto : url.protocol.replace(":", "");
      return scheme + "://" + (host || url.host);
    }
    return url.protocol + "//" + url.host;
  }

  function requestHost(rs: ReqState): string {
    if (trustProxy) {
      const h = (rs.request.headers.get("x-forwarded-host") || "").split(",")[0].trim();
      if (h) return h;
    }
    return new URL(rs.request.url).host;
  }

  function fullURL(rs: ReqState): string {
    return rs.origin + encodeURI(rs.path).replace(/%25([0-9A-Fa-f]{2})/g, "%$1") + rs.search;
  }

  function queryOf(rs: ReqState): Record<string, string> {
    const out: Record<string, string> = {};
    const sp = new URLSearchParams(rs.search);
    for (const k of new Set(sp.keys())) out[k] = sp.get(k) as string;
    return out;
  }

  function headerMap(rs: ReqState): Record<string, string> {
    const out: Record<string, string> = {};
    rs.headers.forEach((v, k) => {
      if (k !== "host") out[canonicalHeader(k)] = v;
    });
    return out;
  }

  function canonicalHeader(k: string): string {
    return k.split("-").map((s) => (s ? s[0].toUpperCase() + s.slice(1) : s)).join("-");
  }

  async function readBody(rs: ReqState, limit: number): Promise<Uint8Array | "too-large" | null> {
    if (rs.method === "GET" || rs.method === "HEAD" || !rs.request.body) return null;
    const declared = Number(rs.request.headers.get("content-length") || 0);
    if (limit > 0 && declared > limit) return "too-large";
    const reader = rs.request.body.getReader();
    const chunks: Uint8Array[] = [];
    let total = 0;
    for (;;) {
      const r = await reader.read();
      if (r.done) break;
      total += r.value.byteLength;
      if (limit > 0 && total > limit) {
        try {
          await reader.cancel();
        } catch {
          // already closed
        }
        return "too-large";
      }
      chunks.push(r.value);
    }
    const out = new Uint8Array(total);
    let off = 0;
    for (const c of chunks) {
      out.set(c, off);
      off += c.byteLength;
    }
    return out;
  }

  // Static layer and 404

  function staticRouteExists(p: string): boolean | Promise<boolean> {
    return staticLayer ? staticLayer.exists(p) : false;
  }

  /** shouldServeStatic: a concrete static file shadows dynamic routes, but never a request-time page. */
  async function shouldServeStatic(p: string, page: HandlerPage | null): Promise<boolean> {
    if (!staticLayer) return false;
    if (page) return staticLayer.isAsset(p);
    return staticLayer.exists(p);
  }

  function redirectToCanonicalSlash(rs: ReqState): Response | null {
    const p = rs.path;
    if (p === "" || p.endsWith("/") || isFileLikePath(p)) return null;
    if (rs.method !== "GET" && rs.method !== "HEAD") return null;
    // "./" keeps a segment such as "ftp:3232235777" or "\\host" from parsing as an absolute URL.
    let target = "./" + baseName(p) + "/";
    if (rs.search) target += rs.search;
    rs.res.set("Location", target);
    return finish(rs, 301, null);
  }

  /** Answers 404 with a not-found page; segment pages are never cached. */
  function custom404(rs: ReqState, page: ErrorPage, status: 401 | 403 | 404 = 404): Response {
    rs.res.set("Content-Type", "text/html; charset=utf-8");
    rs.res.delete("Cache-Control");
    rs.res.delete("Etag");
    rs.res.delete("Last-Modified");
    rs.res.set("Cache-Control", "no-store");
    return finish(rs, status, page.match ? renderSegmentPage(page.html, page.match) : page.html);
  }

  /** The 403 or 401 response of a request whose render called forbidden() or unauthorized(). */
  function accessDenied(rs: ReqState, status: 401 | 403, route?: { segDir?: string; closed?: string }): Response {
    rs.res.delete("X-Krate-Cache");
    const page = errorPage(rs.path, status, route);
    if (page) return custom404(rs, page, status);
    rs.res.set("Cache-Control", "no-store");
    return httpError(rs, status === 403 ? "403 Forbidden" : "401 Unauthorized", status);
  }

  /** handlerWith404: internal-path guard, cache policy, dynamic templates, static files, custom 404. */
  async function staticHandler(rs: ReqState): Promise<Response> {
    const p = rs.path;
    if (isInternalOutputPath(p)) return notFoundText(rs);
    if (p.endsWith(".map")) rs.res.set("Content-Type", "application/json; charset=utf-8");
    rs.res.set("Cache-Control", staticCacheControl(p, hashed));

    let closedPattern = "";
    if (!(await staticRouteExists(p))) {
      for (const t of templates) {
        if (staticOnly.has(normalizeRoutePattern(t.pattern))) {
          if (!closedPattern && matchRoutePattern(p, t.pattern)) closedPattern = t.pattern;
          continue;
        }
        const params = matchRoutePattern(p, t.pattern);
        if (params) {
          const slash = redirectToCanonicalSlash(rs);
          if (slash) return slash;
          rs.res.set("Content-Type", "text/html; charset=utf-8");
          return finish(rs, 200, applyDynamicRouteParams(t.html, params));
        }
      }
    }

    const wantsHTML = isHTMLDocRequest(p) || (rs.headers.get("accept") || "").includes("text/html");
    // Conditional headers dropped by an outer layer (flash notices) must not reach the file server.
    const sreq = rs.headers === rs.request.headers ? rs.request : new Request(rs.request.url, { method: rs.request.method, headers: rs.headers });
    const served = staticLayer ? await staticLayer.serve(sreq, p) : null;
    if (served && served.status !== 404) return mergeStaticResponse(rs, served);
    const page404 = wantsHTML ? errorPage(p, 404, { closed: closedPattern }) : null;
    if (page404) return custom404(rs, page404);
    return notFoundText(rs, true);
  }

  /** Static responses bring their own headers; layers that ran earlier (security, cache policy) stay unless replaced. */
  function mergeStaticResponse(rs: ReqState, served: Response): Response {
    served.headers.forEach((v, k) => {
      if (k.toLowerCase() === "set-cookie") return;
      rs.res.set(k, v);
    });
    return new Response(rs.method === "HEAD" ? null : served.body, { status: served.status, headers: rs.res });
  }

  // Pages

  function pageMethodAllowed(rs: ReqState): Response | null {
    switch (rs.method) {
      case "GET":
      case "HEAD":
        return null;
      case "OPTIONS":
        rs.res.set("Allow", "GET, HEAD, OPTIONS");
        return finish(rs, 204, null);
    }
    rs.res.set("Allow", "GET, HEAD");
    return httpError(rs, "method not allowed", 405);
  }

  const PLAIN_500 = "<!doctype html><title>500</title><h1>Internal Server Error</h1>";

  async function pageHandler(rs: ReqState): Promise<Response> {
    const denied = pageMethodAllowed(rs);
    if (denied) return denied;
    if (data.pages.length === 0) return staticHandler(rs);

    let route = requestRoute(rs.path);
    const found = findPageForRoute(route);
    const page = found ? found.page : null;
    if (await shouldServeStatic(rs.path, page)) return staticHandler(rs);
    if (!found || !page) return staticHandler(rs);
    const slash = redirectToCanonicalSlash(rs);
    if (slash) return slash;

    const isISR = page.mode === "isr";
    if (page.route) route = page.route;
    const params = found.params || {};
    const headers = headerMap(rs);
    const query = queryOf(rs);

    rs.res.set("Content-Type", "text/html; charset=utf-8");
    rs.res.set("X-Content-Type-Options", "nosniff");
    if (!isISR) rs.res.set("Cache-Control", "no-store");
    let reval = 0;
    if (isISR) {
      reval = page.revalidate && page.revalidate > 0 ? page.revalidate : 60;
    }
    const outOfOrder = page.mode === "streaming";

    const shell = loadShell(route);
    if (shell && shell.ok) {
      const result = await servePageRegions(rs, shell, page, route, params, query, headers, isISR, reval, outOfOrder);
      if (result) return result;
    }

    // No region markers in the shell: the page is fully static. Serve the baked shell.
    const baked = data.shells[route];
    if (baked !== undefined) return finish(rs, 200, baked);
    return staticHandler(rs);
  }

  interface Frame {
    type: string;
    id?: string;
    kind?: string;
    status?: number;
    html?: string;
    error?: string;
    digest?: string;
    stack?: string;
    cacheStatus?: string;
    notFound?: boolean;
    denied?: 401 | 403;
    redirect?: string;
    title?: string;
    noStore?: boolean;
    fatal?: boolean;
  }

  /** Renders the regions of a shell like /__krate/regions does and returns one frame per region. */
  function startRegionRenders(page: HandlerPage, route: string, shell: RegionShell, renderReq: RenderRequest): Array<Promise<Frame>> {
    const list: Array<{ id: string; kind: "component" | "page" }> = shell.all.map((b) => ({
      id: b.id,
      kind: b.kind === "suspense" && b.id === "page" ? "page" : "component",
    }));
    const metas = data.regions[route] || [];
    return list.map(async (r): Promise<Frame> => {
      try {
        if (r.kind === "page") return (await core.renderPageRegion(page as ManifestPage, r.id, renderReq)) as Frame;
        const region = metas.find((m) => m.id === r.id);
        if (region) return (await core.renderRegion(page as ManifestPage, region, renderReq)) as Frame;
        return { type: "skip", id: r.id };
      } catch (err: any) {
        const message = (err && err.message) || String(err);
        const stack = (err && typeof err.stack === "string" && err.stack) || "";
        const digest = sha256Hex(message + "\n" + stack).slice(0, 10);
        console.error(`[krate] region ${route}::${r.id} failed (digest ${digest}):`, err);
        return { type: "error", id: r.id, status: 500, error: message, stack, digest };
      }
    });
  }

  async function servePageRegions(
    rs: ReqState,
    shellInfo: RegionShell,
    page: HandlerPage,
    route: string,
    params: Record<string, string>,
    query: Record<string, string>,
    headers: Record<string, string>,
    isISR: boolean,
    revalidate: number,
    outOfOrder: boolean,
  ): Promise<Response | null> {
    let shell = shellInfo.shell;
    const renderReq: RenderRequest = { route, url: route, method: "GET", headers, params, query };
    const pending = startRegionRenders(page, route, shellInfo, renderReq);

    if (outOfOrder && !isISR && canStreamOutOfOrder(shellInfo.all) && (!page.serverComponents || allSuspense(shellInfo.all))) {
      return streamSiblingRegions(rs, shell, shellInfo, pending);
    }

    // Ordered assembly: wait for every region, then decide status, headers and body.
    const regionHTML = new Map<string, string>();
    let isISRFrame = false;
    let cacheStatus = "";
    let titleOverride = "";
    let notFound = false;
    let denied: 401 | 403 | 0 = 0;
    let redirect = "";
    let renderErr = "";
    let fatalErr = "";
    let errStack = "";
    let errDigest = "";
    let noStore = false;
    for (const frame of await Promise.all(pending)) {
      switch (frame.type) {
        case "region": {
          const id = frame.id || "";
          if (id !== "page" && (frame.notFound || frame.denied || frame.redirect)) {
            notFound = notFound || !!frame.notFound;
            denied = denied || frame.denied || 0;
            if (!redirect) redirect = frame.redirect || "";
            continue;
          }
          if (frame.noStore) noStore = true;
          if (id === "page") {
            isISRFrame = !!frame.cacheStatus;
            cacheStatus = frame.cacheStatus || "";
            notFound = !!frame.notFound;
            denied = denied || frame.denied || 0;
            redirect = frame.redirect || "";
            titleOverride = frame.title || "";
            if ((frame.status || 0) >= 500 || frame.error) {
              renderErr = frame.error || `page-region render failed with status ${frame.status}`;
              errStack = frame.stack || "";
              errDigest = frame.digest || "";
              if (frame.fatal) fatalErr = renderErr;
              continue;
            }
            if (frame.notFound || frame.denied) continue;
          }
          regionHTML.set(id, frame.html || "");
          break;
        }
        case "error": {
          const msg = frame.error || `region ${frame.id} render failed with status ${frame.status}`;
          if (frame.fatal && !fatalErr) fatalErr = msg;
          if (!renderErr) {
            renderErr = msg;
            errStack = frame.stack || "";
            errDigest = frame.digest || "";
          }
          break;
        }
      }
    }

    // A segment error page makes any failed render answer with that page
    // instead of the baked body (build/serve.go failOnRenderErr).
    const routeRef = { segDir: page.segDir !== undefined ? page.segDir : "" };
    const segError = errorPage(rs.path, 500, routeRef);
    if (segError && segError.match && renderErr && !fatalErr && !notFound && !denied && !redirect) fatalErr = renderErr;
    if (fatalErr) {
      // A server component failed: there is no meaningful baked body, so answer 500.
      const srvErr = newServerError(fatalErr, errStack, errDigest || sha256Hex(fatalErr + "\n" + errStack).slice(0, 10), false);
      console.error(`[krate] Render error (${route}) [digest ${srvErr.digest}]: ${fatalErr}`);
      if (errStack) console.error(errStack);
      rs.res.set("X-Krate-Error", "render");
      rs.res.delete("X-Krate-Cache");
      rs.res.set("Cache-Control", "no-store");
      rs.res.set("Content-Type", "text/html; charset=utf-8");
      if (segError && segError.match) return finish(rs, 500, renderSegmentPage(segError.html, segError.match, srvErr));
      return finish(rs, 500, segError ? segError.html : PLAIN_500);
    }
    if (notFound || denied || redirect) {
      if (redirect) return goRedirect(redirect, rs.path, rs.method, 302, rs.res);
      if (denied) return accessDenied(rs, denied, routeRef);
      const page404 = errorPage(rs.path, 404, routeRef);
      if (page404) return custom404(rs, page404);
      rs.res.set("Cache-Control", "no-store");
      return notFoundText(rs);
    }

    if (renderErr) {
      console.error(`[krate] Page region error (${route}): ${renderErr}`);
      rs.res.set("X-Krate-Error", "render");
    }
    if (isISR && isISRFrame && cacheStatus) rs.res.set("X-Krate-Cache", cacheStatus.toUpperCase());
    if (isISR) {
      if (noStore || renderErr) {
        rs.res.set("Cache-Control", "private, no-store");
        rs.res.delete("X-Krate-Cache");
      } else {
        const r = revalidate > 0 ? revalidate : 60;
        rs.res.set("Cache-Control", `public, s-maxage=${r}, stale-while-revalidate=${2 * r}`);
      }
    }
    let html = "";
    spliceShell(shell, shellInfo.roots, regionHTML, (c) => (html += c));
    // Swap the title after splicing: the marker offsets belong to the baked shell.
    if (titleOverride) html = replaceTitle(html, titleOverride);
    return finish(rs, 200, html);
  }

  /** Sibling Suspense regions stream in completion order after the shell. */
  function streamSiblingRegions(rs: ReqState, shell: string, shellInfo: RegionShell, pending: Array<Promise<Frame>>): Response {
    const [prefix, tail] = splitBodyScripts(shell);
    const enc = new TextEncoder();
    const method = rs.method;
    const body = new ReadableStream<Uint8Array>({
      async start(controller) {
        controller.enqueue(enc.encode(prefix));
        await Promise.all(
          pending.map(async (p) => {
            const f = await p;
            if (f.type !== "region" || !f.id) return;
            if (f.notFound || f.denied || f.redirect || f.fatal || (f.status || 0) >= 500 || f.error) {
              // The shell is already on the wire: keep the baked fallback for this region.
              if (f.error) console.error(`[krate] region ${f.id} failed: ${f.error}`);
              return;
            }
            if (!f.html) return;
            controller.enqueue(enc.encode(regionReplaceScript(f.id, f.html)));
          }),
        );
        controller.enqueue(enc.encode(tail));
        controller.close();
      },
    });
    void shellInfo;
    if (method === "HEAD") return new Response(null, { status: 200, headers: rs.res });
    return new Response(body, { status: 200, headers: rs.res });
  }

  // Redirects and rewrites

  async function redirectRewriteHandler(rs: ReqState): Promise<Response> {
    const p = rs.path;
    for (const rd of cfg.redirects) {
      if (matchRedirect(p, rd.source)) {
        const dest = rewriteDestination(p, rd.source, rd.destination);
        return goRedirect(dest, p, rs.method, rd.permanent ? 301 : 302, rs.res);
      }
    }
    for (const rw of cfg.rewrites) {
      if (matchRedirect(p, rw.source)) {
        const rewritten = rewriteDestination(p, rw.source, rw.destination);
        rs.path = rewriteTargetPath(safeDecode(rewritten));
        break;
      }
    }
    return pageHandler(rs);
  }

  // API routes

  const apiKeys = new Set(data.apiRoutes);
  const apiDirs = new Set<string>(); // "" and every directory prefix
  apiDirs.add("");
  for (const k of data.apiRoutes) {
    const segs = k.split("/");
    for (let i = 1; i < segs.length; i++) apiDirs.add(segs.slice(0, i).join("/"));
  }

  function isDynamicSegment(name: string): boolean {
    return name.startsWith("[") && name.endsWith("]") && !name.startsWith("[...") && !name.startsWith("[[...");
  }
  function isCatchAllSegment(name: string): boolean {
    return name.startsWith("[...") || name.startsWith("[[...");
  }

  /** Directory entries of the virtual API tree, in a stable order. */
  function apiEntries(dir: string): Array<{ name: string; isDir: boolean }> {
    const prefix = dir === "" ? "" : dir + "/";
    const out = new Map<string, boolean>();
    for (const k of data.apiRoutes) {
      if (!k.startsWith(prefix)) continue;
      const rest = k.slice(prefix.length);
      const i = rest.indexOf("/");
      if (i < 0) out.set(rest, out.get(rest) || false);
      else out.set(rest.slice(0, i), true);
    }
    return [...out].sort((a, b) => strCmp(a[0], b[0])).map(([name, isDir]) => ({ name, isDir }));
  }

  function strCmp(a: string, b: string): number {
    return a < b ? -1 : a > b ? 1 : 0;
  }

  function resolveDynamicAPIDir(dir: string, segs: string[]): string {
    if (!apiDirs.has(dir)) return "";
    const join = (a: string, b: string) => (a === "" ? b : a + "/" + b);
    if (segs.length === 0) {
      const idx = join(dir, "index");
      return apiKeys.has(idx) ? idx : "";
    }
    const next = join(dir, segs[0]);
    if (apiDirs.has(next)) {
      const hit = resolveDynamicAPIDir(next, segs.slice(1));
      if (hit) return hit;
    }
    for (const e of apiEntries(dir)) {
      const full = join(dir, e.name);
      if (e.isDir) {
        if (isCatchAllSegment(e.name) && segs.length > 0) {
          const idx = join(full, "index");
          if (apiKeys.has(idx)) return idx;
        }
        if (isDynamicSegment(e.name) && segs.length > 0) {
          const hit = resolveDynamicAPIDir(full, segs.slice(1));
          if (hit) return hit;
        }
        continue;
      }
      if (isCatchAllSegment(e.name) && (segs.length > 0 || e.name.startsWith("[[..."))) return full;
      if (segs.length === 1 && isDynamicSegment(e.name)) return full;
    }
    return "";
  }

  /** Port of APIRouteRuntime.resolveAPIFile over the inlined route keys. */
  function resolveAPIFile(relPath: string): string {
    let rel = safeDecode(relPath).split("/").filter((s) => s !== "").join("/");
    if (rel === "") rel = "index";
    if (rel.includes("\0") || rel.includes("\\")) return "";
    if (rel.split("/").includes("..")) return "";
    if (apiKeys.has(rel)) return rel;
    if (apiKeys.has(rel + "/index")) return rel + "/index";
    return resolveDynamicAPIDir("", rel.split("/"));
  }

  function apiRouteParams(file: string, reqPath: string): Record<string, string> {
    let rel = file;
    if (rel === "index") rel = "";
    else if (rel.endsWith("/index")) rel = rel.slice(0, -6);
    const fileSegs = rel === "" ? [] : rel.split("/");
    const rawPath = reqPath.replace(/^\/api/, "");
    const pathSegs = rawPath === "" || rawPath === "/" ? [] : rawPath.replace(/^\/+|\/+$/g, "").split("/").map(safeDecode);
    const params: Record<string, string> = {};
    for (let i = 0; i < fileSegs.length; i++) {
      const seg = fileSegs[i];
      if (seg.startsWith("[[...") && seg.endsWith("]]")) {
        if (i < pathSegs.length) params[seg.slice(5, -2)] = pathSegs.slice(i).join("/");
        return params;
      }
      if (seg.startsWith("[...") && seg.endsWith("]")) {
        params[seg.slice(4, -1)] = pathSegs.slice(Math.min(i, pathSegs.length)).join("/");
        return params;
      }
      if (seg.startsWith("[") && seg.endsWith("]") && i < pathSegs.length) params[seg.slice(1, -1)] = pathSegs[i];
    }
    return params;
  }

  function jsonResponse(rs: ReqState, status: number, obj: unknown): Response {
    rs.res.set("Content-Type", "application/json");
    return finish(rs, status, JSON.stringify(obj));
  }

  function apiFailure(rs: ReqState, status: number, allow?: string[]): Response {
    rs.res.set("Content-Type", "application/json");
    rs.res.set("Cache-Control", "no-store");
    if (status === 404) return finish(rs, 404, '{"error":"API Route Not Found"}');
    if (status === 403) return finish(rs, 403, '{"error":"Forbidden"}');
    if (status === 401) return finish(rs, 401, '{"error":"Unauthorized"}');
    if (status === 405) {
      rs.res.set("Allow", (allow || []).join(", "));
      return finish(rs, 405, '{"error":"Method Not Allowed"}');
    }
    if (status === 413) return finish(rs, 413, '{"error":"Request Entity Too Large"}');
    return finish(rs, status < 400 ? 500 : status, '{"error":"Internal Server Error"}');
  }

  function allowList(mod: any): string[] {
    const out = KRATE_METHODS.filter((m) => typeof mod[m] === "function");
    if (out.includes("GET") && !out.includes("HEAD")) out.push("HEAD");
    if (out.length && !out.includes("OPTIONS")) out.push("OPTIONS");
    return out;
  }

  function isForeignResponse(v: any): boolean {
    return (
      v && typeof v === "object" && typeof v.status === "number" && !Array.isArray(v) &&
      (typeof v.text === "function" || typeof v.arrayBuffer === "function" || "body" in v || "headers" in v)
    );
  }

  async function toResponse(v: any): Promise<Response> {
    if (v instanceof Response) return v;
    if (isForeignResponse(v)) {
      let headers: Headers;
      if (!v.headers) headers = new Headers();
      else if (v.headers instanceof Headers) headers = v.headers;
      else if (typeof v.headers.entries === "function") headers = new Headers(v.headers.entries());
      else headers = new Headers(v.headers);
      const bodyP = typeof v.arrayBuffer === "function" ? v.arrayBuffer() : typeof v.text === "function" ? v.text() : v.body;
      const b = await bodyP;
      const empty = v.status === 204 || v.status === 304 || b === undefined || b === null || b === "" || b.byteLength === 0;
      return new Response(empty ? null : b, { status: v.status < 200 ? 200 : v.status, headers });
    }
    if (v === undefined || v === null) return new Response(null, { status: 204 });
    const json = JSON.stringify(v);
    if (json === undefined) return new Response(null, { status: 204 });
    return new Response(json, { status: 200, headers: { "content-type": "application/json" } });
  }

  // Revalidation, applied to this instance's caches.

  async function applyRevalidation(payload: { paths: Array<{ path: string; type?: string }>; tags: string[] }) {
    const revalidated: Array<{ target: string; kind: string; routes?: string[] }> = [];
    const skipped: Array<{ target: string; reason: string }> = [];
    const targets: Array<{ route: string; params?: Record<string, string> }> = [];
    for (const p of payload.paths) {
      const r = resolveRevalidatePath(p.path, p.type || "page");
      if (r.error) {
        skipped.push({ target: p.path, reason: r.error });
        continue;
      }
      skipped.push(...r.skipped);
      if (r.targets.length) {
        targets.push(...r.targets);
        revalidated.push({ target: p.path, kind: "path", routes: r.targets.map((t) => t.route) });
      }
    }
    const tags = payload.tags.map((t) => t.trim()).filter(Boolean);
    for (const t of tags) revalidated.push({ target: t, kind: "tag" });
    if (targets.length || tags.length) {
      await core.applyInternalOp("cache-invalidate", { targets, tags });
      if (target !== "node") {
        for (const r of revalidated) {
          skipped.push({
            target: r.target,
            reason: `${target} runs many instances: only this instance's caches were invalidated; other instances keep serving their cached copy until it expires (add a shared cache store with createKrateHandler({ isrStore }) or redeploy)`,
          });
        }
      }
    }
    return { revalidated, skipped };
  }

  function routeUnder(route: string, prefix: string): boolean {
    if (prefix === "/") return true;
    const rs = route.replace(/^\/+|\/+$/g, "").split("/");
    const ps = prefix.replace(/^\/+|\/+$/g, "").split("/");
    if (rs.length < ps.length) return false;
    for (let i = 0; i < ps.length; i++) {
      if (rs[i] === ps[i]) continue;
      if (rs[i].startsWith("[") && rs[i].endsWith("]")) continue;
      return false;
    }
    return true;
  }

  function resolveRevalidatePath(
    reqPath: string,
    type: string,
  ): { targets: Array<{ route: string; params?: Record<string, string> }>; skipped: Array<{ target: string; reason: string }>; error?: string } {
    const none = { targets: [], skipped: [] as Array<{ target: string; reason: string }> };
    let p = reqPath;
    const qi = p.search(/[?#]/);
    if (qi >= 0) p = p.slice(0, qi);
    if (!p.startsWith("/")) return { ...none, error: 'path must start with "/"' };
    p = cleanPath(p);
    if (p !== "/") p = p.replace(/\/+$/, "");
    if (type !== "page" && type !== "layout") return { ...none, error: `unknown revalidatePath type "${type}" (want "page" or "layout")` };
    const staticSkip = (route: string) => ({
      target: route,
      reason: `route ${route} is static (SSG): its HTML was baked at build time and cannot be revalidated at runtime; make the page ISR (export const revalidate = N) or SSR (export const dynamic = "force-dynamic"), or rebuild`,
    });
    const requestTime = (m: string) => m === "ssr" || m === "isr" || m === "streaming";
    const targets: Array<{ route: string; params?: Record<string, string> }> = [];
    const skipped: Array<{ target: string; reason: string }> = [];
    const add = (pm: { route: string; mode: string }, params?: Record<string, string>) => {
      if (!requestTime(pm.mode)) skipped.push(staticSkip(pm.route));
      else targets.push({ route: pm.route, params });
    };
    if (type === "layout") {
      for (const pm of [...data.allPages].sort((a, b) => strCmp(a.route, b.route))) if (routeUnder(pm.route, p)) add(pm);
      if (targets.length === 0 && skipped.length === 0) return { ...none, error: `no page at or below ${p}` };
      return { targets, skipped };
    }
    for (const pm of data.allPages) {
      if (pm.route === p) {
        add(pm);
        return { targets, skipped };
      }
    }
    const dyn = data.allPages.filter((pm) => pm.route.includes("[")).sort((a, b) => (a.route.match(/\[/g) || []).length - (b.route.match(/\[/g) || []).length);
    for (const pm of dyn) {
      const params = matchRoutePattern(p, pm.route);
      if (params) {
        if (!requestTime(pm.mode)) return { targets: [], skipped: [{ target: p, reason: staticSkip(pm.route).reason }] };
        add(pm, params);
        return { targets, skipped };
      }
    }
    return { ...none, error: `no page matches ${p}` };
  }

  async function flushRevalidate(scope: RevalidateScope | undefined) {
    if (!scope || (scope.paths.length === 0 && scope.tags.length === 0)) return null;
    const payload = { paths: scope.paths.slice(), tags: Array.from(new Set(scope.tags)) };
    scope.paths.length = 0;
    scope.tags.length = 0;
    const result = await applyRevalidation(payload);
    for (const s of result.skipped) console.warn(`[krate] revalidation skipped for ${s.target}: ${s.reason}`);
    return payload;
  }

  async function apiHandler(rs: ReqState): Promise<Response> {
    const limit = cfg.maxAPIBodyBytes;
    const relPath = rs.path.replace(/^\/api/, "") || "/index";
    const file = resolveAPIFile(relPath);
    if (!file) return apiFailure(rs, 404);
    const body = await readBody(rs, limit);
    if (body === "too-large") return apiFailure(rs, 413);
    const mod = modules.api && modules.api[file];
    if (!mod) {
      console.error(`[krate] API route ${file} is not part of this deployment`);
      return apiFailure(rs, 500);
    }
    const method = rs.method.toUpperCase();
    let fn = mod[method];
    let named = typeof fn === "function";
    let headOnly = false;
    if (!named && method === "HEAD" && typeof mod.GET === "function") {
      fn = mod.GET;
      named = true;
      headOnly = true;
    }
    if (!named) {
      if (typeof mod.default === "function") fn = mod.default;
      else {
        const allow = allowList(mod);
        if (method === "OPTIONS" && allow.length) {
          rs.res.set("Allow", allow.join(", "));
          return finish(rs, 204, null);
        }
        if (allow.length) return apiFailure(rs, 405, allow);
        console.error(`[krate] API route ${file} must export a handler function (GET, POST, etc. or default).`);
        return apiFailure(rs, 500);
      }
    }
    if (!named && fn.length >= 2) {
      console.error(`[krate] API route ${file} uses the legacy (req, res) handler, which the portable server does not support; export named handlers (GET, POST, ...) that take a Request and return a Response.`);
      return apiFailure(rs, 500);
    }
    try {
      const request = attachCookies(
        new Request(fullURL(rs), {
          method: rs.method,
          headers: new Headers(rs.request.headers),
          body: body && body.byteLength > 0 ? (body as unknown as BodyInit) : undefined,
          signal: rs.request.signal,
        }),
      );
      const scope: RevalidateScope = { kind: "api", paths: [], tags: [] };
      const out = await revalidateStore.run(scope, async () => toResponse(await fn(request, { params: apiRouteParams(file, rs.path) })));
      await flushRevalidate(scope);
      writeHeaderPairs(rs.res, headerPairs(out.headers));
      const status = out.status < 100 || out.status > 999 ? 502 : out.status;
      if (headOnly || rs.method === "HEAD" || !out.body || NO_BODY_STATUS.has(status)) {
        if (out.body) void out.body.cancel().catch(() => {});
        return finish(rs, status, null);
      }
      return new Response(out.body, { status, headers: rs.res });
    } catch (err) {
      // notFound(), forbidden() and unauthorized() end the request with that status.
      const fallback = httpFallbackStatus(err);
      if (fallback === 403 || fallback === 401) return apiFailure(rs, fallback);
      if (fallback === 404) {
        rs.res.set("Content-Type", "application/json");
        rs.res.set("Cache-Control", "no-store");
        return finish(rs, 404, '{"error":"Not Found"}');
      }
      console.error(`[krate] API error (${rs.method} ${rs.path}): ${(err && (err as any).stack) || err}`);
      return apiFailure(rs, 500);
    }
  }

  // Server actions

  const actionById = new Map(data.actions.map((a) => [a.id, a]));
  const ACTION_COOKIE_NAME = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;

  function serializeActionCookie(name: string, value: unknown, opts?: any): string {
    const o = opts || {};
    if (!ACTION_COOKIE_NAME.test(name)) throw new TypeError("invalid cookie name: " + name);
    let s = name + "=" + encodeURIComponent(String(value));
    const p = o.path === undefined ? "/" : String(o.path);
    if (p && !/[;\r\n]/.test(p)) s += "; Path=" + p;
    if (o.domain && !/[;\r\n]/.test(String(o.domain))) s += "; Domain=" + o.domain;
    if (typeof o.maxAge === "number" && isFinite(o.maxAge)) s += "; Max-Age=" + Math.floor(o.maxAge);
    if (o.expires instanceof Date) s += "; Expires=" + o.expires.toUTCString();
    if (o.httpOnly !== false) s += "; HttpOnly";
    const same = o.sameSite === undefined ? "Lax" : o.sameSite;
    const sameText = same ? (same === true ? "Strict" : String(same).replace(/[^A-Za-z]/g, "")) : "";
    // Browsers drop SameSite=None cookies that are not Secure.
    if (o.secure || sameText.toLowerCase() === "none") s += "; Secure";
    if (sameText) s += "; SameSite=" + sameText;
    return s;
  }

  function parseActionCookies(header: string | null): Map<string, string> {
    const jar = new Map<string, string>();
    for (const part of String(header || "").split(";")) {
      const i = part.indexOf("=");
      if (i <= 0) continue;
      const name = part.slice(0, i).trim();
      if (!name || jar.has(name)) continue;
      let value = part.slice(i + 1).trim();
      if (value.startsWith('"') && value.endsWith('"')) value = value.slice(1, -1);
      try {
        value = decodeURIComponent(value);
      } catch {
        // keep the raw value
      }
      jar.set(name, value);
    }
    return jar;
  }

  function makeActionContext(headers: Headers, id: string, urlPath: string) {
    const jar = parseActionCookies(headers.get("cookie"));
    const setCookies: string[] = [];
    // Cookies set over https are Secure unless the action says otherwise.
    const https = isHTTPSRequest(headers);
    const withSecure = (opts?: any) => Object.assign({}, opts, { secure: opts && opts.secure !== undefined ? opts.secure : https });
    const cookies = {
      get(name: string) {
        return jar.get(name);
      },
      getAll() {
        return Array.from(jar, ([name, value]) => ({ name, value }));
      },
      has(name: string) {
        return jar.has(name);
      },
      set(name: string, value: unknown, opts?: any) {
        setCookies.push(serializeActionCookie(name, value, withSecure(opts)));
        jar.set(name, String(value));
      },
      delete(name: string, opts?: any) {
        setCookies.push(serializeActionCookie(name, "", withSecure(Object.assign({}, opts, { maxAge: 0 }))));
        jar.delete(name);
      },
    };
    return { actionId: id, url: urlPath, method: "POST", headers, cookies, setCookies };
  }

  function safeActionRedirect(value: unknown): string {
    if (typeof value !== "string") return "";
    const to = value.trim();
    if (!to || to.charAt(0) !== "/" || to.charAt(1) === "/" || to.indexOf("\\") !== -1) return "";
    if (/[\u0000-\u001f\u007f]/.test(to)) return "";
    return to;
  }

  function sanitizeActionReturn(value: string, rs: ReqState): string {
    value = value.trim();
    if (!value) return "";
    if (value.startsWith("/") && !value.startsWith("//") && !/[\\\r\n]/.test(value) && !/[\u0000-\u001f\u007f]/.test(value)) return value;
    let u: URL;
    try {
      u = new URL(value);
    } catch {
      return "";
    }
    if (!u.host) return "";
    if (u.host.toLowerCase() !== requestHost(rs).toLowerCase() || (u.protocol !== "http:" && u.protocol !== "https:")) return "";
    return u.pathname + u.search;
  }

  function actionHeaders(type: string, cookies?: string[]): Headers {
    const h = new Headers({ "content-type": type, "cache-control": "no-store" });
    for (const c of cookies || []) h.append("set-cookie", c);
    return h;
  }

  function actionFail(status: number, message: string, digest?: string, cookies?: string[]): Response {
    const error: Record<string, string> = { message };
    if (digest) error.digest = digest;
    return new Response(JSON.stringify({ ok: false, error }), { status, headers: actionHeaders("application/json", cookies) });
  }

  /**
   * actionFallback answers an action that called notFound(), forbidden() or
   * unauthorized(): JSON for the fetch client, else the nearest segment page of
   * the page the form was posted from (the sanitized return path).
   */
  function actionFallback(rs: ReqState, status: 401 | 403 | 404, fetchMode: boolean, cookies: string[], returnTo: string): Response {
    const label = status === 403 ? "Forbidden" : status === 401 ? "Unauthorized" : "Not Found";
    if (fetchMode) return actionFail(status, label, undefined, cookies);
    const headers = actionHeaders("text/html; charset=utf-8", cookies);
    headers.set("Cache-Control", "no-store");
    let from = "/";
    try {
      if (returnTo) from = safeDecode(new URL(returnTo, "http://x").pathname);
      if (basePath && (from === basePath || from.startsWith(basePath + "/"))) from = from.slice(basePath.length) || "/";
    } catch {
      from = "/";
    }
    const page = errorPage(from, status);
    if (page) return new Response(page.match ? renderSegmentPage(page.html, page.match) : page.html, { status, headers });
    headers.set("Content-Type", "text/plain; charset=utf-8");
    return new Response(status + " " + label + "\n", { status, headers });
  }

  function randomDigest(): string {
    const bytes = new Uint8Array(6);
    crypto.getRandomValues(bytes);
    return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
  }

  function leadingActionArgs(raw: unknown): unknown[] | null {
    if (raw == null || raw === "") return [];
    try {
      const v = JSON.parse(String(raw));
      return Array.isArray(v) ? v : [];
    } catch {
      return null;
    }
  }

  async function actionHandler(rs: ReqState): Promise<Response> {
    const limit = cfg.maxAPIBodyBytes;
    const fetchModeHeader = (rs.headers.get("x-krate-action") || "").toLowerCase() === "fetch";
    const wantsJSON = fetchModeHeader || (rs.headers.get("accept") || "").includes("application/json");
    const fail = (status: number, msg: string): Response => {
      rs.res.set("Cache-Control", "no-store");
      if (wantsJSON) {
        rs.res.set("Content-Type", "application/json");
        return finish(rs, status, JSON.stringify({ error: { message: msg }, ok: false }) + "\n");
      }
      return httpError(rs, msg, status);
    };
    rs.res.set("Cache-Control", "no-store");
    if (rs.method !== "POST") {
      rs.res.set("Allow", "POST");
      return fail(405, "method not allowed");
    }
    if (!sameOriginAction(rs)) return fail(403, "cross-site server action rejected");
    const declared = Number(rs.headers.get("content-length") || 0);
    if (limit > 0 && declared > limit) return fail(413, "server action request body too large");
    let ret = sanitizeActionReturn(new URLSearchParams(rs.search).get("__krate_return") || "", rs);
    if (!ret) ret = sanitizeActionReturn(rs.headers.get("x-krate-return") || "", rs);
    if (!ret && !fetchModeHeader) ret = sanitizeActionReturn(rs.headers.get("referer") || "", rs);

    // Re-run through the sidecar semantics (krateRunAction).
    const reqHeaders = new Headers(rs.request.headers);
    if (ret) reqHeaders.set("x-krate-return", ret);
    else reqHeaders.delete("x-krate-return");
    const site = (reqHeaders.get("sec-fetch-site") || "").toLowerCase();
    if (site === "cross-site") return withHeaders(rs, actionFail(403, "cross-site server action rejected"));
    const origin = reqHeaders.get("origin");
    const host = requestHost(rs);
    if (origin && host) {
      try {
        if (new URL(origin).host !== host) return withHeaders(rs, actionFail(403, "cross-origin server action rejected"));
      } catch {
        return withHeaders(rs, actionFail(403, "invalid action origin"));
      }
    }
    let id = "";
    try {
      id = decodeURIComponent(rs.path.slice("/__krate/action/".length));
    } catch {
      // not decodable: no action matches
    }
    const entry = actionById.get(id);
    if (!entry) return withHeaders(rs, actionFail(404, "Server action not found"));
    const mod = modules.actions && modules.actions[entry.file];
    if (!mod) {
      console.error(`[krate] server action module ${entry.file} is not part of this deployment`);
      return withHeaders(rs, actionFail(500, "Server action failed"));
    }
    const fn = entry.export === "default" ? mod.default || mod[entry.export] : mod[entry.export];
    if (typeof fn !== "function") return withHeaders(rs, actionFail(500, "Server action export is not a function"));

    const fetchMode = String(reqHeaders.get("x-krate-action") || "") === "fetch";
    const bodyBytes = await readBody(rs, limit);
    if (bodyBytes === "too-large") return withHeaders(rs, actionFail(413, "server action request body too large"));
    const query = new URLSearchParams(rs.search);
    let args: unknown[];
    try {
      const probe = new Request(fullURL(rs), {
        method: "POST",
        headers: reqHeaders,
        body: bodyBytes && bodyBytes.byteLength > 0 ? (bodyBytes as unknown as BodyInit) : undefined,
      });
      if (String(reqHeaders.get("content-type") || "").includes("application/json")) {
        const payload: any = await probe.json();
        args = payload && Array.isArray(payload.args) ? payload.args : [];
      } else {
        const fd = await probe.formData();
        let raw: unknown = fd.get("__krate_args");
        fd.delete("__krate_args");
        if (raw == null) raw = query.get("__krate_args");
        const lead = leadingActionArgs(raw);
        if (lead === null) throw new Error("bad __krate_args");
        args = lead;
        args.push(fd);
      }
    } catch {
      return withHeaders(rs, actionFail(400, "Invalid server action request body"));
    }

    const ctx = makeActionContext(reqHeaders, id, rs.path);
    const scope: RevalidateScope = { kind: "action", paths: [], tags: [] };
    let result: any;
    let redirect = "";
    let failure: any = null;
    let fallback: 401 | 403 | 404 | 0 = 0;
    try {
      result = await revalidateStore.run(scope, () => actionStore.run(ctx, () => fn(...args)));
    } catch (e: any) {
      if (e && typeof e.__krateRedirect === "string") redirect = e.__krateRedirect;
      else if ((fallback = httpFallbackStatus(e))) failure = null;
      else failure = e === undefined ? new Error("undefined") : e;
    }
    let revalidation: { paths: unknown[]; tags: string[] } | null = null;
    try {
      revalidation = await flushRevalidate(scope);
    } catch (e) {
      if (!failure) failure = e;
    }
    if (!redirect && result && typeof result === "object" && typeof result.redirect === "string") redirect = result.redirect;
    redirect = safeActionRedirect(redirect);
    if (fallback && !failure) return withHeaders(rs, actionFallback(rs, fallback, fetchMode, ctx.setCookies, reqHeaders.get("x-krate-return") || ""));
    let body = "";
    if (!failure && fetchMode) {
      const envelope: Record<string, unknown> = { ok: true, result: result === undefined ? null : result };
      if (redirect) envelope.redirect = redirect;
      if (revalidation) {
        envelope.revalidated = true;
        envelope.revalidation = revalidation;
      }
      try {
        body = JSON.stringify(envelope);
      } catch (e) {
        failure = e;
      }
    }
    if (failure) {
      const digest = randomDigest();
      console.error(`[krate] server action ${entry.export} (${entry.source || id}) failed, digest ${digest}:`, failure);
      if (fetchMode) return withHeaders(rs, actionFail(500, "Server action failed", digest, ctx.setCookies));
      return withHeaders(
        rs,
        new Response("Server action failed (digest " + digest + ")\n", { status: 500, headers: actionHeaders("text/plain; charset=utf-8", ctx.setCookies) }),
      );
    }
    if (fetchMode) return withHeaders(rs, new Response(body, { status: 200, headers: actionHeaders("application/json", ctx.setCookies) }));
    const headers = actionHeaders("text/plain; charset=utf-8", ctx.setCookies);
    headers.set("location", redirect || reqHeaders.get("x-krate-return") || "/");
    // The returned state would be lost across the 303; carry it in a short-lived cookie.
    if (!redirect && result !== undefined) {
      try {
        const json = JSON.stringify({ a: id, r: result });
        const bytes = new TextEncoder().encode(json);
        let bin = "";
        for (const b of bytes) bin += String.fromCharCode(b);
        const flash = btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
        if (flash.length <= 3000) headers.append("set-cookie", FLASH_COOKIE + "=" + flash + "; Path=/; Max-Age=60; SameSite=Lax" + (isHTTPSRequest(reqHeaders) ? "; Secure" : ""));
        else console.warn(`[krate] action ${entry.export} returned state too large to carry across the redirect; it is not shown after a no-JS submit`);
      } catch {
        // unserializable state is not carried
      }
    }
    return withHeaders(rs, new Response(null, { status: 303, headers }));
  }

  /** Layers a handler-built Response on the headers the outer layers set (security, middleware). */
  function withHeaders(rs: ReqState, out: Response): Response {
    const merged = new Headers(rs.res);
    writeHeaderPairs(merged, headerPairs(out.headers));
    const noBody = NO_BODY_STATUS.has(out.status) || rs.method === "HEAD";
    return new Response(noBody ? null : out.body, { status: out.status, headers: merged });
  }

  function sameOriginAction(rs: ReqState): boolean {
    if ((rs.headers.get("sec-fetch-site") || "").toLowerCase() === "cross-site") return false;
    const origin = (rs.headers.get("origin") || "").trim();
    if (!origin) return true;
    let u: URL;
    try {
      u = new URL(origin);
    } catch {
      return false;
    }
    if (!u.host) return false;
    return u.host.toLowerCase() === requestHost(rs).toLowerCase();
  }

  // The chain

  /** flashNoticeMiddleware: render pending no-JS form feedback into a page. */
  async function flashNotice(rs: ReqState, next: (rs: ReqState) => Promise<Response>): Promise<Response> {
    if (rs.method !== "GET" || (extName(rs.path) !== "" && !isHTMLDocRequest(rs.path))) return next(rs);
    const flash = requestFlash(rs.headers.get("cookie"));
    if (!flash) return next(rs);
    // The page is rendered fresh with the notice: drop conditional and range headers.
    const headers = new Headers(rs.headers);
    for (const h of ["If-None-Match", "If-Modified-Since", "If-Match", "If-Unmodified-Since", "If-Range", "Range"]) headers.delete(h);
    rs.headers = headers;
    const out = await next(rs);
    if (out.status !== 200 || !(out.headers.get("content-type") || "").startsWith("text/html") || out.headers.get("content-encoding")) return out;
    const text = await out.text();
    const injected = injectFlashNotice(text, flash.raw, flash.payload);
    const outHeaders = new Headers(out.headers);
    if (injected === null) return new Response(text, { status: out.status, headers: outHeaders });
    outHeaders.set("Cache-Control", "no-store");
    outHeaders.delete("Etag");
    outHeaders.delete("Last-Modified");
    outHeaders.append("Set-Cookie", FLASH_COOKIE + "=; Path=/; Max-Age=0; SameSite=Lax");
    return new Response(injected, { status: 200, headers: outHeaders });
  }

  async function pageChain(rs: ReqState): Promise<Response> {
    return flashNotice(rs, async (rs2) => {
      const blocked = await runMiddleware(rs2);
      if (blocked) return blocked;
      return redirectRewriteHandler(rs2);
    });
  }

  async function mux(rs: ReqState): Promise<Response> {
    const p = rs.path;
    // net/http.ServeMux cleans the escaped path and redirects when it changed.
    const raw = rs.rawPath;
    const cleaned = cleanPath(raw) + (raw.endsWith("/") && raw !== "/" ? "/" : "");
    if (raw !== "" && cleaned !== raw && rs.method !== "CONNECT") {
      let target = cleaned;
      if (rs.search) target += rs.search;
      return goRedirect(target, p, rs.method, 307, rs.res);
    }
    if (p === "/healthz") return textResponse(rs, 200, "ok\n");
    if (p === "/readyz") return textResponse(rs, 200, "ready\n");
    if (p === "/api") return goRedirect("/api/", p, rs.method, 307, rs.res);
    if (p.startsWith("/api/")) {
      const blocked = await runMiddleware(rs);
      if (blocked) return blocked;
      return apiHandler(rs);
    }
    if (p.startsWith("/__krate/action/") && data.actions.length > 0) {
      const blocked = await runMiddleware(rs);
      if (blocked) return blocked;
      return actionHandler(rs);
    }
    return pageChain(rs);
  }

  function applySecurityAndHeaders(rs: ReqState): void {
    const sec = cfg.security;
    const h = rs.res;
    h.set("X-Request-Id", "req-" + ++requestSeq);
    if (sec.nosniff) h.set("X-Content-Type-Options", "nosniff");
    if (sec.frameOptions) h.set("X-Frame-Options", sec.frameOptions);
    if (sec.referrerPolicy) h.set("Referrer-Policy", sec.referrerPolicy);
    if (sec.permissionsPolicy) h.set("Permissions-Policy", sec.permissionsPolicy);
    if (sec.hsts) h.set("Strict-Transport-Security", sec.hsts);
    if (sec.csp) h.set("Content-Security-Policy", sec.csp);
    for (const rule of cfg.headers) {
      if (headerSourceMatches(rule.source, rs.path)) for (const [k, v] of Object.entries(rule.headers)) h.set(k, v);
    }
  }

  /** corsMiddleware; returns a Response for a preflight. */
  function applyCORS(rs: ReqState): Response | null {
    const c = cfg.cors;
    if (!c.enabled) return null;
    const allowAll = corsOrigins.length === 1 && corsOrigins[0] === "*";
    const hasWildcard = corsOrigins.includes("*");
    const origin = rs.headers.get("origin") || "";
    let allowed = "";
    if (allowAll && c.credentials) {
      // A wildcard cannot be combined with credentials.
    } else if (allowAll) {
      allowed = "*";
    } else if (origin && corsOrigins.some((o) => o === "*" || o.toLowerCase() === origin.toLowerCase()) && !(c.credentials && hasWildcard)) {
      allowed = origin;
      rs.res.append("Vary", "Origin");
    }
    if (allowed) {
      const h = rs.res;
      h.set("Access-Control-Allow-Origin", allowed);
      h.set("Access-Control-Allow-Methods", (c.methods.length ? c.methods : ["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"]).join(", "));
      h.set("Access-Control-Allow-Headers", (c.headers.length ? c.headers : ["Content-Type", "Authorization"]).join(", "));
      if (c.credentials) h.set("Access-Control-Allow-Credentials", "true");
      if (c.maxAge > 0) h.set("Access-Control-Max-Age", String(c.maxAge));
    }
    if (rs.method === "OPTIONS" && rs.headers.get("access-control-request-method")) return finish(rs, 204, null);
    return null;
  }

  return async function handle(request: Request, ctx: HandlerContext = {}): Promise<Response> {
    if (ctx.waitUntil) latestWaitUntil = ctx.waitUntil.bind(ctx);
    const url = new URL(request.url);
    const rs: ReqState = {
      request,
      method: request.method.toUpperCase(),
      path: safeDecode(url.pathname),
      rawPath: url.pathname,
      search: url.search,
      res: new Headers(),
      headers: request.headers,
      origin: requestOrigin(request, url),
      ctx,
    };
    try {
      // Mount under the base path: requests outside it redirect (GET/HEAD) or 404.
      if (basePath) {
        const p = rs.path;
        if (!(p.startsWith("/__krate/") || p === "/healthz" || p === "/readyz")) {
          if (p === basePath || p === basePath + "/") rs.path = "/";
          else if (p.startsWith(basePath + "/")) rs.path = p.slice(basePath.length);
          else {
            applySecurityAndHeaders(rs);
            if (rs.method === "GET" || rs.method === "HEAD") return goRedirect(basePath + (p.startsWith("/") ? p : "/" + p), p, rs.method, 307, rs.res);
            return notFoundText(rs);
          }
        }
      }
      applySecurityAndHeaders(rs);
      const preflight = applyCORS(rs);
      if (preflight) return preflight;
      await sleepMicrotask();
      return await mux(rs);
    } catch (err) {
      console.error(`[krate] panic recovered: ${rs.method} ${rs.path}: ${(err && (err as any).stack) || err}`);
      if (rs.path.startsWith("/api/")) {
        rs.res.set("Content-Type", "application/json");
        return finish(rs, 500, '{"error":"Internal Server Error"}');
      }
      return httpError(rs, "Internal Server Error", 500);
    }
  };
}

export { isDynamicPattern };
