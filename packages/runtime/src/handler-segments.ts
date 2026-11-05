// Per-segment not-found and error pages for the portable handler: a port of
// build/segments.go (LookupSegmentPage) and build/segmentserve.go
// (newServerError, fillServerError). Pure functions over manifest data.

import {
  applyDynamicRouteParams,
  compareRouteSpecificity,
  goHTMLEscape,
  matchRoutePattern,
  parseRoutePattern,
  trimSlashes,
} from "./handler-routing.js";

export type SegmentKind = "notFound" | "error" | "forbidden" | "unauthorized";

/** The segment page role that answers each status. */
export const SEGMENT_KIND_OF_STATUS: Record<401 | 403 | 404 | 500, SegmentKind> = {
  401: "unauthorized",
  403: "forbidden",
  404: "notFound",
  500: "error",
};

/** One directory's segment pages (manifest.json segments[]). */
export interface SegmentEntry {
  key: string;
  dir: string;
  group?: boolean;
  notFound?: string;
  error?: string;
  forbidden?: string;
  unauthorized?: string;
}

export interface SegmentMatch {
  /** Page path relative to the output dir ("blog/404.html"). */
  file: string;
  key: string;
  /** Dynamic-segment values when matched against a URL, else undefined. */
  params?: Record<string, string>;
}

function entryPage(e: SegmentEntry, kind: SegmentKind): string {
  switch (kind) {
    case "error":
      return e.error || "";
    case "forbidden":
      return e.forbidden || "";
    case "unauthorized":
      return e.unauthorized || "";
    default:
      return e.notFound || "";
  }
}

/**
 * Returns the nearest segment page of a kind. matched: the request matched a
 * route whose source directory is segDir, so the lookup walks up from it.
 * Otherwise the longest URL-prefix entry outside route groups wins (static >
 * [param] > catch-all).
 */
export function lookupSegmentPage(
  entries: SegmentEntry[],
  kind: SegmentKind,
  urlPath: string,
  matched: boolean,
  segDir: string,
): SegmentMatch | null {
  if (matched) {
    const byKey = new Map<string, SegmentEntry>();
    for (const e of entries) byKey.set(e.key, e);
    let dir = segDir;
    for (;;) {
      const e = byKey.get(dir);
      if (e && entryPage(e, kind) !== "") return { file: entryPage(e, kind), key: e.key };
      if (dir === "") return null;
      const i = dir.lastIndexOf("/");
      dir = i >= 0 ? dir.slice(0, i) : "";
    }
  }
  let best: SegmentEntry | null = null;
  let bestParams: Record<string, string> | undefined;
  for (const e of entries) {
    if (entryPage(e, kind) === "" || e.group) continue;
    const params = matchRoutePrefix(urlPath, e.dir);
    if (!params) continue;
    if (!best || compareRouteSpecificity(e.dir, best.dir) < 0) {
      best = e;
      bestParams = params;
    }
  }
  if (!best) return null;
  return { file: entryPage(best, kind), key: best.key, params: bestParams };
}

/** Matches the leading segments of urlPath; a trailing catch-all takes the rest. */
function matchRoutePrefix(urlPath: string, pattern: string): Record<string, string> | null {
  const segs = parseRoutePattern(pattern);
  if (segs.length === 0) return {};
  const trimmed = trimSlashes(urlPath);
  const parts = trimmed === "" ? [] : trimmed.split("/");
  const last = segs[segs.length - 1];
  if (last.kind === 2 || last.kind === 3) return matchRoutePattern(urlPath, pattern);
  if (parts.length < segs.length) return null;
  return matchRoutePattern("/" + parts.slice(0, segs.length).join("/"), pattern);
}

export interface ServerError {
  name: string;
  message: string;
  digest: string;
  stack?: string;
}

/** The error prop for a failed request: sanitized outside dev, digest always kept. */
export function newServerError(message: string, stack: string, digest: string, dev: boolean): ServerError {
  if (!dev) return { name: "Error", message: "Internal Server Error", digest };
  const e: ServerError = { name: "Error", message: message || "Internal Server Error", digest };
  if (stack) e.stack = stack;
  return e;
}

function escapeJSONForScript(s: string): string {
  return s.replace(/</g, "\\u003c").replace(/>/g, "\\u003e").replace(/&/g, "\\u0026").replace(/\u2028/g, "\\u2028").replace(/\u2029/g, "\\u2029");
}

/**
 * Turns a built error page into the response for one failed request: the error
 * placeholders are replaced with the escaped error and the error is injected
 * as a JSON data block before </head>.
 */
export function fillServerError(page: string, e: ServerError): string {
  const subs: Array<[string, string]> = [
    ["__KRATE_ERR_NAME__", e.name],
    ["__KRATE_ERR_MESSAGE__", e.message],
    ["__KRATE_ERR_DIGEST__", e.digest],
    ["__KRATE_ERR_STACK__", e.stack || ""],
  ];
  for (const [sentinel, val] of subs) page = page.split(sentinel).join(goHTMLEscape(val));
  const block = '<script type="application/json" id="__krate_error">' + escapeJSONForScript(JSON.stringify(e)) + "</script>";
  const at = page.indexOf("</head>");
  return at >= 0 ? page.slice(0, at) + block + page.slice(at) : block + page;
}

/** A segment page filled for one response (dynamic dir params, then the error). */
export function renderSegmentPage(html: string, m: SegmentMatch, err?: ServerError): string {
  let page = html;
  if (m.params && Object.keys(m.params).length > 0) page = applyDynamicRouteParams(page, m.params);
  if (err) page = fillServerError(page, err);
  return page;
}
