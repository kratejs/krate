import { AuthError, createContext, type RequestContext, type ResolvedConfig } from "./config.js";
import { ResponseCookies } from "./cookies.js";
import { startEmail } from "./email-flow.js";
import { startOAuth } from "./oauth-flow.js";
import { pagePath } from "./redirect.js";
import { readSession } from "./session.js";
import { handleCredentials, handleSignOut, resolveCallbackUrl } from "./signin.js";
import type { CredentialsConfig, EmailConfig, OAuthConfig, Session } from "./types.js";

// The request scope Krate publishes for server actions, route handlers (the
// action store) and server components (the request store), the same stores
// behind headers() and cookies() in @krate/runtime/server.
interface AmbientCookies {
  set?: (name: string, value: string, options?: Record<string, unknown>) => void;
  delete?: (name: string, options?: Record<string, unknown>) => void;
}

interface Ambient {
  headers: Headers;
  url: string;
  writable: boolean;
  cookies?: AmbientCookies;
}

function readStore(name: string): any {
  const store = (globalThis as Record<symbol, any>)[Symbol.for(name)];
  return store && typeof store.getStore === "function" ? store.getStore() : undefined;
}

function ambient(): Ambient | null {
  const action = readStore("krate.action.store");
  if (action?.headers) return { headers: action.headers, url: String(action.url ?? "/"), writable: true, cookies: action.cookies };
  const render = readStore("krate.request.store");
  if (render?.headers) return { headers: render.headers, url: String(render.url ?? "/"), writable: false };
  return null;
}

const FORWARDED = ["cookie", "host", "forwarded", "x-forwarded-for", "x-forwarded-host", "x-forwarded-proto", "x-real-ip", "user-agent", "cf-connecting-ip"];

// A GET Request for the current scope. Only identity headers are copied: accept
// and content-type would make the flow answer with JSON instead of a redirect.
function scopeRequest(amb: Ambient): Request {
  const h = amb.headers;
  const host = (h.get("x-forwarded-host") ?? h.get("host") ?? "localhost").split(",")[0]!.trim();
  const proto = (h.get("x-forwarded-proto") ?? (/^(localhost|127\.|\[::1\])/.test(host) ? "http" : "https")).split(",")[0]!.trim();
  let url: string;
  try {
    url = new URL(amb.url, `${proto}://${host}`).toString();
  } catch {
    url = `${proto}://${host}/`;
  }
  const headers = new Headers();
  for (const name of FORWARDED) {
    const value = h.get(name);
    if (value !== null) headers.set(name, value);
  }
  return new Request(url, { method: "GET", headers });
}

function requireScope(api: string, needWrite: boolean): Ambient {
  const amb = ambient();
  if (!amb) {
    throw new AuthError("Configuration", `${api}() with no request needs a request scope: call it in a server action, a route handler or a server component (or pass the Request)`, 500);
  }
  if (needWrite && !amb.writable) {
    throw new AuthError("Configuration", `${api}() sets cookies, so it can run in a server action or a route handler, not while a server component renders`, 500);
  }
  return amb;
}

interface ParsedCookie {
  name: string;
  value: string;
  options: Record<string, unknown>;
}

function parseSetCookie(line: string): ParsedCookie | null {
  const parts = line.split(";").map((p) => p.trim());
  const first = parts.shift();
  const eq = first ? first.indexOf("=") : -1;
  if (!first || eq <= 0) return null;
  const raw = first.slice(eq + 1);
  let value = raw;
  try {
    value = decodeURIComponent(raw);
  } catch {
    // keep the raw value
  }
  const options: Record<string, unknown> = { httpOnly: false, secure: false };
  for (const part of parts) {
    const i = part.indexOf("=");
    const key = (i < 0 ? part : part.slice(0, i)).toLowerCase();
    const val = i < 0 ? "" : part.slice(i + 1);
    if (key === "path") options.path = val;
    else if (key === "domain") options.domain = val;
    else if (key === "max-age") options.maxAge = Number(val);
    else if (key === "expires") options.expires = new Date(val);
    else if (key === "httponly") options.httpOnly = true;
    else if (key === "secure") options.secure = true;
    else if (key === "samesite") options.sameSite = val.charAt(0).toUpperCase() + val.slice(1).toLowerCase();
  }
  return { name: first.slice(0, eq), value, options };
}

function applyCookies(amb: Ambient, lines: string[]): void {
  if (lines.length === 0) return;
  if (!amb.writable || typeof amb.cookies?.set !== "function") {
    throw new AuthError("Configuration", "cookies cannot be set in this scope", 500);
  }
  for (const line of lines) {
    const c = parseSetCookie(line);
    if (c) amb.cookies.set(c.name, c.value, c.options);
  }
}

function redirectSignal(url: string, external: boolean): Error {
  const err = new Error("Server action redirect to " + url) as Error & { __krateRedirect: string; __krateRedirectExternal?: boolean; digest: string };
  err.__krateRedirect = url;
  if (external) err.__krateRedirectExternal = true;
  err.digest = `NEXT_REDIRECT;replace;${url};307;`;
  return err;
}

export interface SignInOptions {
  /** Where to land after sign-in. Defaults to the configured signed-in page or "/". */
  redirectTo?: string;
  /** Alias of `redirectTo` kept for Auth.js code. */
  callbackUrl?: string;
  /** false returns the target URL instead of redirecting. */
  redirect?: boolean;
  /** Credentials and email fields (`email`, `password`, ...). */
  [field: string]: unknown;
}

export interface SignOutOptions {
  redirectTo?: string;
  callbackUrl?: string;
  /** false returns the target URL instead of redirecting. */
  redirect?: boolean;
}

// Hands the flow's Response to the caller: its cookies go to the scope's jar and
// its Location becomes a thrown redirect (or the returned URL with redirect:false).
async function finish(ctx: RequestContext, amb: Ambient, response: Response, redirect: boolean): Promise<string | undefined> {
  applyCookies(amb, response.headers.getSetCookie());
  const location = response.headers.get("location");
  if (!location) {
    if (response.status >= 400) throw new AuthError("Configuration", `auth responded with ${response.status}`, response.status);
    return undefined;
  }
  let target = location;
  let external = false;
  try {
    const u = new URL(location, ctx.baseUrl);
    if (u.origin === ctx.baseUrl) target = u.pathname + u.search + u.hash;
    else external = true;
  } catch {
    // leave as given
  }
  if (redirect === false) return external ? target : ctx.baseUrl + target;
  throw redirectSignal(target, external);
}

export async function serverSignIn(config: ResolvedConfig, providerId?: string, options: SignInOptions = {}, authorizationParams?: Record<string, string>): Promise<string | undefined> {
  const amb = requireScope("signIn", true);
  const ctx = createContext(config, scopeRequest(amb));
  const { redirectTo, callbackUrl, redirect, ...fields } = options;
  const wanted = await resolveCallbackUrl(ctx, redirectTo ?? callbackUrl);
  const doRedirect = redirect !== false;

  const provider = providerId ? config.providers.find((p) => p.id === providerId) : undefined;
  if (!providerId) {
    const target = new URL(pagePath(ctx, config.pages.signIn, "/signin"));
    target.searchParams.set("callbackUrl", wanted);
    return finish(ctx, amb, new Response(null, { status: 302, headers: { location: target.toString() } }), doRedirect);
  }
  if (!provider) throw new AuthError("Configuration", `unknown provider "${providerId}"`, 500);

  const strings: Record<string, string> = {};
  for (const [k, v] of Object.entries(fields)) if (typeof v === "string") strings[k] = v;
  strings.callbackUrl = wanted;

  let response: Response;
  if (provider.type === "oauth" || provider.type === "oidc") {
    response = await startOAuth(ctx, provider as OAuthConfig, { callbackUrl: wanted, params: authorizationParams });
  } else if (provider.type === "email") {
    response = await startEmail(ctx, provider as EmailConfig, strings);
  } else if (provider.type === "credentials") {
    response = await handleCredentials(ctx, provider as CredentialsConfig, strings);
  } else {
    throw new AuthError("Configuration", `${provider.type} sign-in needs the browser: use the client signIn()`, 500);
  }
  return finish(ctx, amb, response, doRedirect);
}

export async function serverSignOut(config: ResolvedConfig, options: SignOutOptions = {}): Promise<string | undefined> {
  const amb = requireScope("signOut", true);
  const ctx = createContext(config, scopeRequest(amb));
  const target = options.redirectTo ?? options.callbackUrl;
  const response = await handleSignOut(ctx, target ? { callbackUrl: target } : {});
  return finish(ctx, amb, response, options.redirect !== false);
}

// auth() with no argument: the session of the request this scope belongs to.
// Server actions and route handlers also pick up a refreshed session cookie.
export async function scopedSession(config: ResolvedConfig): Promise<Session | null> {
  const amb = requireScope("auth", false);
  const ctx = createContext(config, scopeRequest(amb));
  const jar = amb.writable ? new ResponseCookies() : undefined;
  const state = await readSession(ctx, jar);
  if (jar) {
    try {
      applyCookies(amb, jar.list());
    } catch (err) {
      config.logger.debug("SessionCookieSkipped", err);
    }
  }
  return state.session;
}
