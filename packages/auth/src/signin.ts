import { AuthError, type RequestContext } from "./config.js";
import { ResponseCookies } from "./cookies.js";
import { verifyCsrfCookie } from "./crypto.js";
import { clientIp, consume } from "./ratelimit.js";
import { errorRedirect, jsonResponse, pagePath, redirectResponse, sanitizeRedirect } from "./redirect.js";
import { destroySession, issueSession, requestCookies } from "./session.js";
import type { Account, AdapterUser, CredentialsConfig, User } from "./types.js";

export type AuthMethod = "pwd" | "oauth" | "email" | "passkey";

export interface PendingTwoFactor {
  uid: string;
  method: AuthMethod;
  provider?: string;
  providerAccountId?: string;
  callbackUrl: string;
  isNewUser?: boolean;
}

export async function parseBody(request: Request): Promise<Record<string, string>> {
  const type = request.headers.get("content-type") ?? "";
  if (type.includes("application/json")) {
    const data = (await request.json().catch(() => ({}))) as Record<string, unknown>;
    const out: Record<string, string> = {};
    for (const [k, v] of Object.entries(data ?? {})) {
      if (typeof v === "string") out[k] = v;
      else if (typeof v === "number" || typeof v === "boolean") out[k] = String(v);
    }
    return out;
  }
  const text = await request.text().catch(() => "");
  const out: Record<string, string> = {};
  for (const [k, v] of new URLSearchParams(text)) out[k] = v;
  return out;
}

export function assertSameOrigin(ctx: RequestContext): void {
  const origin = ctx.request.headers.get("origin");
  if (!origin || origin === "null") {
    if (origin === "null") throw new AuthError("InvalidOrigin", "cross-origin request blocked", 403);
    return;
  }
  if (origin !== ctx.baseUrl && !ctx.config.redirects.allowed.includes(origin)) {
    throw new AuthError("InvalidOrigin", "cross-origin request blocked", 403);
  }
}

export function assertCsrf(ctx: RequestContext, supplied: string | undefined): void {
  assertSameOrigin(ctx);
  const cookie = requestCookies(ctx.request)[ctx.cookies.csrfToken.name];
  const verified = verifyCsrfCookie(cookie, ctx.secrets);
  const header = ctx.request.headers.get("x-csrf-token") ?? undefined;
  const token = supplied ?? header;
  if (!verified?.valid || !token || token !== verified.token) {
    throw new AuthError("MissingCSRF", "invalid or missing CSRF token", 403);
  }
}

export function limit(ctx: RequestContext, action: string, identifier?: string): void {
  const { rateLimit } = ctx.config;
  if (!rateLimit.enabled) return;
  const ip = clientIp(ctx.request);
  const checks = [`${action}:ip:${ip}`];
  if (identifier) checks.push(`${action}:id:${identifier.toLowerCase()}`);
  for (const key of checks) {
    const result = consume(key, rateLimit.max, rateLimit.window);
    if (!result.allowed) throw new AuthError("RateLimited", `too many attempts, retry in ${result.retryAfter}s`, 429);
  }
}

export interface CompleteSignInOptions {
  user: User | AdapterUser;
  account?: Account | null;
  profile?: Record<string, any>;
  isNewUser?: boolean;
  method: AuthMethod;
  callbackUrl?: string | null;
  credentials?: Record<string, string>;
  skipTwoFactor?: boolean;
  jar?: ResponseCookies;
  verificationRequest?: boolean;
}

export async function resolveCallbackUrl(ctx: RequestContext, requested?: string | null): Promise<string> {
  const fallback = ctx.config.redirects.signedIn ? ctx.baseUrl + ctx.config.redirects.signedIn : ctx.baseUrl + "/";
  return sanitizeRedirect(ctx, requested, fallback);
}

export async function completeSignIn(ctx: RequestContext, options: CompleteSignInOptions): Promise<Response> {
  const { config, cookies } = ctx;
  const jar = options.jar ?? new ResponseCookies();
  const user = options.user as AdapterUser;
  if (!user?.id) {
    config.logger.error("MissingUserId", "authorize or the adapter must return a user with an id");
    return errorRedirect(ctx, "Configuration", jar);
  }
  const account = options.account ?? null;
  if (config.callbacks.signIn) {
    let decision: boolean | string;
    try {
      decision = await config.callbacks.signIn({ user, account, profile: options.profile, credentials: options.credentials });
    } catch (err) {
      config.logger.error("SignInCallbackError", err);
      return errorRedirect(ctx, "AccessDenied", jar);
    }
    if (decision === false) return errorRedirect(ctx, "AccessDenied", jar);
    if (typeof decision === "string") return redirectResponse(ctx, await sanitizeRedirect(ctx, decision), jar);
  }

  const callbackUrl = await resolveCallbackUrl(ctx, options.callbackUrl);
  let enrolled = false;
  if (config.twoFactor.enabled && config.adapter?.getTwoFactor) {
    const record = await config.adapter.getTwoFactor(user.id);
    enrolled = Boolean(record?.confirmed);
    if (enrolled && !options.skipTwoFactor) {
      const pending: PendingTwoFactor = {
        uid: user.id,
        method: options.method,
        provider: account?.provider,
        providerAccountId: account?.providerAccountId,
        callbackUrl,
        isNewUser: options.isNewUser,
      };
      const token = await config.encode({ token: { ...pending }, secret: ctx.secrets, salt: cookies.pendingTwoFactor.name, maxAge: 600 });
      jar.set(cookies.pendingTwoFactor.name, token, cookies.pendingTwoFactor.options);
      const target = new URL(pagePath(ctx, config.pages.twoFactor, "/2fa"));
      target.searchParams.set("callbackUrl", callbackUrl);
      return redirectResponse(ctx, target.toString(), jar, { twoFactorRequired: true });
    }
  }

  await finalizeSignIn(ctx, jar, { ...options, callbackUrl, enrolled, twoFactorVerified: false });
  const destination = options.isNewUser && config.pages.newUser ? ctx.baseUrl + config.pages.newUser : callbackUrl;
  return redirectResponse(ctx, destination, jar);
}

export interface FinalizeOptions extends CompleteSignInOptions {
  enrolled: boolean;
  twoFactorVerified: boolean;
}

export async function finalizeSignIn(ctx: RequestContext, jar: ResponseCookies, options: FinalizeOptions): Promise<void> {
  const { config } = ctx;
  const user = options.user as AdapterUser;
  const amr: string[] = [options.method];
  if (options.twoFactorVerified) amr.push("otp");
  await issueSession(ctx, jar, {
    user: { ...user, twoFactorEnabled: options.enrolled },
    account: options.account,
    profile: options.profile,
    isNewUser: options.isNewUser,
    claims: { amr, ...(options.twoFactorVerified ? { tfv: true } : {}) },
  });
  jar.delete(ctx.cookies.pendingTwoFactor.name, ctx.cookies.pendingTwoFactor.options);
  if (config.events.signIn) {
    await config.events.signIn({ user, account: options.account ?? null, profile: options.profile, isNewUser: options.isNewUser });
  }
}

export async function handleCredentials(ctx: RequestContext, provider: CredentialsConfig): Promise<Response> {
  const body = await parseBody(ctx.request);
  assertCsrf(ctx, body.csrfToken);
  const identifier = body.email ?? body.username ?? body.identifier;
  limit(ctx, "credentials", identifier);
  const submitted: Record<string, string> = {};
  for (const key of Object.keys(provider.credentials)) if (body[key] !== undefined) submitted[key] = body[key]!;
  for (const [key, value] of Object.entries(body)) if (!(key in submitted) && key !== "csrfToken" && key !== "callbackUrl") submitted[key] = value;
  let user: User | null | undefined | false;
  try {
    user = await provider.authorize(submitted, ctx.request);
  } catch (err) {
    ctx.config.logger.error("AuthorizeError", err);
    return errorRedirect(ctx, "CredentialsSignin", undefined, { provider: provider.id });
  }
  if (!user) return errorRedirect(ctx, "CredentialsSignin", undefined, { provider: provider.id });
  const account: Account = { type: "credentials", provider: provider.id, providerAccountId: String(user.id ?? "") };
  return completeSignIn(ctx, { user, account, method: "pwd", callbackUrl: body.callbackUrl, credentials: submitted });
}

export async function handleSignOut(ctx: RequestContext): Promise<Response> {
  const body = await parseBody(ctx.request);
  assertCsrf(ctx, body.csrfToken);
  const jar = new ResponseCookies();
  const state = await destroySession(ctx, jar);
  if (ctx.config.events.signOut) await ctx.config.events.signOut({ session: state.session, token: state.token });
  const fallback = ctx.config.redirects.signedOut ? ctx.baseUrl + ctx.config.redirects.signedOut : ctx.baseUrl + "/";
  const target = await sanitizeRedirect(ctx, body.callbackUrl, fallback);
  return redirectResponse(ctx, target, jar);
}

export function sessionJson(ctx: RequestContext, session: unknown, jar?: ResponseCookies): Response {
  return jsonResponse(session ?? null, { jar });
}
