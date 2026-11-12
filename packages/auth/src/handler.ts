import { AuthError, createContext, type RequestContext, type ResolvedConfig } from "./config.js";
import { ResponseCookies } from "./cookies.js";
import { createCsrfToken, verifyCsrfCookie } from "./crypto.js";
import { finishEmail, startEmail } from "./email-flow.js";
import { handlePasskey } from "./passkeys.js";
import { handleTwoFactor } from "./twofactor.js";
import { finishOAuth, startOAuth } from "./oauth-flow.js";
import { errorPage, signInPage, signOutPage, providerSummary, verifyRequestPage, twoFactorPage } from "./pages.js";
import { errorRedirect, isJsonClient, jsonResponse, redirectResponse, sanitizeRedirect } from "./redirect.js";
import { readSession, requestCookies } from "./session.js";
import { assertCsrf, handleCredentials, handleSignOut, parseBody, resolveCallbackUrl } from "./signin.js";
import type { CredentialsConfig, EmailConfig, OAuthConfig } from "./types.js";

export function csrfFor(ctx: RequestContext, jar: ResponseCookies): string {
  const existing = verifyCsrfCookie(requestCookies(ctx.request)[ctx.cookies.csrfToken.name], ctx.secrets);
  if (existing?.valid) return existing.token;
  const fresh = createCsrfToken(ctx.secret);
  jar.set(ctx.cookies.csrfToken.name, fresh.cookieValue, ctx.cookies.csrfToken.options);
  return fresh.token;
}

function actionSegments(ctx: RequestContext): string[] | null {
  const base = ctx.config.basePath.replace(/\/+$/, "");
  const path = ctx.url.pathname;
  const idx = path.indexOf(base + "/");
  const exact = path === base || path.endsWith(base);
  if (idx < 0 && !exact) return null;
  const rest = idx >= 0 ? path.slice(idx + base.length) : "";
  return rest.split("/").filter(Boolean).map((s) => decodeURIComponent(s));
}

function failure(ctx: RequestContext | null, request: Request, err: unknown): Response {
  if (err instanceof AuthError) {
    if (!ctx || isJsonClient(request) || (request.method === "GET" && (err.status >= 500 || err.status === 401))) {
      return jsonResponse({ error: err.code, message: err.message }, { status: err.status });
    }
    return errorRedirect(ctx, err.code);
  }
  ctx?.config.logger.error("UnhandledError", err);
  if (!ctx || isJsonClient(request)) return jsonResponse({ error: "Configuration" }, { status: 500 });
  return errorRedirect(ctx, "Configuration");
}

export async function handleAuth(request: Request, config: ResolvedConfig): Promise<Response> {
  let ctx: RequestContext | null = null;
  try {
    if (!config.enabled) return jsonResponse({ error: "Disabled" }, { status: 404 });
    ctx = createContext(config, request);
    const segments = actionSegments(ctx);
    if (!segments) return jsonResponse({ error: "NotFound" }, { status: 404 });
    const response = await route(ctx, segments);
    return response ?? jsonResponse({ error: "NotFound" }, { status: 404 });
  } catch (err) {
    return failure(ctx, request, err);
  }
}

async function route(ctx: RequestContext, segments: string[]): Promise<Response | null> {
  const { request, config } = ctx;
  const method = request.method.toUpperCase();
  const [action, id] = segments;

  switch (action) {
    case "providers":
      if (method !== "GET") return null;
      return jsonResponse(providerSummary(ctx));
    case "csrf": {
      if (method !== "GET") return null;
      const jar = new ResponseCookies();
      return jsonResponse({ csrfToken: csrfFor(ctx, jar) }, { jar });
    }
    case "session": {
      const jar = new ResponseCookies();
      if (method === "GET") {
        const state = await readSession(ctx, jar);
        return jsonResponse(state.session, { jar });
      }
      if (method === "POST") {
        const body = (await request.json().catch(() => ({}))) as { csrfToken?: string; data?: unknown };
        assertCsrf(ctx, body.csrfToken);
        const state = await readSession(ctx, jar, { update: body.data });
        return jsonResponse(state.session, { jar });
      }
      return null;
    }
    case "signin": {
      if (method === "GET") {
        const jar = new ResponseCookies();
        const callbackUrl = await resolveCallbackUrl(ctx, ctx.url.searchParams.get("callbackUrl"));
        if (config.pages.signIn) {
          const target = new URL(config.pages.signIn.startsWith("http") ? config.pages.signIn : ctx.baseUrl + config.pages.signIn);
          target.searchParams.set("callbackUrl", callbackUrl);
          const err = ctx.url.searchParams.get("error");
          if (err) target.searchParams.set("error", err);
          return redirectResponse(ctx, target.toString(), jar);
        }
        const token = csrfFor(ctx, jar);
        const page = signInPage({ ctx, csrfToken: token, callbackUrl, error: ctx.url.searchParams.get("error") });
        jar.apply(page.headers);
        return page;
      }
      if (method === "POST" && id) {
        const provider = config.providers.find((p) => p.id === id);
        if (!provider) return errorRedirect(ctx, "Configuration");
        if (provider.type === "credentials") return null;
        if (provider.type === "email") return startEmail(ctx, provider as EmailConfig);
        if (provider.type === "oauth" || provider.type === "oidc") return startOAuth(ctx, provider as OAuthConfig);
        return errorRedirect(ctx, "Configuration");
      }
      return null;
    }
    case "callback": {
      if (!id) return null;
      const provider = config.providers.find((p) => p.id === id);
      if (!provider) return errorRedirect(ctx, "Configuration");
      if (provider.type === "credentials") {
        if (method !== "POST") return null;
        return handleCredentials(ctx, provider as CredentialsConfig);
      }
      if (provider.type === "email") return finishEmail(ctx, provider as EmailConfig);
      if (provider.type === "oauth" || provider.type === "oidc") return finishOAuth(ctx, provider as OAuthConfig);
      return errorRedirect(ctx, "Configuration");
    }
    case "passkey":
      return handlePasskey(ctx, segments);
    case "signout": {
      if (method === "GET") {
        const jar = new ResponseCookies();
        const callbackUrl = await sanitizeRedirect(ctx, ctx.url.searchParams.get("callbackUrl"));
        const page = signOutPage({ ctx, csrfToken: csrfFor(ctx, jar), callbackUrl });
        jar.apply(page.headers);
        return page;
      }
      if (method === "POST") return handleSignOut(ctx);
      return null;
    }
    case "error":
      if (method !== "GET") return null;
      return errorPage(ctx, ctx.url.searchParams.get("error"));
    case "verify-request":
      if (method !== "GET") return null;
      return verifyRequestPage();
    case "2fa":
      if (id) return handleTwoFactor(ctx, segments);
      if (method === "GET") {
        const jar = new ResponseCookies();
        const callbackUrl = await sanitizeRedirect(ctx, ctx.url.searchParams.get("callbackUrl"));
        const page = twoFactorPage(ctx, csrfFor(ctx, jar), callbackUrl, ctx.url.searchParams.get("error"));
        jar.apply(page.headers);
        return page;
      }
      return null;
    default:
      return null;
  }
}

export { parseBody };
