import { AuthError, type RequestContext } from "./config.js";
import { ResponseCookies, type CookieOptions, type CookieSpec } from "./cookies.js";
import { b64urlDecodeString, b64urlEncode, timingSafeEqual, randomToken } from "./bytes.js";
import { sha256Bytes } from "./crypto.js";
import { errorRedirect, redirectResponse } from "./redirect.js";
import { requestCookies } from "./session.js";
import { assertCsrf, completeSignIn, limit, parseBody, resolveCallbackUrl } from "./signin.js";
import { resolveOAuthUser } from "./users.js";
import type { Account, OAuthConfig, OAuthTokens, User } from "./types.js";

const TIMEOUT_MS = 10_000;
const discoveryCache = new Map<string, { expires: number; doc: Record<string, any> }>();

export function clearDiscoveryCache(): void {
  discoveryCache.clear();
}

export async function timedFetch(input: string, init: RequestInit = {}): Promise<Response> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), TIMEOUT_MS);
  try {
    return await fetch(input, { ...init, signal: controller.signal });
  } finally {
    clearTimeout(timer);
  }
}

async function discover(provider: OAuthConfig): Promise<OAuthConfig> {
  if (provider.authorization.url && provider.token.url) return provider;
  const url = provider.wellKnown ?? (provider.issuer ? `${provider.issuer.replace(/\/+$/, "")}/.well-known/openid-configuration` : undefined);
  if (!url) throw new AuthError("Configuration", `provider ${provider.id} needs authorization and token endpoints, or an issuer`, 500);
  let entry = discoveryCache.get(url);
  if (!entry || entry.expires < Date.now()) {
    const res = await timedFetch(url, { headers: { accept: "application/json" } });
    if (!res.ok) throw new AuthError("OAuthSignin", `discovery failed for ${provider.id}: ${res.status}`, 502);
    entry = { expires: Date.now() + 3_600_000, doc: (await res.json()) as Record<string, any> };
    discoveryCache.set(url, entry);
  }
  const doc = entry.doc;
  return {
    ...provider,
    issuer: provider.issuer ?? doc.issuer,
    authorization: { ...provider.authorization, url: provider.authorization.url ?? doc.authorization_endpoint },
    token: { ...provider.token, url: provider.token.url ?? doc.token_endpoint },
    userinfo: provider.userinfo?.url ? provider.userinfo : doc.userinfo_endpoint ? { ...provider.userinfo, url: doc.userinfo_endpoint } : provider.userinfo,
  };
}

function callbackUri(ctx: RequestContext, provider: OAuthConfig): string {
  return `${ctx.baseUrl}${ctx.config.basePath}/callback/${provider.id}`;
}

function usesFormPost(provider: OAuthConfig): boolean {
  return provider.authorization.params?.response_mode === "form_post";
}

function checkCookieOptions(provider: OAuthConfig, base: CookieSpec): CookieOptions {
  if (usesFormPost(provider)) return { ...base.options, sameSite: "none", secure: true };
  return base.options;
}

export async function startOAuth(ctx: RequestContext, provider: OAuthConfig): Promise<Response> {
  const body = await parseBody(ctx.request);
  assertCsrf(ctx, body.csrfToken);
  limit(ctx, "oauth-start");
  if (!provider.clientId) throw new AuthError("Configuration", `missing client id for ${provider.id} (set AUTH_${provider.id.toUpperCase().replace(/[^A-Z0-9]+/g, "_")}_ID)`, 500);
  const resolved = await discover(provider);
  const { config, cookies } = ctx;
  const jar = new ResponseCookies();
  const url = new URL(resolved.authorization.url!);
  const checks = resolved.checks ?? ["pkce", "state"];
  const callbackUrl = await resolveCallbackUrl(ctx, body.callbackUrl);

  for (const [k, v] of Object.entries(resolved.authorization.params ?? {})) url.searchParams.set(k, v);
  url.searchParams.set("response_type", "code");
  url.searchParams.set("client_id", provider.clientId);
  url.searchParams.set("redirect_uri", callbackUri(ctx, resolved));
  if (!url.searchParams.has("scope") && resolved.type === "oidc") url.searchParams.set("scope", "openid profile email");

  const seal = (claims: Record<string, unknown>, name: string) => config.encode({ token: claims, secret: ctx.secrets, salt: name, maxAge: 900 });

  if (checks.includes("state") || usesFormPost(resolved)) {
    const state = randomToken(24);
    url.searchParams.set("state", state);
    jar.set(cookies.state.name, await seal({ state, callbackUrl }, cookies.state.name), checkCookieOptions(provider, cookies.state));
  } else {
    jar.set(cookies.state.name, await seal({ callbackUrl }, cookies.state.name), checkCookieOptions(provider, cookies.state));
  }
  if (checks.includes("pkce")) {
    const verifier = randomToken(32);
    url.searchParams.set("code_challenge", b64urlEncode(sha256Bytes(verifier)));
    url.searchParams.set("code_challenge_method", "S256");
    jar.set(cookies.pkceCodeVerifier.name, await seal({ verifier }, cookies.pkceCodeVerifier.name), checkCookieOptions(provider, cookies.pkceCodeVerifier));
  }
  if (checks.includes("nonce") && resolved.type === "oidc") {
    const nonce = randomToken(24);
    url.searchParams.set("nonce", nonce);
    jar.set(cookies.nonce.name, await seal({ nonce }, cookies.nonce.name), checkCookieOptions(provider, cookies.nonce));
  }
  config.logger.debug("OAuthStart", { provider: provider.id, url: url.origin + url.pathname });
  return redirectResponse(ctx, url.toString(), jar);
}

function parseTokenResponse(text: string, contentType: string): OAuthTokens {
  if (contentType.includes("application/json") || text.trim().startsWith("{")) return JSON.parse(text) as OAuthTokens;
  const out: Record<string, string> = {};
  for (const [k, v] of new URLSearchParams(text)) out[k] = v;
  return out as OAuthTokens;
}

async function exchangeCode(ctx: RequestContext, provider: OAuthConfig, code: string, verifier: string | undefined): Promise<OAuthTokens> {
  const params = new URLSearchParams({ grant_type: "authorization_code", code, redirect_uri: callbackUri(ctx, provider) });
  for (const [k, v] of Object.entries(provider.token.params ?? {})) params.set(k, v);
  if (verifier) params.set("code_verifier", verifier);
  const headers: Record<string, string> = { "content-type": "application/x-www-form-urlencoded", accept: "application/json" };
  const method = provider.client?.token_endpoint_auth_method ?? "client_secret_basic";
  if (method === "client_secret_post") {
    params.set("client_id", provider.clientId!);
    if (provider.clientSecret) params.set("client_secret", provider.clientSecret);
  } else if (method === "client_secret_basic") {
    const user = encodeURIComponent(provider.clientId!).replace(/%20/g, "+");
    const pass = encodeURIComponent(provider.clientSecret ?? "").replace(/%20/g, "+");
    headers.authorization = `Basic ${btoa(`${user}:${pass}`)}`;
  } else {
    params.set("client_id", provider.clientId!);
  }
  const res = await timedFetch(provider.token.url!, { method: "POST", headers, body: params.toString() });
  const text = await res.text();
  let tokens: OAuthTokens;
  try {
    tokens = parseTokenResponse(text, res.headers.get("content-type") ?? "");
  } catch {
    throw new AuthError("OAuthCallback", `token endpoint of ${provider.id} returned an unreadable response`, 502);
  }
  if (!res.ok || tokens.error || !tokens.access_token && !tokens.id_token) {
    ctx.config.logger.error("OAuthTokenError", { provider: provider.id, status: res.status, error: tokens.error, description: tokens.error_description });
    throw new AuthError("OAuthCallback", `token exchange with ${provider.id} failed`, 502);
  }
  if (typeof tokens.expires_in === "string") tokens.expires_in = Number(tokens.expires_in);
  if (typeof tokens.expires_in === "number" && !tokens.expires_at) tokens.expires_at = Math.floor(Date.now() / 1000) + tokens.expires_in;
  return tokens;
}

export function decodeIdToken(idToken: string): Record<string, any> {
  const parts = idToken.split(".");
  if (parts.length !== 3) throw new AuthError("OAuthCallback", "id_token is malformed", 502);
  return JSON.parse(b64urlDecodeString(parts[1]!)) as Record<string, any>;
}

function validateIdToken(provider: OAuthConfig, claims: Record<string, any>, nonce: string | undefined): void {
  const aud = Array.isArray(claims.aud) ? claims.aud : [claims.aud];
  if (!aud.includes(provider.clientId)) throw new AuthError("OAuthCallback", "id_token audience mismatch", 502);
  if (provider.issuer && claims.iss && !provider.skipIssuerCheck) {
    const normalize = (v: string) => v.replace(/\/+$/, "");
    if (normalize(String(claims.iss)) !== normalize(provider.issuer)) throw new AuthError("OAuthCallback", "id_token issuer mismatch", 502);
  }
  if (typeof claims.exp === "number" && claims.exp * 1000 < Date.now() - 60_000) throw new AuthError("OAuthCallback", "id_token expired", 502);
  if (nonce !== undefined && claims.nonce !== nonce) throw new AuthError("OAuthCallback", "id_token nonce mismatch", 502);
}

async function fetchProfile(provider: OAuthConfig, tokens: OAuthTokens, nonce: string | undefined, explicitUserinfo: boolean): Promise<Record<string, any>> {
  if (provider.userinfo?.request) {
    try {
      return await provider.userinfo.request({ tokens, provider, fetch: (i, init) => timedFetch(String(i), init) });
    } catch (err) {
      throw new AuthError("OAuthCallback", `profile request to ${provider.id} failed: ${(err as Error).message}`, 502);
    }
  }
  let claims: Record<string, any> | undefined;
  if (tokens.id_token) {
    claims = decodeIdToken(tokens.id_token);
    validateIdToken(provider, claims, nonce);
  }
  if (provider.type === "oidc" && claims && !explicitUserinfo) return claims;
  if (provider.userinfo?.url && tokens.access_token) {
    const target = new URL(provider.userinfo.url);
    for (const [k, v] of Object.entries(provider.userinfo.params ?? {})) target.searchParams.set(k, v);
    const res = await timedFetch(target.toString(), { headers: { authorization: `Bearer ${tokens.access_token}`, accept: "application/json" } });
    if (!res.ok) throw new AuthError("OAuthCallback", `userinfo request to ${provider.id} failed: ${res.status}`, 502);
    const info = (await res.json()) as Record<string, any>;
    return claims ? { ...claims, ...info } : info;
  }
  if (claims) return claims;
  throw new AuthError("OAuthCallback", `${provider.id} returned no id_token and has no userinfo endpoint`, 502);
}

async function openCookie(ctx: RequestContext, name: string, jar: Record<string, string>): Promise<Record<string, any> | null> {
  const raw = jar[name];
  if (!raw) return null;
  return ctx.config.decode({ token: raw, secret: ctx.secrets, salt: name });
}

export async function finishOAuth(ctx: RequestContext, provider: OAuthConfig): Promise<Response> {
  const { config, cookies, request } = ctx;
  const params = new URLSearchParams(ctx.url.search);
  if (request.method.toUpperCase() === "POST") {
    const body = await parseBody(request);
    for (const [k, v] of Object.entries(body)) params.set(k, v);
  }
  const clear = new ResponseCookies();
  for (const spec of [cookies.state, cookies.pkceCodeVerifier, cookies.nonce]) clear.delete(spec.name, spec.options);

  if (params.get("error")) {
    const code = params.get("error") === "access_denied" ? "AccessDenied" : "OAuthCallback";
    config.logger.warn("OAuthProviderError", { provider: provider.id, error: params.get("error"), description: params.get("error_description") });
    return errorRedirect(ctx, code, clear);
  }
  const code = params.get("code");
  if (!code) return errorRedirect(ctx, "OAuthCallback", clear);

  const sent = requestCookies(request);
  const statePayload = await openCookie(ctx, cookies.state.name, sent);
  if (!statePayload) return errorRedirect(ctx, "OAuthCallback", clear);
  const checks = provider.checks ?? ["pkce", "state"];
  if (checks.includes("state") || typeof statePayload.state === "string") {
    const returned = params.get("state");
    if (!returned || typeof statePayload.state !== "string" || !timingSafeEqual(returned, statePayload.state)) {
      config.logger.warn("OAuthStateMismatch", { provider: provider.id });
      return errorRedirect(ctx, "OAuthCallback", clear);
    }
  }
  let verifier: string | undefined;
  if (checks.includes("pkce")) {
    const pk = await openCookie(ctx, cookies.pkceCodeVerifier.name, sent);
    verifier = pk?.verifier as string | undefined;
    if (!verifier) return errorRedirect(ctx, "OAuthCallback", clear);
  }
  let nonce: string | undefined;
  if (checks.includes("nonce") && provider.type === "oidc") {
    const n = await openCookie(ctx, cookies.nonce.name, sent);
    nonce = n?.nonce as string | undefined;
    if (!nonce) return errorRedirect(ctx, "OAuthCallback", clear);
  }

  limit(ctx, "oauth-callback");
  let resolved: OAuthConfig;
  let tokens: OAuthTokens;
  let profile: Record<string, any>;
  let profileUser: User;
  try {
    resolved = await discover(provider);
    tokens = await exchangeCode(ctx, resolved, code, verifier);
    profile = await fetchProfile(resolved, tokens, nonce, Boolean(provider.userinfo?.url));
    profileUser = await (resolved.profile ?? ((p: Record<string, any>) => ({ id: p.sub ?? p.id })))(profile, tokens);
  } catch (err) {
    if (err instanceof AuthError) {
      config.logger.error("OAuthCallbackError", { provider: provider.id, message: err.message });
      return errorRedirect(ctx, err.code === "OAuthSignin" ? "OAuthSignin" : "OAuthCallback", clear);
    }
    throw err;
  }
  if (profileUser.id === undefined || profileUser.id === null || profileUser.id === "") {
    config.logger.error("OAuthProfileError", { provider: provider.id, message: "profile() returned no id" });
    return errorRedirect(ctx, "OAuthCallback", clear);
  }
  const account: Account = {
    type: provider.type,
    provider: provider.id,
    providerAccountId: String(profileUser.id),
    access_token: tokens.access_token ?? null,
    refresh_token: tokens.refresh_token ?? null,
    expires_at: tokens.expires_at ?? null,
    token_type: tokens.token_type ? String(tokens.token_type).toLowerCase() : null,
    scope: tokens.scope ?? null,
    id_token: tokens.id_token ?? null,
    session_state: tokens.session_state ?? null,
  };
  let result;
  try {
    result = await resolveOAuthUser(ctx, profileUser, account, profile, Boolean(provider.allowDangerousEmailAccountLinking));
  } catch (err) {
    if (err instanceof AuthError) return errorRedirect(ctx, err.code, clear);
    throw err;
  }
  const callbackUrl = await resolveCallbackUrl(ctx, statePayload.callbackUrl as string | undefined);
  return completeSignIn(ctx, {
    user: result.user,
    account: result.account,
    profile,
    isNewUser: result.isNewUser,
    method: "oauth",
    callbackUrl,
    jar: clear,
  });
}
