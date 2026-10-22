/**
 * Request context for server actions, exported from `@krate/runtime/server`.
 *
 * The action sidecar runs every action inside an AsyncLocalStorage scope and
 * publishes it on `globalThis[Symbol.for('krate.action.store')]`. These helpers
 * read that scope, so they only work while a server action is executing.
 */

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
    throw new Error(`${api}() can only be called while a server action runs (inside a "use server" export)`);
  }
  return ctx;
}

/** Request headers of the current server action call. */
export function headers(): Headers {
  return requireContext('headers').headers;
}

/** Cookies of the current server action call. */
export function cookies(): ActionCookies {
  return requireContext('cookies').cookies;
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
