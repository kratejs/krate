import { AuthError, createContext, type ResolvedConfig } from "./config.js";
import { ResponseCookies } from "./cookies.js";
import { matchesPath } from "./match.js";
import { readSession } from "./session.js";
import type { ProtectRule, Session } from "./types.js";

export type AuthMiddleware = (request: Request) => Promise<Response | undefined>;

function wantsJson(request: Request, pathname: string): boolean {
  return pathname.startsWith("/api/") || (request.headers.get("accept") ?? "").includes("application/json");
}

function deny(request: Request, url: URL, status: number, message: string): Response {
  if (wantsJson(request, url.pathname)) return Response.json({ error: message }, { status, headers: { "cache-control": "no-store" } });
  return new Response(message, { status, headers: { "content-type": "text/plain; charset=utf-8", "cache-control": "no-store" } });
}

function signInRedirect(config: ResolvedConfig, request: Request, url: URL, target?: string): Response {
  const base = target ?? config.pages.signIn ?? `${config.basePath}/signin`;
  const location = new URL(base, url.origin);
  location.searchParams.set("callbackUrl", url.pathname + url.search);
  return new Response(null, { status: 302, headers: { location: location.toString(), "cache-control": "no-store" } });
}

function findRule(rules: ProtectRule[], pathname: string): ProtectRule | undefined {
  return rules.find((rule) => matchesPath(rule.source, pathname));
}

function passThrough(jar: ResponseCookies): Response | undefined {
  const entries = jar.list();
  if (entries.length === 0) return undefined;
  const headers = new Headers({ "x-middleware-next": "1" });
  jar.apply(headers);
  return new Response(null, { headers });
}

export function createMiddleware(getConfig: () => ResolvedConfig): AuthMiddleware {
  return async (request: Request): Promise<Response | undefined> => {
    const config = getConfig();
    if (!config.enabled) return undefined;
    const url = new URL(request.url);
    const base = config.basePath.replace(/\/+$/, "");
    if (url.pathname === base || url.pathname.startsWith(base + "/")) return undefined;

    const rule = findRule(config.protect, url.pathname);
    const authorized = config.callbacks.authorized;
    if (!rule && !authorized) return undefined;

    let session: Session | null = null;
    const jar = new ResponseCookies();
    try {
      const ctx = createContext(config, request);
      session = (await readSession(ctx, jar)).session;
    } catch (err) {
      if (err instanceof AuthError && err.status >= 500) {
        config.logger.error("MiddlewareConfigError", err.message);
        return deny(request, url, 503, "authentication is not configured");
      }
      config.logger.error("MiddlewareSessionError", err);
      return deny(request, url, 503, "authentication unavailable");
    }

    if (authorized) {
      let decision: boolean | Response | undefined | void;
      try {
        decision = await authorized({ request, auth: session });
      } catch (err) {
        config.logger.error("AuthorizedCallbackError", err);
        return deny(request, url, 503, "authentication unavailable");
      }
      if (decision instanceof Response) return decision;
      if (decision === false) return rule?.redirect || !wantsJson(request, url.pathname) ? signInRedirect(config, request, url, rule?.redirect) : deny(request, url, rule?.status ?? 401, "unauthorized");
    }

    if (!rule) return passThrough(jar);

    if (!session) {
      if (rule.redirect) return signInRedirect(config, request, url, rule.redirect);
      if (rule.status) return deny(request, url, rule.status, rule.status === 404 ? "not found" : rule.status === 403 ? "forbidden" : "unauthorized");
      if (wantsJson(request, url.pathname)) return deny(request, url, 401, "unauthorized");
      return signInRedirect(config, request, url);
    }

    if (rule.roles && rule.roles.length > 0) {
      const role = session.user.role;
      if (!role || !rule.roles.includes(role)) {
        return deny(request, url, rule.status === 404 ? 404 : 403, rule.status === 404 ? "not found" : "forbidden");
      }
    }
    return passThrough(jar);
  };
}
