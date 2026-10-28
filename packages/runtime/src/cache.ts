/**
 * `@krate/runtime/cache` - on-demand revalidation and the server data cache,
 * mirroring `next/cache`. Also re-exported from `@krate/runtime/server`.
 *
 * - `revalidatePath(path, type?)` and `revalidateTag(tag)` invalidate the
 *   host's ISR page cache, region caches and data cache. Inside a server
 *   action or an API route the invalidation is applied before the response
 *   is sent (read-your-writes); actions also tell the client which paths and
 *   tags changed. They throw when called while a page renders or outside the
 *   Krate server (for example during a build).
 * - `unstable_cache(fn, keyParts, { tags, revalidate })` caches an async
 *   function's JSON-serializable result.
 * - `unstable_noStore()` marks the current render uncacheable.
 */
import {
  cacheRuntime,
  currentRenderScope,
  currentRevalidateScope,
  dataCacheGetJSON,
  dataCacheSetJSON,
  markNoStore,
  recordCacheRead,
} from './cache-scope.js';

/** What `revalidatePath` invalidates: the page at path, or every page under it. */
export type RevalidatePathType = 'page' | 'layout';

/** A revalidation request as sent to the Krate host. */
export interface RevalidatePayload {
  paths: Array<{ path: string; type: RevalidatePathType }>;
  tags: string[];
}

/** The host's answer: what was invalidated and what could not be. */
export interface RevalidateResult {
  revalidated: Array<{ target: string; kind: 'path' | 'tag'; routes?: string[] }>;
  skipped: Array<{ target: string; reason: string }>;
}

interface GoRevalidateBridge {
  _go_revalidate?: (payloadJSON: string) => string;
}

function env(name: string): string {
  const proc = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process;
  return (proc && proc.env && proc.env[name]) || '';
}

/** Logs targets the host could not revalidate (static pages, unknown routes). */
export function reportSkippedRevalidations(result: RevalidateResult | null | undefined): void {
  if (!result || !Array.isArray(result.skipped)) return;
  for (const s of result.skipped) {
    console.warn(`[krate] revalidation skipped for ${s.target}: ${s.reason}`);
  }
}

/** Sends a revalidation to the host (used outside a request scope). */
export async function sendRevalidation(payload: RevalidatePayload): Promise<RevalidateResult> {
  const host = env('KRATE_HOST_URL');
  const secret = env('KRATE_INTERNAL_SECRET');
  if (!host || !secret) {
    throw new Error('revalidation needs the Krate server: KRATE_HOST_URL / KRATE_INTERNAL_SECRET are not set');
  }
  const res = await fetch(host.replace(/\/$/, '') + '/__krate/internal/revalidate', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-krate-internal': secret },
    body: JSON.stringify(payload),
  });
  const text = await res.text();
  if (!res.ok) throw new Error(`revalidation failed: host answered ${res.status} ${text}`);
  const result = JSON.parse(text) as RevalidateResult;
  reportSkippedRevalidations(result);
  return result;
}

function request(api: string, payload: RevalidatePayload): void {
  if (currentRenderScope()) {
    throw new Error(
      `${api}() cannot be called while a page renders. Call it from a server action ("use server") or an API route after the data changed.`,
    );
  }
  // Same-process data cache entries go right away; the host fans the tags
  // out to every other process and worker.
  if (payload.tags.length) cacheRuntime().invalidate({ tags: payload.tags });

  const scope = currentRevalidateScope();
  if (scope) {
    scope.paths.push(...payload.paths);
    scope.tags.push(...payload.tags);
    return;
  }
  const go = (globalThis as GoRevalidateBridge)._go_revalidate;
  if (typeof go === 'function') {
    // Embedded QuickJS API route: the Go host applies it synchronously.
    const out = JSON.parse(go(JSON.stringify(payload))) as RevalidateResult & { error?: string };
    if (out.error) throw new Error(`${api}() failed: ${out.error}`);
    reportSkippedRevalidations(out);
    return;
  }
  if (env('KRATE_HOST_URL') && env('KRATE_INTERNAL_SECRET') && typeof fetch === 'function') {
    // Server code outside a request (a timer, a queue consumer): send now.
    sendRevalidation(payload).catch((err) => {
      console.error(`[krate] ${api}() failed:`, err);
    });
    return;
  }
  throw new Error(
    `${api}() can only run on the Krate server: in a server action, an API route, or server code under \`krate serve\` / \`krate dev\` (KRATE_HOST_URL is not set, e.g. during a build).`,
  );
}

/**
 * Invalidates the cached render of `path`. `type: "page"` (the default for a
 * concrete URL) targets that page; a dynamic pattern such as `/blog/[slug]`
 * targets every cached variant; `type: "layout"` targets every page under
 * the path.
 */
export function revalidatePath(path: string, type?: RevalidatePathType): void {
  if (typeof path !== 'string' || !path.startsWith('/')) {
    throw new TypeError(`revalidatePath() expects an absolute path starting with "/", got ${JSON.stringify(path)}`);
  }
  if (type !== undefined && type !== 'page' && type !== 'layout') {
    throw new TypeError(`revalidatePath() type must be "page" or "layout", got ${JSON.stringify(type)}`);
  }
  request('revalidatePath', { paths: [{ path, type: type || 'page' }], tags: [] });
}

/** Invalidates every cached page render, region and data entry that read `tag`. */
export function revalidateTag(tag: string): void {
  if (typeof tag !== 'string' || tag === '') {
    throw new TypeError(`revalidateTag() expects a non-empty string tag, got ${JSON.stringify(tag)}`);
  }
  request('revalidateTag', { paths: [], tags: [tag] });
}

/** Options for `unstable_cache`. */
export interface UnstableCacheOptions {
  /** Tags that `revalidateTag` can invalidate this entry by. */
  tags?: string[];
  /** Seconds to keep the result; false (default) keeps it until invalidated. */
  revalidate?: number | false;
}

const pending = new Map<string, Promise<unknown>>();

/**
 * Caches the JSON-serializable result of an async function. The cache key is
 * the function source, keyParts and the call arguments. Results expire after
 * `revalidate` seconds (never when false/omitted) and are invalidated by
 * `revalidateTag` for any of `tags`. When called during a render, the tags
 * and window are recorded on the page's cache entry too.
 */
export function unstable_cache<A extends unknown[], R>(
  fn: (...args: A) => Promise<R> | R,
  keyParts?: string[],
  options: UnstableCacheOptions = {},
): (...args: A) => Promise<R> {
  if (typeof fn !== 'function') throw new TypeError('unstable_cache() expects a function');
  const tags = (options.tags || []).filter((t) => typeof t === 'string' && t !== '');
  const revalidate = options.revalidate;
  if (revalidate !== undefined && revalidate !== false && !(typeof revalidate === 'number' && revalidate >= 0)) {
    throw new TypeError('unstable_cache() revalidate must be a number of seconds or false');
  }
  const ttl = typeof revalidate === 'number' ? revalidate : Infinity;
  const base = String(fn) + '\u0000' + JSON.stringify(keyParts || []);
  return async (...args: A): Promise<R> => {
    recordCacheRead(tags, revalidate === undefined ? false : revalidate);
    if (ttl === 0) return fn(...args);
    const key = 'uc:' + base + '\u0000' + JSON.stringify(args);
    const hit = dataCacheGetJSON(key);
    if (hit !== undefined) return JSON.parse(hit) as R;
    const running = pending.get(key);
    if (running) return running as Promise<R>;
    const epoch = cacheRuntime().epoch;
    const p = (async () => {
      const value = await fn(...args);
      const json = JSON.stringify(value);
      if (json === undefined) {
        throw new TypeError('unstable_cache() results must be JSON-serializable (got undefined or a function)');
      }
      dataCacheSetJSON(key, json, ttl, tags, epoch);
      return value;
    })();
    pending.set(key, p);
    try {
      return await p;
    } finally {
      pending.delete(key);
    }
  };
}

/**
 * Opts the current render out of caching: an ISR page rendered with it is
 * served fresh and not stored. It has no effect outside a render.
 */
export function unstable_noStore(): void {
  markNoStore();
}
