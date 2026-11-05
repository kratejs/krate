/**
 * Request context for server components, exported from `@krate/runtime/server`.
 *
 * The SSR sidecar renders every page and region inside an AsyncLocalStorage
 * scope and publishes it on `globalThis[Symbol.for('krate.request.store')]`.
 * `headers()` and `cookies()` read that scope while a server component renders
 * (and the action scope while a server action runs), so the same imports work
 * in both places.
 */

/** The request a server component is rendering for. */
export interface RequestContext {
  url: string;
  method: string;
  headers: Headers;
  /** Request cookies parsed from the Cookie header. */
  cookies: Map<string, string>;
  params: Record<string, string>;
  searchParams: Record<string, string>;
}

interface RequestStore {
  getStore(): RequestContext | undefined;
}

const REQUEST_STORE = Symbol.for('krate.request.store');

/** The current render request, or undefined outside a server render. */
export function getRequestContext(): RequestContext | undefined {
  const store = (globalThis as Record<symbol, unknown>)[REQUEST_STORE] as RequestStore | undefined;
  return store && typeof store.getStore === 'function' ? store.getStore() : undefined;
}

/** Parses a Cookie request header into name/value pairs. */
export function parseCookieHeader(header: string | null | undefined): Map<string, string> {
  const out = new Map<string, string>();
  if (!header) return out;
  for (const part of header.split(';')) {
    const eq = part.indexOf('=');
    if (eq <= 0) continue;
    const name = part.slice(0, eq).trim();
    let value = part.slice(eq + 1).trim();
    if (value.startsWith('"') && value.endsWith('"')) value = value.slice(1, -1);
    try {
      value = decodeURIComponent(value);
    } catch {
      // keep the raw value when it is not valid percent-encoding
    }
    if (name && !out.has(name)) out.set(name, value);
  }
  return out;
}

/**
 * thenable makes a value usable both synchronously and with `await`, matching
 * Next.js 15 where `params`, `searchParams`, `cookies()` and `headers()` are
 * Promises: `params.slug` and `(await params).slug` both work. The `then` is
 * non-enumerable, so spreading or JSON-serializing the value is unaffected, and
 * it resolves to a plain copy so awaiting never loops.
 */
export function thenable<T extends object>(value: T, resolved: () => unknown): T & PromiseLike<T> {
  Object.defineProperty(value, 'then', {
    value: (onFulfilled?: (v: any) => any, onRejected?: (e: any) => any) =>
      Promise.resolve(resolved()).then(onFulfilled, onRejected),
    enumerable: false,
    configurable: true,
  });
  return value as T & PromiseLike<T>;
}

/** thenableRecord wraps a plain record (route params, search params). */
export function thenableRecord<T extends Record<string, any>>(rec: T | undefined): T & PromiseLike<T> {
  const plain = { ...(rec || {}) } as T;
  return thenable({ ...plain }, () => ({ ...plain }));
}

/** Read-only cookie view used while a server component renders. */
export interface RequestCookies {
  get(name: string): { name: string; value: string } | undefined;
  getAll(): Array<{ name: string; value: string }>;
  has(name: string): boolean;
  set(name: string, value: string, options?: unknown): never;
  delete(name: string, options?: unknown): never;
}

export function requestCookies(jar: Map<string, string>): RequestCookies {
  const readOnly = (op: string): never => {
    throw new Error(
      `cookies().${op}() cannot be called while a server component renders; ` +
        `modify cookies in a server action ("use server") or an API route instead`,
    );
  };
  const make = (): RequestCookies => ({
    get: (name) => (jar.has(name) ? { name, value: jar.get(name)! } : undefined),
    getAll: () => [...jar.entries()].map(([name, value]) => ({ name, value })),
    has: (name) => jar.has(name),
    set: () => readOnly('set'),
    delete: () => readOnly('delete'),
  });
  return thenable(make(), make);
}

/** Thrown by notFound(); the sidecar turns it into a 404 response. */
export interface NotFoundSignal extends Error {
  __krateNotFound: true;
  digest: string;
}

/**
 * Stops rendering and responds with the 404 page. Works in server components
 * (async pages and their children) rendered by the SSR sidecar.
 */
export function notFound(): never {
  const err = new Error('NEXT_NOT_FOUND') as NotFoundSignal;
  err.__krateNotFound = true;
  err.digest = 'NEXT_NOT_FOUND';
  throw err;
}

/** isNotFoundSignal reports whether err was thrown by notFound(). */
export function isNotFoundSignal(err: unknown): boolean {
  if (!err || typeof err !== 'object') return false;
  const e = err as Record<string, unknown>;
  return e.__krateNotFound === true || e.digest === 'NEXT_NOT_FOUND' || e.message === 'NEXT_NOT_FOUND' ||
    (typeof e.digest === 'string' && e.digest.startsWith('NEXT_HTTP_ERROR_FALLBACK;404'));
}

/** Thrown by forbidden() and unauthorized(); the host answers 403 or 401. */
export interface HTTPFallbackSignal extends Error {
  __krateHTTPStatus: 401 | 403;
  digest: string;
}

function httpFallback(status: 401 | 403): never {
  const err = new Error(status === 403 ? 'NEXT_FORBIDDEN' : 'NEXT_UNAUTHORIZED') as HTTPFallbackSignal;
  err.__krateHTTPStatus = status;
  err.digest = 'NEXT_HTTP_ERROR_FALLBACK;' + status;
  throw err;
}

/**
 * Stops rendering and responds with 403 and the nearest forbidden page
 * (forbidden.tsx or 403.tsx). Works in pages, layouts, async server
 * components, server actions and API routes.
 */
export function forbidden(): never {
  return httpFallback(403);
}

/**
 * Stops rendering and responds with 401 and the nearest unauthorized page
 * (unauthorized.tsx or 401.tsx). Works wherever forbidden() does.
 */
export function unauthorized(): never {
  return httpFallback(401);
}

/**
 * accessDeniedStatus returns 403 or 401 when err was thrown by forbidden() or
 * unauthorized() (or the matching Next.js digest), else 0.
 */
export function accessDeniedStatus(err: unknown): 401 | 403 | 0 {
  if (!err || typeof err !== 'object') return 0;
  const e = err as Record<string, unknown>;
  if (e.__krateHTTPStatus === 403 || e.__krateHTTPStatus === 401) return e.__krateHTTPStatus;
  if (typeof e.digest === 'string') {
    if (e.digest === 'NEXT_HTTP_ERROR_FALLBACK;403') return 403;
    if (e.digest === 'NEXT_HTTP_ERROR_FALLBACK;401') return 401;
  }
  return 0;
}

/** httpFallbackStatus is 404, 403 or 401 for notFound(), forbidden() and unauthorized() signals, else 0. */
export function httpFallbackStatus(err: unknown): 401 | 403 | 404 | 0 {
  if (isNotFoundSignal(err)) return 404;
  return accessDeniedStatus(err);
}

/** redirectTarget returns the URL of a redirect() signal, or "" for other errors. */
export function redirectTarget(err: unknown): string {
  if (!err || typeof err !== 'object') return '';
  const e = err as Record<string, unknown>;
  if (typeof e.__krateRedirect === 'string') return e.__krateRedirect;
  if (typeof e.message === 'string' && e.message.startsWith('NEXT_REDIRECT:')) return e.message.slice('NEXT_REDIRECT:'.length);
  if (typeof e.digest === 'string' && e.digest.startsWith('NEXT_REDIRECT;')) {
    const parts = e.digest.split(';');
    if (parts.length >= 3) return parts[2];
  }
  return '';
}
