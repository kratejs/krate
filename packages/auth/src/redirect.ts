import type { ResponseCookies } from "./cookies.js";
import type { RequestContext } from "./config.js";

export function isJsonClient(request: Request): boolean {
  return request.headers.get("x-auth-return-redirect") === "1" || (request.headers.get("accept") ?? "").includes("application/json");
}

export async function sanitizeRedirect(ctx: RequestContext, target: string | null | undefined, fallback?: string): Promise<string> {
  const { baseUrl, config } = ctx;
  const fallbackUrl = fallback ?? baseUrl + "/";
  if (!target) return fallbackUrl;
  let value = target;
  if (config.callbacks.redirect) {
    try {
      const out = await config.callbacks.redirect({ url: value, baseUrl });
      if (typeof out === "string" && out) return out;
    } catch (err) {
      config.logger.error("RedirectCallbackError", err);
    }
    return fallbackUrl;
  }
  if (value.startsWith("//") || value.startsWith("/\\") || /[\r\n\t]/.test(value)) return fallbackUrl;
  if (value.startsWith("/")) return baseUrl + value;
  try {
    const parsed = new URL(value);
    if (parsed.origin === baseUrl) return parsed.toString();
    if (config.redirects.allowed.some((allowed) => allowed === parsed.origin || allowed === parsed.toString())) return parsed.toString();
  } catch {
    value = "";
  }
  return fallbackUrl;
}

export function redirectResponse(ctx: RequestContext, url: string, jar?: ResponseCookies, extra?: Record<string, unknown>): Response {
  const headers = new Headers();
  headers.set("cache-control", "no-store");
  jar?.apply(headers);
  if (isJsonClient(ctx.request)) {
    headers.set("content-type", "application/json");
    return new Response(JSON.stringify({ url, ...extra }), { status: 200, headers });
  }
  headers.set("location", url);
  return new Response(null, { status: 302, headers });
}

export function jsonResponse(body: unknown, init: { status?: number; jar?: ResponseCookies; headers?: Record<string, string> } = {}): Response {
  const headers = new Headers({ "content-type": "application/json", "cache-control": "no-store", ...(init.headers ?? {}) });
  init.jar?.apply(headers);
  return new Response(JSON.stringify(body), { status: init.status ?? 200, headers });
}

export function pagePath(ctx: RequestContext, custom: string | undefined, builtin: string): string {
  if (custom) return custom.startsWith("http") ? custom : ctx.baseUrl + custom;
  return ctx.baseUrl + ctx.config.basePath + builtin;
}

export function errorRedirect(ctx: RequestContext, code: string, jar?: ResponseCookies, extra: Record<string, string> = {}): Response {
  const target = new URL(pagePath(ctx, ctx.config.pages.error, "/error"));
  target.searchParams.set("error", code);
  for (const [k, v] of Object.entries(extra)) target.searchParams.set(k, v);
  if (isJsonClient(ctx.request)) {
    return jsonResponse({ url: target.toString(), error: code }, { status: 200, jar });
  }
  const headers = new Headers({ location: target.toString(), "cache-control": "no-store" });
  jar?.apply(headers);
  return new Response(null, { status: 302, headers });
}
