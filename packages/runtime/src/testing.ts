/**
 * Test helpers, exported from `@krate/runtime/testing`. They call an API route
 * module or a server action in-process, without a running server. Node only
 * (it uses AsyncLocalStorage); import it from your test files, never from app
 * code.
 */

import { AsyncLocalStorage } from "node:async_hooks";

export type RouteMethod = "GET" | "POST" | "PUT" | "PATCH" | "DELETE" | "HEAD" | "OPTIONS";

export type RouteHandler = (
  request: Request,
  context: { params: Record<string, string> },
) => Response | Promise<Response>;

/** An imported `src/api/*.ts` module: named method exports and/or a default Web handler. */
export type RouteModule = Partial<Record<RouteMethod, RouteHandler>> & { default?: unknown };

export interface CallRouteOptions {
  /** Dynamic segments of the route (`[id]` -> `{ id: "7" }`). */
  params?: Record<string, string>;
  headers?: HeadersInit;
  /** A JSON body; sets `content-type: application/json`. */
  json?: unknown;
  /** A raw body (string, FormData, ...). Ignored when `json` is set. */
  body?: BodyInit | null;
}

const METHODS: RouteMethod[] = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"];

/**
 * Calls a route module the way the server does: the named export for `method`
 * (HEAD falls back to GET without a body, OPTIONS answers `Allow`), else a
 * default Web-style handler, else a 405 with `Allow`. `target` is a path
 * (`"/api/users/7?x=1"`), an absolute URL, or a ready `Request`.
 */
export async function callRoute(
  mod: RouteModule,
  method: RouteMethod,
  target: string | Request,
  options: CallRouteOptions = {},
): Promise<Response> {
  const request = toRequest(method, target, options);
  const context = { params: options.params ?? {} };
  const named = (m: RouteMethod): RouteHandler | undefined => (typeof mod[m] === "function" ? mod[m] : undefined);
  const fallback = typeof mod.default === "function" ? (mod.default as (...a: unknown[]) => unknown) : undefined;
  if (fallback && fallback.length >= 2) {
    throw new TypeError("callRoute: a default export with a (req, res) signature is the legacy Node style and is not supported; export named GET/POST handlers instead");
  }

  const direct = named(method);
  if (direct) return direct(request, context);
  if (method === "HEAD" && named("GET")) {
    const res = await named("GET")!(request, context);
    return new Response(null, { status: res.status, statusText: res.statusText, headers: res.headers });
  }
  if (fallback && !METHODS.some((m) => named(m))) return (await fallback(request, context)) as Response;

  const allow = METHODS.filter((m) => named(m) || (m === "HEAD" && named("GET")));
  if (allow.indexOf("OPTIONS") < 0) allow.push("OPTIONS");
  if (method === "OPTIONS") return new Response(null, { status: 204, headers: { Allow: allow.join(", ") } });
  return new Response("Method Not Allowed", { status: 405, headers: { Allow: allow.join(", ") } });
}

function toRequest(method: RouteMethod, target: string | Request, options: CallRouteOptions): Request {
  if (typeof target !== "string") return target;
  const url = /^https?:\/\//i.test(target) ? target : "http://localhost" + (target.startsWith("/") ? target : "/" + target);
  const headers = new Headers(options.headers);
  let body: BodyInit | null | undefined = options.body;
  if (options.json !== undefined) {
    body = JSON.stringify(options.json);
    if (!headers.has("content-type")) headers.set("content-type", "application/json");
  }
  const hasBody = method !== "GET" && method !== "HEAD" && body !== undefined && body !== null;
  return new Request(url, { method, headers, body: hasBody ? body : undefined });
}

export interface ActionCallOptions {
  /** Request headers the action sees through `headers()`. */
  headers?: HeadersInit;
  /** Cookies the request carried; the action reads them with `cookies()`. */
  cookies?: Record<string, string>;
  /** The page URL path the action was submitted from. */
  url?: string;
  /** The action id; only visible through `getActionContext()`. */
  actionId?: string;
}

export interface SetCookieCall {
  name: string;
  /** Empty string for a delete. */
  value: string;
  options?: Record<string, unknown>;
  deleted: boolean;
}

export interface ActionCallResult<T> {
  /** What the action returned. A thrown error (including `redirect()`) rejects instead. */
  result: T;
  /** Every `cookies().set()` / `delete()` call, in order. */
  setCookies: SetCookieCall[];
}

interface Store {
  getStore(): unknown;
}

const ACTION_STORE = Symbol.for("krate.action.store");
const als = new AsyncLocalStorage<unknown>();

/**
 * Runs a server action function with the request context a real call has, so
 * `cookies()`, `headers()` and `getActionContext()` from `@krate/runtime/server`
 * work. The action is the plain exported function you imported from the module.
 */
export async function runAction<T>(
  action: () => T | Promise<T>,
  options: ActionCallOptions = {},
): Promise<ActionCallResult<T>> {
  const jar = new Map(Object.entries(options.cookies ?? {}));
  const setCookies: SetCookieCall[] = [];
  const cookies = {
    get: (name: string) => jar.get(name),
    getAll: () => Array.from(jar, ([name, value]) => ({ name, value })),
    has: (name: string) => jar.has(name),
    set: (name: string, value: string, opts?: Record<string, unknown>) => {
      setCookies.push({ name, value: String(value), options: opts, deleted: false });
      jar.set(name, String(value));
    },
    delete: (name: string, opts?: Record<string, unknown>) => {
      setCookies.push({ name, value: "", options: opts, deleted: true });
      jar.delete(name);
    },
  };
  const context = {
    actionId: options.actionId ?? "test-action",
    url: options.url ?? "/",
    method: "POST",
    headers: new Headers(options.headers),
    cookies,
  };
  // Published once and left in place: concurrent runAction calls share it, and
  // each call's context lives in its own async scope.
  (globalThis as Record<symbol, unknown>)[ACTION_STORE] = als as Store;
  const result = await als.run(context, async () => action());
  return { result, setCookies };
}
