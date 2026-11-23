// Pure routing and URL helpers for the portable server handler. Everything
// here is a port of the Go host (internal/build: routematch.go, serve.go,
// serve_pages.go, serve_hardening.go), so `krate start` and the serverless
// targets decide identically. No Node or platform imports.

// Route patterns

export type RouteSegKind = 0 | 1 | 2 | 3; // static, param, catch-all, optional catch-all

export interface RouteSeg {
  kind: RouteSegKind;
  text: string;
}

export function parseRoutePattern(pattern: string): RouteSeg[] {
  const trimmed = trimSlashes(pattern);
  if (trimmed === "" || trimmed === ".") return [];
  return trimmed.split("/").map(parseRouteSeg);
}

function parseRouteSeg(p: string): RouteSeg {
  if (p.startsWith("[[...") && p.endsWith("]]") && p.length > 7) return { kind: 3, text: p.slice(5, -2) };
  if (p.startsWith("[...") && p.endsWith("]") && p.length > 5) return { kind: 2, text: p.slice(4, -1) };
  if (p.startsWith("[") && p.endsWith("]") && p.length > 2) return { kind: 1, text: p.slice(1, -1) };
  return { kind: 0, text: p };
}

export function trimSlashes(s: string): string {
  let a = 0;
  let b = s.length;
  while (a < b && s.charCodeAt(a) === 47) a++;
  while (b > a && s.charCodeAt(b - 1) === 47) b--;
  return s.slice(a, b);
}

export function isDynamicPattern(pattern: string): boolean {
  return parseRoutePattern(pattern).some((s) => s.kind !== 0);
}

/**
 * Matches a URL path against a route pattern. A param matches one non-empty
 * segment; a catch-all one or more remaining segments, an optional catch-all
 * zero or more. Catch-all values are the matched segments joined with "/".
 */
export function matchRoutePattern(urlPath: string, pattern: string): Record<string, string> | null {
  const segs = parseRoutePattern(pattern);
  const trimmed = trimSlashes(urlPath);
  const parts = trimmed === "" ? [] : trimmed.split("/");
  const params: Record<string, string> = {};
  for (let i = 0; i < segs.length; i++) {
    const s = segs[i];
    if (s.kind === 2 || s.kind === 3) {
      if (i !== segs.length - 1) return null;
      const rest = parts.slice(Math.min(i, parts.length));
      if (s.kind === 2 && rest.length === 0) return null;
      for (const r of rest) if (r === "") return null;
      params[s.text] = rest.join("/");
      return params;
    }
    if (i >= parts.length) return null;
    if (s.kind === 1) {
      if (parts[i] === "") return null;
      params[s.text] = parts[i];
    } else if (parts[i] !== s.text) {
      return null;
    }
  }
  if (parts.length !== segs.length) return null;
  return params;
}

function strCompare(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0;
}

/** Negative when a is more specific than b: static > param > catch-all > optional catch-all. */
export function compareRouteSpecificity(a: string, b: string): number {
  const sa = parseRoutePattern(a);
  const sb = parseRoutePattern(b);
  for (let i = 0; i < sa.length && i < sb.length; i++) {
    if (sa[i].kind !== sb[i].kind) return sa[i].kind - sb[i].kind;
    if (sa[i].kind === 0 && sa[i].text !== sb[i].text) return strCompare(sa[i].text, sb[i].text);
  }
  if (sa.length !== sb.length) {
    // A trailing optional catch-all is the less specific: "shop" answers /shop before "shop/[[...x]]".
    if (sa.length > sb.length && sa[sb.length].kind === 3) return 1;
    if (sb.length > sa.length && sb[sa.length].kind === 3) return -1;
    return sb.length - sa.length;
  }
  return strCompare(a, b);
}

export function normalizeRoutePattern(p: string): string {
  p = p.trim();
  if (p === "") return "/";
  if (!p.startsWith("/")) p = "/" + p;
  if (p.length > 1) p = p.replace(/\/+$/, "") || "/";
  return p;
}

/** The route form used by the manifest: no trailing slash, "/" for the root. */
export function requestRoute(urlPath: string): string {
  const r = urlPath.replace(/\/+$/, "");
  return r === "" ? "/" : r;
}

export function isFileLikePath(p: string): boolean {
  return baseName(p).includes(".");
}

/**
 * The internal path a rewrite serves. Page URLs are canonical with a trailing
 * slash and the slashless spelling answers 301, so a rewrite to "/feedback" would
 * change the visible URL; the canonical form keeps it invisible. API routes,
 * internal paths and file-like paths are left alone.
 */
export function rewriteTargetPath(p: string): string {
  if (p === "" || p === "/" || p.endsWith("/") || isFileLikePath(p)) return p;
  if (p === "/api" || p.startsWith("/api/") || p.startsWith("/__krate/")) return p;
  return p + "/";
}

/** path.Base for slash paths. */
export function baseName(p: string): string {
  if (p === "") return ".";
  let end = p.length;
  while (end > 0 && p[end - 1] === "/") end--;
  if (end === 0) return "/";
  const s = p.slice(0, end);
  const i = s.lastIndexOf("/");
  return i !== -1 ? s.slice(i + 1) : s;
}

export function isHTMLDocRequest(urlPath: string): boolean {
  return urlPath === "" || urlPath.endsWith("/") || urlPath.endsWith(".html");
}

/** path.Ext for slash paths. */
export function extName(p: string): string {
  for (let i = p.length - 1; i >= 0; i--) {
    if (p[i] === ".") return p.slice(i);
    if (p[i] === "/") return "";
  }
  return "";
}

/** path.Clean for slash paths. */
export function cleanPath(p: string): string {
  if (p === "") return ".";
  const rooted = p[0] === "/";
  const out: string[] = [];
  for (const seg of p.split("/")) {
    if (seg === "" || seg === ".") continue;
    if (seg === "..") {
      if (out.length > 0 && out[out.length - 1] !== "..") out.pop();
      else if (!rooted) out.push("..");
      continue;
    }
    out.push(seg);
  }
  const joined = out.join("/");
  if (rooted) return "/" + joined;
  return joined === "" ? "." : joined;
}

function decodePath(p: string): string {
  try {
    return decodeURIComponent(p);
  } catch {
    return p;
  }
}

/** True for requests aimed at Krate's server-only output directory. */
export function isInternalOutputPath(urlPath: string): boolean {
  const p = cleanPath("/" + decodePath(urlPath).replace(/\\/g, "/"));
  if (p === "/.krate" || p.startsWith("/.krate/")) return true;
  // The shell of a segment page that renders per request is not a page of the site.
  if (/(^|\/)__(401|403|404|500)(\/(index\.html)?)?$/.test(p)) return true;
  return p === "/server-manifest.json" || p === "/api-node" || p.startsWith("/api-node/");
}

export function isHiddenName(name: string): boolean {
  return name.startsWith(".") && name !== ".well-known" && name !== "." && name !== "..";
}

/** True when any segment of a (decoded) URL path is a dotfile name. */
export function isHiddenPath(urlPath: string): boolean {
  const decoded = decodePath(urlPath);
  return decoded.split(/[/\\]/).filter(Boolean).some(isHiddenName);
}

// Redirect, rewrite and header rule matching lives in route-source.ts.

// Dynamic SSG templates

export const DYNAMIC_PARAM_PREFIX = "__KRATE_PARAM_";

export function dynamicParamSentinel(name: string): string {
  return DYNAMIC_PARAM_PREFIX + name + "__";
}

const htmlEscapes: Record<string, string> = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&#34;", "'": "&#39;" };

/** Go's html.EscapeString (used for dynamic template params). */
export function goHTMLEscape(s: string): string {
  return s.replace(/[&<>"']/g, (c) => htmlEscapes[c]);
}

/** escape.HTML of the compiler: & < > " ' */
export function escapeHTML(s: string): string {
  return s.replace(/[&<>"']/g, (c) => (c === '"' ? "&quot;" : htmlEscapes[c]));
}

/**
 * Renders a dynamic-route template for one request: injects the matched params
 * for client-side access and replaces every build-time sentinel.
 */
export function applyDynamicRouteParams(templateHTML: string, params: Record<string, string>): string {
  const injectScript = "<script>window.__KRATE_PARAMS__=" + goJSON(params) + "</script>";
  const at = templateHTML.indexOf("</head>");
  let out = at === -1 ? templateHTML : templateHTML.slice(0, at) + injectScript + templateHTML.slice(at);
  for (const [name, value] of Object.entries(params)) {
    out = out.split(dynamicParamSentinel(name)).join(goHTMLEscape(value));
  }
  return out;
}

/** json.Marshal of a string map: sorted keys, HTML-significant characters escaped. */
export function goJSON(obj: Record<string, string>): string {
  const keys = Object.keys(obj).sort();
  const enc = (s: string) =>
    JSON.stringify(s)
      .replace(/</g, "\\u003c")
      .replace(/>/g, "\\u003e")
      .replace(/&/g, "\\u0026")
      .replace(/\u2028/g, "\\u2028")
      .replace(/\u2029/g, "\\u2029");
  return "{" + keys.map((k) => enc(k) + ":" + enc(obj[k])).join(",") + "}";
}

// Hashed assets

/** Cache-Control of a content-hashed asset; a user headers rule never replaces it. */
export const IMMUTABLE_CACHE_CONTROL = "public, max-age=31536000, immutable";

export function staticCacheControl(urlPath: string, hashed: ReadonlySet<string>): string {
  if (hashed.has(urlPath)) return IMMUTABLE_CACHE_CONTROL;
  return "no-cache";
}

// Middleware matcher (path-to-regexp subset), shared with the Go runtimes

export function compileMatcher(src: unknown): RegExp {
  if (typeof src !== "string") throw new Error("middleware matcher must be a string or { source }");
  if (src.charAt(0) !== "/") throw new Error('middleware matcher "' + src + '" must start with "/"');
  let out = "";
  let i = 0;
  const n = src.length;
  const esc = (c: string) => c.replace(/[.*+?^${}()|[\]\\/]/g, "\\$&");
  while (i < n) {
    let c = src.charAt(i);
    let prefix = "";
    if ((c === "/" || c === ".") && i + 1 < n && (src.charAt(i + 1) === ":" || src.charAt(i + 1) === "(")) {
      prefix = c;
      i++;
      c = src.charAt(i);
    }
    if (c === ":" || c === "(") {
      let pattern = "";
      if (c === ":") {
        let j = i + 1;
        while (j < n && /[A-Za-z0-9_]/.test(src.charAt(j))) j++;
        i = j;
        if (src.charAt(i) === "(") {
          let depth = 0;
          let k = i;
          for (; k < n; k++) {
            const ch = src.charAt(k);
            if (ch === "\\") {
              k++;
              continue;
            }
            if (ch === "(") depth++;
            else if (ch === ")") {
              depth--;
              if (depth === 0) break;
            }
          }
          pattern = src.slice(i + 1, k);
          i = k + 1;
        }
      } else {
        let depth2 = 0;
        let k2 = i;
        for (; k2 < n; k2++) {
          const ch2 = src.charAt(k2);
          if (ch2 === "\\") {
            k2++;
            continue;
          }
          if (ch2 === "(") depth2++;
          else if (ch2 === ")") {
            depth2--;
            if (depth2 === 0) break;
          }
        }
        pattern = src.slice(i + 1, k2);
        i = k2 + 1;
      }
      let mod = src.charAt(i);
      if (mod === "*" || mod === "+" || mod === "?") i++;
      else mod = "";
      const body = pattern || "[^\\/]+?";
      const p = prefix ? esc(prefix) : "";
      if (mod === "*") out += "(?:" + p + "(" + body + "(?:" + esc(prefix || "/") + body + ")*))?";
      else if (mod === "+") out += p + "(" + body + "(?:" + esc(prefix || "/") + body + ")*)";
      else if (mod === "?") out += "(?:" + p + "(" + body + "))?";
      else out += p + "(" + body + ")";
      continue;
    }
    if (c === "*") {
      out += ".*";
      i++;
      continue;
    }
    out += esc(c);
    i++;
  }
  return new RegExp("^" + out + "\\/?$", "i");
}

// Go http.Redirect

function hexEscapeNonASCII(s: string): string {
  let out = "";
  const bytes = new TextEncoder().encode(s);
  let ascii = true;
  for (const b of bytes) if (b >= 0x80) ascii = false;
  if (ascii) return s;
  for (const b of bytes) out += b >= 0x80 ? "%" + b.toString(16).toUpperCase().padStart(2, "0") : String.fromCharCode(b);
  return out;
}

const statusText: Record<number, string> = {
  301: "Moved Permanently",
  302: "Found",
  303: "See Other",
  307: "Temporary Redirect",
  308: "Permanent Redirect",
};

/**
 * Builds the response http.Redirect writes: a relative target is resolved
 * against the request path and cleaned, Location is escaped, and a GET gets a
 * small HTML body.
 */
export function goRedirect(target: string, requestPath: string, method: string, status: number, extra?: Headers): Response {
  let url = target;
  let hasScheme = false;
  try {
    const u = new URL(target);
    hasScheme = u.protocol !== "" && /^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(target);
  } catch {
    hasScheme = false;
  }
  const schemeRelative = target.startsWith("//");
  if (!hasScheme && !schemeRelative) {
    const oldpath = requestPath === "" ? "/" : requestPath;
    if (url === "" || url[0] !== "/") {
      const slash = oldpath.lastIndexOf("/");
      url = oldpath.slice(0, slash + 1) + url;
    }
    let query = "";
    const qi = url.indexOf("?");
    if (qi !== -1) {
      query = url.slice(qi);
      url = url.slice(0, qi);
    }
    const trailing = url.endsWith("/");
    url = cleanPath(url);
    if (trailing && !url.endsWith("/")) url += "/";
    url += query;
  }
  const headers = extra ? new Headers(extra) : new Headers();
  const hadCT = headers.has("content-type");
  headers.set("Location", hexEscapeNonASCII(url));
  if (!hadCT && (method === "GET" || method === "HEAD")) headers.set("Content-Type", "text/html; charset=utf-8");
  let body: string | null = null;
  if (!hadCT && method === "GET") body = "<a href=\"" + goHTMLEscape(headers.get("Location")!) + "\">" + (statusText[status] || "") + "</a>.\n\n";
  return new Response(body, { status, headers });
}
