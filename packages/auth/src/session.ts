import { AuthError, type RequestContext } from "./config.js";
import { ResponseCookies, parseCookies, readChunked } from "./cookies.js";
import type { TokenClaims } from "./crypto.js";
import type { Account, AdapterUser, Session, User } from "./types.js";

export interface SessionState {
  session: Session | null;
  token?: TokenClaims;
  user?: AdapterUser;
  sessionToken?: string;
}

export function requestCookies(request: Request): Record<string, string> {
  return parseCookies(request.headers.get("cookie"));
}

function buildSession(ctx: RequestContext, expires: Date, base: { id: string; name?: unknown; email?: unknown; image?: unknown; role?: unknown; twoFactorEnabled?: unknown; extra?: Record<string, unknown> }): Session {
  const user: Session["user"] = {
    id: base.id,
    name: (base.name as string | null | undefined) ?? null,
    email: (base.email as string | null | undefined) ?? null,
    image: (base.image as string | null | undefined) ?? null,
  };
  if (base.role !== undefined && base.role !== null) user.role = base.role as string;
  if (base.twoFactorEnabled !== undefined) user.twoFactorEnabled = Boolean(base.twoFactorEnabled);
  if (ctx.config.twoFactor.enabled && ctx.config.twoFactor.required && !user.twoFactorEnabled) user.twoFactorSetupRequired = true;
  return { user, expires: expires.toISOString(), ...(base.extra ?? {}) };
}

export async function readSession(ctx: RequestContext, jar?: ResponseCookies, trigger?: { update: unknown }): Promise<SessionState> {
  const { config, cookies } = ctx;
  const jarIn = requestCookies(ctx.request);
  const raw = readChunked(jarIn, cookies.sessionToken.name);
  if (!raw) return { session: null };

  if (config.strategy === "jwt") {
    let token: TokenClaims | null;
    try {
      token = await config.decode({ token: raw, secret: ctx.secrets, salt: cookies.sessionToken.name });
    } catch (err) {
      config.logger.warn("JWTSessionError", err);
      token = null;
    }
    if (!token || !token.sub) {
      jar?.deleteChunked(cookies.sessionToken.name, cookies.sessionToken.options, jarIn);
      return { session: null };
    }
    let claims: TokenClaims = token;
    if (trigger && config.callbacks.jwt) {
      const next = await config.callbacks.jwt({ token: claims, trigger: "update", session: trigger.update });
      if (next) claims = next;
    }
    const exp = typeof claims.exp === "number" ? claims.exp * 1000 : Date.now() + config.maxAge * 1000;
    let session = buildSession(ctx, new Date(exp), {
      id: String(claims.sub),
      name: claims.name,
      email: claims.email,
      image: claims.picture,
      role: claims.role,
      twoFactorEnabled: claims.tfe,
    });
    if (config.callbacks.session) {
      session = await config.callbacks.session({ session, token: claims, trigger: trigger ? "update" : undefined, newSession: trigger?.update });
    }
    const iat = typeof claims.iat === "number" ? claims.iat * 1000 : 0;
    const stale = Date.now() - iat > config.updateAge * 1000;
    if (jar && (stale || trigger)) {
      const refreshed = await config.encode({
        token: { ...claims },
        secret: ctx.secrets,
        salt: cookies.sessionToken.name,
        maxAge: config.jwtMaxAge,
      });
      jar.setChunked(
        cookies.sessionToken.name,
        refreshed,
        { ...cookies.sessionToken.options, expires: new Date(Date.now() + config.jwtMaxAge * 1000) },
        jarIn,
      );
      session.expires = new Date(Date.now() + config.jwtMaxAge * 1000).toISOString();
    }
    if (config.events.session) await config.events.session({ session, token: claims });
    return { session, token: claims };
  }

  const adapter = config.adapter;
  if (!adapter?.getSessionAndUser) {
    config.logger.error("MissingAdapter", "database sessions need an adapter with getSessionAndUser");
    return { session: null };
  }
  const found = await adapter.getSessionAndUser(raw);
  if (!found) {
    jar?.delete(cookies.sessionToken.name, cookies.sessionToken.options);
    return { session: null };
  }
  const { session: stored, user } = found;
  if (stored.expires.valueOf() <= Date.now()) {
    await adapter.deleteSession?.(raw);
    jar?.delete(cookies.sessionToken.name, cookies.sessionToken.options);
    return { session: null };
  }
  let expires = stored.expires;
  const sinceRefresh = Date.now() - (stored.expires.valueOf() - config.maxAge * 1000);
  if (jar && sinceRefresh > config.updateAge * 1000) {
    expires = new Date(Date.now() + config.maxAge * 1000);
    await adapter.updateSession?.({ sessionToken: raw, expires });
    jar.set(cookies.sessionToken.name, raw, { ...cookies.sessionToken.options, expires });
  }
  let session = buildSession(ctx, expires, {
    id: user.id,
    name: user.name,
    email: user.email,
    image: user.image,
    role: user.role,
    twoFactorEnabled: user.twoFactorEnabled,
  });
  if (config.callbacks.session) {
    session = await config.callbacks.session({ session, user, trigger: trigger ? "update" : undefined, newSession: trigger?.update });
  }
  if (config.events.session) await config.events.session({ session });
  return { session, user, sessionToken: raw };
}

export interface IssueOptions {
  user: User | AdapterUser;
  account?: Account | null;
  profile?: Record<string, any>;
  isNewUser?: boolean;
  trigger?: "signIn" | "signUp";
  claims?: Record<string, unknown>;
}

export async function issueSession(ctx: RequestContext, jar: ResponseCookies, options: IssueOptions): Promise<void> {
  const { config, cookies } = ctx;
  const existing = requestCookies(ctx.request);
  const user = options.user as AdapterUser;
  if (config.strategy === "jwt") {
    let token: TokenClaims = {
      sub: user.id,
      name: user.name ?? null,
      email: user.email ?? null,
      picture: user.image ?? null,
      ...(user.role !== undefined && user.role !== null ? { role: user.role } : {}),
      ...(user.twoFactorEnabled !== undefined ? { tfe: user.twoFactorEnabled } : {}),
      ...(options.claims ?? {}),
    };
    if (config.callbacks.jwt) {
      const next = await config.callbacks.jwt({
        token,
        user,
        account: options.account ?? null,
        profile: options.profile,
        isNewUser: options.isNewUser,
        trigger: options.isNewUser ? "signUp" : "signIn",
      });
      if (!next) throw new Error("jwt callback returned no token");
      token = next;
    }
    const encoded = await config.encode({ token, secret: ctx.secrets, salt: cookies.sessionToken.name, maxAge: config.jwtMaxAge });
    jar.setChunked(
      cookies.sessionToken.name,
      encoded,
      { ...cookies.sessionToken.options, expires: new Date(Date.now() + config.jwtMaxAge * 1000) },
      existing,
    );
    return;
  }
  const adapter = config.adapter;
  if (!adapter?.createSession) throw new Error("database sessions need an adapter with createSession");
  const sessionToken = config.generateSessionToken();
  const expires = new Date(Date.now() + config.maxAge * 1000);
  await adapter.createSession({ sessionToken, userId: user.id, expires });
  jar.set(cookies.sessionToken.name, sessionToken, { ...cookies.sessionToken.options, expires });
}

export async function destroySession(ctx: RequestContext, jar: ResponseCookies): Promise<SessionState> {
  const { config, cookies } = ctx;
  const existing = requestCookies(ctx.request);
  const state = await readSession(ctx).catch(() => ({ session: null }) as SessionState);
  if (config.strategy === "database") {
    const raw = existing[cookies.sessionToken.name];
    if (raw) await config.adapter?.deleteSession?.(raw);
  }
  jar.deleteChunked(cookies.sessionToken.name, cookies.sessionToken.options, existing);
  return state;
}

export async function patchSessionClaims(ctx: RequestContext, jar: ResponseCookies, patch: Record<string, unknown>): Promise<void> {
  const { config, cookies } = ctx;
  if (config.strategy !== "jwt") return;
  const existing = requestCookies(ctx.request);
  const raw = readChunked(existing, cookies.sessionToken.name);
  if (!raw) return;
  const token = await config.decode({ token: raw, secret: ctx.secrets, salt: cookies.sessionToken.name });
  if (!token) return;
  const { exp: _exp, iat: _iat, jti: _jti, ...claims } = token;
  const encoded = await config.encode({ token: { ...claims, ...patch }, secret: ctx.secrets, salt: cookies.sessionToken.name, maxAge: config.jwtMaxAge });
  jar.setChunked(
    cookies.sessionToken.name,
    encoded,
    { ...cookies.sessionToken.options, expires: new Date(Date.now() + config.jwtMaxAge * 1000) },
    existing,
  );
}

export async function requireSession(ctx: RequestContext, jar?: ResponseCookies): Promise<SessionState & { session: Session }> {
  const state = await readSession(ctx, jar);
  if (!state.session) throw new AuthError("Unauthorized", "sign in required", 401);
  return state as SessionState & { session: Session };
}
