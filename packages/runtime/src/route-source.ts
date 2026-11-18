// Redirect, rewrite and header sources, has/missing conditions and
// destinations. This is the TypeScript twin of the Go package
// internal/routesource and the helpers in build/routesource.go; keep the two
// in step. The syntax is the path-to-regexp 6 subset Next.js uses, plus
// Krate's "/x/*" splat form and the header globs.

export type SourceKind = "redirect" | "rewrite" | "header";

export interface RouteCondition {
  type: "query" | "header" | "cookie" | "host" | string;
  key?: string;
  value?: string;
}

export interface MatchResult {
  params: Record<string, string>;
  mods: Record<string, string>;
}

export interface CompiledSource {
  match(path: string): MatchResult | null;
}

interface Tok {
  lit?: string;
  param?: boolean;
  name?: string;
  prefix?: string;
  suffix?: string;
  pattern?: string;
  mod?: string;
}

const DEFAULT_PATTERN = "[^/#?]+?";

function lex(s: string): Array<[string, string]> {
  const out: Array<[string, string]> = [];
  const word = /[0-9A-Za-z_]/;
  for (let i = 0; i < s.length; ) {
    const c = s[i];
    if (c === "*" || c === "+" || c === "?") {
      out.push(["MODIFIER", c]);
      i++;
    } else if (c === "\\") {
      if (i + 1 >= s.length) throw new Error("a trailing backslash");
      out.push(["ESCAPED", s[i + 1]]);
      i += 2;
    } else if (c === "{") {
      out.push(["OPEN", c]);
      i++;
    } else if (c === "}") {
      out.push(["CLOSE", c]);
      i++;
    } else if (c === ":") {
      let j = i + 1;
      while (j < s.length && word.test(s[j])) j++;
      if (j === i + 1) throw new Error("missing parameter name");
      out.push(["NAME", s.slice(i + 1, j)]);
      i = j;
    } else if (c === "(") {
      let count = 1;
      let j = i + 1;
      if (s[j] === "?") throw new Error('a pattern cannot start with "?"');
      let pat = "";
      while (j < s.length) {
        if (s[j] === "\\") {
          pat += s[j] + (s[j + 1] ?? "");
          j += 2;
          continue;
        }
        if (s[j] === ")") {
          count--;
          if (count === 0) {
            j++;
            break;
          }
        } else if (s[j] === "(") {
          count++;
          if (s[j + 1] !== "?") throw new Error("capturing groups are not allowed");
        }
        pat += s[j++];
      }
      if (count) throw new Error("unbalanced pattern");
      if (!pat) throw new Error("missing pattern");
      out.push(["PATTERN", pat]);
      i = j;
    } else {
      out.push(["CHAR", c]);
      i++;
    }
  }
  out.push(["END", ""]);
  return out;
}

function parse(s: string): Tok[] {
  const lt = lex(s);
  let i = 0;
  const tryC = (kind: string): string | undefined => (lt[i][0] === kind ? lt[i++][1] : undefined);
  const text = (): string => {
    let out = "";
    for (;;) {
      const v = tryC("CHAR") ?? tryC("ESCAPED");
      if (v === undefined) return out;
      out += v;
    }
  };
  const toks: Tok[] = [];
  let lit = "";
  const flush = () => {
    if (lit) toks.push({ lit });
    lit = "";
  };
  while (lt[i][0] !== "END") {
    const ch = tryC("CHAR");
    const name = tryC("NAME");
    const pat = tryC("PATTERN");
    if (name !== undefined || pat !== undefined) {
      let prefix = ch ?? "";
      if (prefix !== "." && prefix !== "/") {
        lit += prefix;
        prefix = "";
      }
      flush();
      toks.push({ param: true, name: name ?? "", prefix, suffix: "", pattern: pat ?? DEFAULT_PATTERN, mod: tryC("MODIFIER") ?? "" });
      continue;
    }
    if (ch !== undefined) {
      lit += ch;
      continue;
    }
    const esc = tryC("ESCAPED");
    if (esc !== undefined) {
      lit += esc;
      continue;
    }
    flush();
    if (tryC("OPEN") !== undefined) {
      const prefix = text();
      const n = tryC("NAME");
      const p = tryC("PATTERN");
      const suffix = text();
      if (tryC("CLOSE") === undefined) throw new Error('expected "}"');
      toks.push({ param: true, name: n ?? "", prefix, suffix, pattern: n !== undefined && p === undefined ? DEFAULT_PATTERN : (p ?? ""), mod: tryC("MODIFIER") ?? "" });
      continue;
    }
    throw new Error(`unexpected ${lt[i][0].toLowerCase()} ${JSON.stringify(lt[i][1])}`);
  }
  flush();
  return toks;
}

const quote = (s: string) => s.replace(/[.*+?^${}()|[\]\\/]/g, "\\$&");

/** Removes leading (?!...) groups from a parameter pattern. */
function splitLookaheads(p: string): { negs: string[]; rest: string } {
  const negs: string[] = [];
  while (p.startsWith("(?!")) {
    let depth = 1;
    let i = 3;
    let inClass = false;
    for (; i < p.length; i++) {
      const c = p[i];
      if (c === "\\") i++;
      else if (inClass) {
        if (c === "]") inClass = false;
      } else if (c === "[") inClass = true;
      else if (c === "(") depth++;
      else if (c === ")") depth--;
      if (depth === 0) break;
    }
    if (depth !== 0) throw new Error("unbalanced lookahead");
    negs.push(p.slice(3, i));
    p = p.slice(i + 1);
  }
  if (negs.length && p === "") throw new Error("a lookahead needs a pattern after it");
  return { negs, rest: p };
}

interface ParamInfo {
  name: string;
  group: number;
  mod: string;
  negs: RegExp[];
}

function compilePattern(src: string): CompiledSource {
  let pat = src;
  let splat = false;
  if (pat.endsWith("/*")) {
    pat = pat.slice(0, -2);
    splat = true;
  }
  const toks = parse(pat);
  let re = "^";
  let group = 0;
  const params: ParamInfo[] = [];
  for (const t of toks) {
    if (!t.param) {
      re += quote(t.lit!);
      continue;
    }
    const prefix = quote(t.prefix!);
    const suffix = quote(t.suffix!);
    const mod = t.mod!;
    if (!t.pattern) {
      re += `(?:${prefix}${suffix})${mod}`;
      continue;
    }
    const { negs, rest } = splitLookaheads(t.pattern);
    group++;
    params.push({ name: t.name!, group, mod, negs: negs.map((n) => new RegExp("^(?:" + n + ")")) });
    if ((mod === "+" || mod === "*") && (prefix || suffix)) {
      re += `(?:${prefix}((?:${rest})(?:${suffix}${prefix}(?:${rest}))*)${suffix})${mod === "*" ? "?" : ""}`;
    } else if (prefix || suffix) {
      re += `(?:${prefix}(${rest})${suffix})${mod}`;
    } else if (mod === "+" || mod === "*") {
      re += `((?:${rest})${mod})`;
    } else {
      re += `(${rest})${mod}`;
    }
  }
  let splatGroup = 0;
  if (splat) {
    splatGroup = ++group;
    re += "(?:/([\\s\\S]*))?$";
  } else {
    re += "/?$";
  }
  const rx = new RegExp(re);
  return {
    match(path: string) {
      const m = rx.exec(path);
      if (!m) return null;
      const out: MatchResult = { params: {}, mods: {} };
      for (const p of params) {
        const v = m[p.group];
        if (v !== undefined && p.negs.some((n) => n.test(v))) return null;
        if (!p.name) continue;
        out.mods[p.name] = p.mod;
        if (v !== undefined || p.mod === "?" || p.mod === "*") out.params[p.name] = v ?? "";
      }
      if (splatGroup) out.params.splat = m[splatGroup] ?? "";
      return out;
    },
  };
}

/** path.Match for slash paths: "*" and "?" do not cross "/", "[...]" classes. */
function pathMatch(pattern: string, name: string): boolean {
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
    } else if (c === "\\" && i + 1 < pattern.length) re += quote(pattern[++i]);
    else re += quote(c);
  }
  try {
    return new RegExp(re + "$").test(name);
  } catch {
    return false;
  }
}

/** Compiles a source. It throws when the source cannot be matched. */
export function compileSource(source: string, kind: SourceKind): CompiledSource {
  if (kind === "header") source = source.trim();
  const none = (): MatchResult => ({ params: {}, mods: {} });
  if (!/[:({]/.test(source)) {
    if (kind === "header") {
      return {
        match(p) {
          if (source === "") return null;
          if (!source.includes("*")) return p === source ? none() : null;
          if (source.endsWith("*")) return p.startsWith(source.slice(0, -1)) ? none() : null;
          return pathMatch(source, p) ? none() : null;
        },
      };
    }
    if (source.endsWith("/*")) {
      const prefix = source.slice(0, -2);
      return {
        match(p) {
          if (p === prefix) return { params: { splat: "" }, mods: {} };
          if (!p.startsWith(prefix + "/")) return null;
          return { params: { splat: p.slice(prefix.length + 1) }, mods: {} };
        },
      };
    }
    return { match: (p) => (p === source ? none() : null) };
  }
  return compilePattern(source);
}

interface CompiledCond extends RouteCondition {
  re?: RegExp;
}

const safeParamName = (key: string) => key.replace(/[^A-Za-z]/g, "");

export interface CondRequest {
  headers: Headers;
  url: URL;
}

function condValue(c: CompiledCond, req: CondRequest): string | undefined {
  let v: string | undefined;
  switch (c.type) {
    case "query": {
      const all = req.url.searchParams.getAll(c.key || "");
      v = all.length ? all[all.length - 1] : undefined;
      break;
    }
    case "header":
      v = req.headers.get(c.key || "") ?? undefined;
      break;
    case "cookie":
      for (const part of (req.headers.get("cookie") || "").split(";")) {
        const i = part.indexOf("=");
        if (i > 0 && part.slice(0, i).trim() === c.key) {
          const raw = part.slice(i + 1).trim();
          try {
            v = decodeURIComponent(raw);
          } catch {
            v = raw;
          }
          break;
        }
      }
      break;
    case "host":
      v = (req.headers.get("host") || req.url.host).split(":")[0].toLowerCase();
      break;
  }
  return v || undefined;
}

function condMatch(c: CompiledCond, req: CondRequest, into: Record<string, string>): boolean {
  const v = condValue(c, req);
  if (v === undefined) return false;
  if (!c.re) {
    const key = c.type === "header" ? (c.key || "").toLowerCase() : c.key || "";
    into[safeParamName(key)] = v;
    return true;
  }
  const m = c.re.exec(v);
  if (!m) return false;
  if (m.groups) Object.assign(into, m.groups);
  else if (c.type === "host" && m[0]) into.host = m[0];
  return true;
}

export interface Conditions {
  match(req: CondRequest): Record<string, string> | null;
}

/** Compiles has/missing; null when there are none. It throws on an invalid item. */
export function compileConditions(has: RouteCondition[] = [], missing: RouteCondition[] = []): Conditions | null {
  if (!has.length && !missing.length) return null;
  const one = (c: RouteCondition): CompiledCond => {
    if (!["query", "header", "cookie", "host"].includes(c.type)) throw new Error(`condition type must be query, header, cookie or host, got ${c.type}`);
    if (c.type !== "host" && !c.key) throw new Error(`condition type ${c.type} needs a key`);
    return { ...c, re: c.value ? new RegExp("^(?:" + c.value + ")$") : undefined };
  };
  const h = has.map(one);
  const m = missing.map(one);
  return {
    match(req) {
      const params: Record<string, string> = {};
      for (const c of h) if (!condMatch(c, req, params)) return null;
      for (const c of m) if (condMatch(c, req, {})) return null;
      return params;
    },
  };
}

/** A compiled rule: source plus conditions. */
export interface RouteMatcher {
  match(path: string, req: CondRequest): MatchResult | null;
}

export function compileMatcher(source: string, kind: SourceKind, has?: RouteCondition[], missing?: RouteCondition[]): RouteMatcher {
  const src = compileSource(source, kind);
  const cond = compileConditions(has, missing);
  return {
    match(path, req) {
      const m = src.match(path);
      if (!m) return null;
      if (!cond) return m;
      const extra = cond.match(req);
      if (!extra) return null;
      return { params: { ...m.params, ...extra }, mods: m.mods };
    },
  };
}

// Destinations

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

function escapeQueryValue(v: string): string {
  return encodeURIComponent(v).replace(/%2F/gi, "/");
}

function replaceDestParams(destination: string, params: Record<string, string>, mods: Record<string, string>): string {
  const queryAt = destination.indexOf("?");
  const re = /:([A-Za-z_][A-Za-z0-9_]*)/g;
  let out = "";
  let last = 0;
  let m: RegExpExecArray | null;
  while ((m = re.exec(destination)) !== null) {
    const name = m[1];
    if (name === "splat" || !Object.prototype.hasOwnProperty.call(params, name)) continue;
    let end = m.index + m[0].length;
    const mod = mods[name];
    if (mod && end < destination.length && destination[end] === mod) end++;
    out += destination.slice(last, m.index) + (queryAt >= 0 && m.index > queryAt ? escapeQueryValue(params[name]) : escapeRedirectValue(params[name]));
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

/** Fills ":splat" and ":name" placeholders of a destination from a match. */
export function buildDestination(destination: string, m: MatchResult): string {
  const params = m.params;
  let out: string;
  if (Object.prototype.hasOwnProperty.call(params, "splat")) {
    const splat = params.splat;
    if (destination.includes(":splat")) out = destination.split(":splat").join(escapeRedirectValue(splat));
    else if (splat !== "") out = destination.replace(/\/+$/, "") + "/" + escapeRedirectValue(splat);
    else out = destination;
    if (Object.keys(params).length > 1) out = replaceDestParams(out, params, m.mods);
  } else {
    out = replaceDestParams(destination, params, m.mods);
  }
  return sanitizeRedirectTarget(destination, out);
}

function parseQuery(raw: string): Array<[string, string[]]> {
  const out: Array<[string, string[]]> = [];
  const idx = new Map<string, number>();
  for (const part of raw.split("&")) {
    if (!part) continue;
    const eq = part.indexOf("=");
    let k: string;
    let v: string;
    try {
      k = decodeURIComponent((eq < 0 ? part : part.slice(0, eq)).replace(/\+/g, " "));
      v = decodeURIComponent((eq < 0 ? "" : part.slice(eq + 1)).replace(/\+/g, " "));
    } catch {
      continue;
    }
    const i = idx.get(k);
    if (i === undefined) {
      idx.set(k, out.length);
      out.push([k, [v]]);
    } else out[i][1].push(v);
  }
  return out;
}

/** Adds the request's query to the destination's own; the destination's keys win. */
export function mergeRequestQuery(dest: string, rawQuery: string): string {
  if (rawQuery.startsWith("?")) rawQuery = rawQuery.slice(1);
  if (!rawQuery) return dest;
  let frag = "";
  const h = dest.indexOf("#");
  if (h >= 0) {
    frag = dest.slice(h);
    dest = dest.slice(0, h);
  }
  let destQuery = "";
  const q = dest.indexOf("?");
  if (q >= 0) {
    destQuery = dest.slice(q + 1);
    dest = dest.slice(0, q);
  }
  const pairs = parseQuery(rawQuery);
  const idx = new Map(pairs.map(([k], i) => [k, i] as [string, number]));
  for (const [k, vals] of parseQuery(destQuery)) {
    const i = idx.get(k);
    if (i !== undefined) pairs[i][1] = vals;
    else {
      idx.set(k, pairs.length);
      pairs.push([k, vals]);
    }
  }
  const enc = (s: string) => encodeURIComponent(s);
  const qs = pairs.flatMap(([k, vals]) => vals.map((v) => enc(k) + "=" + enc(v))).join("&");
  return dest + (qs ? "?" + qs : "") + frag;
}
