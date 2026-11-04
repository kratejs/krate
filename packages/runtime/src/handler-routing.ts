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
  if (sa.length !== sb.length) return sb.length - sa.length;
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

/** path.Base for slash paths. */
export function baseName(p: string): string {
  if (p === "") return ".";
  let end = p.length;
  while (end > 0 && p[end - 1] === "/") end--;
  if (end === 0) return "/";
  const s = p.slice(0, end);
  const i = s.lastIndexOf("/");
  return i >= 0 ? s.slice(i + 1) : s;
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

// Redirect and rewrite rules

function splitURLPath(p: string): string[] {
  const t = trimSlashes(p);
  return t === "" ? [] : t.split("/");
}

function splitParamModifier(name: string): [string, string] {
  const n = name.length;
  if (n > 0) {
    const c = name[n - 1];
    if (c === "*" || c === "+" || c === "?") return [name.slice(0, n - 1), c];
  }
  return [name, ""];
}

export interface RuleMatch {
  params: Record<string, string>;
  mods: Record<string, string>;
}

/**
 * Matches a request path against a redirect/rewrite source: exact, a trailing
 * "/*" wildcard (param "splat"), or named segments with the "*", "+" and "?"
 * modifiers.
 */
export function matchRedirectParamsMods(path: string, source: string): RuleMatch | null {
  const params: Record<string, string> = {};
  const mods: Record<string, string> = {};
  if (source === path) return { params, mods };
  if (!/[*:]/.test(source)) return null;
  if (source.endsWith("/*")) {
    const prefix = source.slice(0, -2);
    if (path === prefix) {
      params.splat = "";
      return { params, mods };
    }
    if (!path.startsWith(prefix + "/")) return null;
    params.splat = path.slice(prefix.length).replace(/^\//, "");
    return { params, mods };
  }
  const sourceParts = splitURLPath(source);
  const pathParts = splitURLPath(path);
  let pi = 0;
  for (const part of sourceParts) {
    if (part.startsWith(":")) {
      const [name, mod] = splitParamModifier(part.slice(1));
      mods[name] = mod;
      switch (mod) {
        case "*":
          params[name] = pathParts.slice(pi).join("/");
          return { params, mods };
        case "+":
          if (pi >= pathParts.length) return null;
          params[name] = pathParts.slice(pi).join("/");
          return { params, mods };
        case "?":
          if (pi < pathParts.length) {
            params[name] = pathParts[pi];
            pi++;
          } else {
            params[name] = "";
          }
          continue;
      }
      if (pi >= pathParts.length) return null;
      params[name] = pathParts[pi];
      pi++;
      continue;
    }
    if (pi >= pathParts.length || part !== pathParts[pi]) return null;
    pi++;
  }
  if (pi !== pathParts.length) return null;
  return { params, mods };
}

export function matchRedirect(path: string, source: string): boolean {
  return matchRedirectParamsMods(path, source) !== null;
}

/** url.PathEscape of Go: segment-escapes everything but unreserved and $&+=:@. */
export function pathEscape(s: string): string {
  return encodeURIComponent(s)
    .replace(/[!'()*]/g, (c) => "%" + c.charCodeAt(0).toString(16).toUpperCase())
    .replace(/%(24|26|2B|3D|3A|40)/g, (_m, h: string) => String.fromCharCode(parseInt(h, 16)));
}

/** Percent-encodes a matched (decoded) value segment by segment, keeping "/" between segments. */
export function escapeRedirectValue(v: string): string {
  if (v === "") return "";
  return v.split("/").map(pathEscape).join("/");
}

function replaceDestParams(destination: string, params: Record<string, string>, mods: Record<string, string>): string {
  const re = /:([A-Za-z_][A-Za-z0-9_]*)/g;
  let out = "";
  let last = 0;
  let m: RegExpExecArray | null;
  while ((m = re.exec(destination)) !== null) {
    const name = m[1];
    if (!Object.prototype.hasOwnProperty.call(params, name)) continue;
    let end = m.index + m[0].length;
    const mod = mods[name];
    if (mod && end < destination.length && destination[end] === mod) end++;
    out += destination.slice(last, m.index) + escapeRedirectValue(params[name]);
    last = end;
    re.lastIndex = end;
  }
  return out + destination.slice(last);
}

/** Keeps a substituted path destination on-site (no "//host" or "/\\host" results). */
export function sanitizeRedirectTarget(configured: string, out: string): string {
  if (!configured.startsWith("/") || configured.startsWith("//") || configured.startsWith("/\\")) return out;
  out = out.replace(/[\u0000-\u001f\u007f]/g, (c) => "%" + c.charCodeAt(0).toString(16).toUpperCase().padStart(2, "0"));
  const trimmed = out.replace(/^[/\\]+/, "");
  return trimmed !== out ? "/" + trimmed : out;
}

/** Fills ":splat" and ":name" placeholders of a destination from the matched path. */
export function rewriteDestination(path: string, source: string, destination: string): string {
  const m = matchRedirectParamsMods(path, source);
  if (!m) return destination;
  let out: string;
  if (Object.prototype.hasOwnProperty.call(m.params, "splat")) {
    const splat = m.params.splat;
    if (destination.includes(":splat")) out = destination.split(":splat").join(escapeRedirectValue(splat));
    else if (splat !== "") out = destination.replace(/\/+$/, "") + "/" + escapeRedirectValue(splat);
    else out = destination;
  } else {
    out = replaceDestParams(destination, m.params, m.mods);
  }
  return sanitizeRedirectTarget(destination, out);
}

// Header rules

/** path.Match for slash paths: "*" and "?" do not cross "/", "[...]" classes. */
export function pathMatch(pattern: string, name: string): boolean {
  let re = "^";
  for (let i = 0; i < pattern.length; i++) {
    const c = pattern[i];
    if (c === "*") re += "[^/]*";
    else if (c === "?") re += "[^/]";
    else if (c === "[") {
      const end = pattern.indexOf("]", i + 1);
      if (end < 0) return false;
      let cls = pattern.slice(i + 1, end);
      let neg = false;
      if (cls.startsWith("^")) {
        neg = true;
        cls = cls.slice(1);
      }
      re += "[" + (neg ? "^" : "") + cls.replace(/\\/g, "\\\\") + "]";
      i = end;
    } else if (c === "\\" && i + 1 < pattern.length) {
      re += pattern[++i].replace(/[.*+?^${}()|[\]\\/]/g, "\\$&");
    } else {
      re += c.replace(/[.*+?^${}()|[\]\\/]/g, "\\$&");
    }
  }
  try {
    return new RegExp(re + "$").test(name);
  } catch {
    return false;
  }
}

/** Exact path, trailing "*" prefix, or path.Match glob (config `headers[].source`). */
export function headerSourceMatches(pattern: string, p: string): boolean {
  pattern = pattern.trim();
  if (pattern === "") return false;
  if (!pattern.includes("*")) return p === pattern;
  if (pattern.endsWith("*")) return p.startsWith(pattern.slice(0, -1));
  return pathMatch(pattern, p);
}

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
  let out = at < 0 ? templateHTML : templateHTML.slice(0, at) + injectScript + templateHTML.slice(at);
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

export function staticCacheControl(urlPath: string, hashed: ReadonlySet<string>): string {
  if (hashed.has(urlPath)) return "public, max-age=31536000, immutable";
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
  if (method === "GET") body = "<a href=\"" + goHTMLEscape(headers.get("Location")!) + "\">" + (statusText[status] || "") + "</a>.\n\n";
  return new Response(body, { status, headers });
}
