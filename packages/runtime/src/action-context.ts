/**
 * Request context for server actions, exported from `@krate/runtime/server`.
 *
 * The action sidecar runs every action inside an AsyncLocalStorage scope and
 * publishes it on `globalThis[Symbol.for('krate.action.store')]`. These helpers
 * read that scope, so they only work while a server action is executing.
 */

import { getRequestContext, requestCookies, thenable } from './request-context.js';

/** Options for `cookies().set()`. HttpOnly, Path=/ and SameSite=Lax are the defaults. */
export interface ActionCookieOptions {
  path?: string;
  domain?: string;
  maxAge?: number;
  expires?: Date;
  httpOnly?: boolean;
  secure?: boolean;
  sameSite?: 'Strict' | 'Lax' | 'None' | boolean;
}

/** Cookie access for the current action request. */
export interface ActionCookies {
  get(name: string): string | undefined;
  getAll(): Array<{ name: string; value: string }>;
  has(name: string): boolean;
  /** Adds a Set-Cookie header to the action response. */
  set(name: string, value: string, options?: ActionCookieOptions): void;
  delete(name: string, options?: ActionCookieOptions): void;
}

/** The request a server action is running for. */
export interface ActionContext {
  actionId: string;
  url: string;
  method: string;
  headers: Headers;
  cookies: ActionCookies;
}

interface ActionStore {
  getStore(): ActionContext | undefined;
}

/** The current action context, or undefined outside a server action. */
export function getActionContext(): ActionContext | undefined {
  const store = (globalThis as Record<symbol, unknown>)[Symbol.for('krate.action.store')] as ActionStore | undefined;
  return store && typeof store.getStore === 'function' ? store.getStore() : undefined;
}

function requireContext(api: string): ActionContext {
  const ctx = getActionContext();
  if (!ctx) {
    throw new Error(
      `${api}() can only be called while a server action runs (inside a "use server" export) ` +
        `or while a server component (an async component) renders in the SSR sidecar`,
    );
  }
  return ctx;
}

/**
 * Request headers of the current server action call or server component
 * render. The result can also be awaited (`await headers()`), as in Next.js 15.
 */
export function headers(): Headers {
  const action = getActionContext();
  if (action) return awaitableHeaders(action.headers);
  const req = getRequestContext();
  if (req) return awaitableHeaders(req.headers);
  return requireContext('headers').headers;
}

/**
 * Cookies of the current server action call, or a read-only view of the
 * request cookies while a server component renders (`get(name)` returns
 * `{ name, value }` there, like Next.js). Can also be awaited.
 */
export function cookies(): ActionCookies {
  const action = getActionContext();
  if (action) return awaitableActionCookies(action.cookies);
  const req = getRequestContext();
  if (req) return requestCookies(req.cookies) as unknown as ActionCookies;
  return requireContext('cookies').cookies;
}

function awaitableActionCookies(c: ActionCookies): ActionCookies {
  const view = (): ActionCookies => ({
    get: (name) => c.get(name),
    getAll: () => c.getAll(),
    has: (name) => c.has(name),
    set: (name, value, options) => c.set(name, value, options),
    delete: (name, options) => c.delete(name, options),
  });
  return thenable(view(), view);
}

function awaitableHeaders(h: Headers): Headers {
  const plain = new Headers(h);
  const view = new Headers(h);
  return thenable(view, () => plain);
}

/**
 * Ends the server action and navigates the client to `url` (a JSON
 * `redirect` for enhanced forms, a 303 for native form posts). Returning
 * `{ redirect: url }` from the action has the same effect.
 */
export function redirect(url: string): never {
  const err = new Error('Server action redirect to ' + url) as Error & { __krateRedirect: string };
  err.__krateRedirect = url;
  throw err;
}
