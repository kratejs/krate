import { AuthError, type RequestContext } from "./config.js";
import { ResponseCookies } from "./cookies.js";
import { randomToken } from "./bytes.js";
import { sha256Hex } from "./crypto.js";
import { errorRedirect, pagePath, redirectResponse } from "./redirect.js";
import { assertCsrf, completeSignIn, limit, parseBody, resolveCallbackUrl } from "./signin.js";
import { resolveEmailUser } from "./users.js";
import type { EmailConfig } from "./types.js";

function hashToken(ctx: RequestContext, token: string): string {
  return sha256Hex(`${token}${ctx.secret}`);
}

export async function startEmail(ctx: RequestContext, provider: EmailConfig): Promise<Response> {
  const adapter = ctx.config.adapter;
  if (!adapter?.createVerificationToken) throw new AuthError("Configuration", "the email provider needs an adapter with verification tokens", 500);
  const body = await parseBody(ctx.request);
  assertCsrf(ctx, body.csrfToken);
  let identifier: string;
  try {
    identifier = (provider.normalizeIdentifier ?? ((v: string) => v.trim().toLowerCase()))(body.email ?? "");
  } catch {
    return errorRedirect(ctx, "EmailSignin");
  }
  limit(ctx, "email-start", identifier);
  const callbackUrl = await resolveCallbackUrl(ctx, body.callbackUrl);

  if (ctx.config.callbacks.signIn) {
    const existing = await adapter.getUserByEmail(identifier);
    const decision = await ctx.config.callbacks.signIn({
      user: existing ?? { id: identifier, email: identifier },
      account: { type: "email", provider: provider.id, providerAccountId: identifier },
      email: { verificationRequest: true },
    });
    if (decision === false) return errorRedirect(ctx, "AccessDenied");
  }

  const token = provider.generateVerificationToken ? await provider.generateVerificationToken() : randomToken(32);
  const expires = new Date(Date.now() + provider.maxAge * 1000);
  const url = new URL(`${ctx.baseUrl}${ctx.config.basePath}/callback/${provider.id}`);
  url.searchParams.set("callbackUrl", callbackUrl);
  url.searchParams.set("token", token);
  url.searchParams.set("email", identifier);
  await adapter.createVerificationToken({ identifier, token: hashToken(ctx, token), expires });
  try {
    await provider.sendVerificationRequest({ identifier, url: url.toString(), token, expires, provider, request: ctx.request });
  } catch (err) {
    ctx.config.logger.error("SendVerificationRequestError", err);
    return errorRedirect(ctx, "EmailSignin");
  }
  return redirectResponse(ctx, pagePath(ctx, ctx.config.pages.verifyRequest, "/verify-request"));
}

export async function finishEmail(ctx: RequestContext, provider: EmailConfig): Promise<Response> {
  const adapter = ctx.config.adapter;
  if (!adapter?.useVerificationToken) throw new AuthError("Configuration", "the email provider needs an adapter with verification tokens", 500);
  if (ctx.request.method.toUpperCase() !== "GET") return errorRedirect(ctx, "Verification");
  const token = ctx.url.searchParams.get("token");
  const emailParam = ctx.url.searchParams.get("email");
  if (!token || !emailParam) return errorRedirect(ctx, "Verification");
  let identifier: string;
  try {
    identifier = (provider.normalizeIdentifier ?? ((v: string) => v.trim().toLowerCase()))(emailParam);
  } catch {
    return errorRedirect(ctx, "Verification");
  }
  limit(ctx, "email-verify", identifier);
  const record = await adapter.useVerificationToken({ identifier, token: hashToken(ctx, token) });
  if (!record || record.expires.valueOf() < Date.now() || record.identifier !== identifier) return errorRedirect(ctx, "Verification");
  const result = await resolveEmailUser(ctx, identifier, provider.id);
  const callbackUrl = await resolveCallbackUrl(ctx, ctx.url.searchParams.get("callbackUrl"));
  return completeSignIn(ctx, {
    user: result.user,
    account: result.account,
    isNewUser: result.isNewUser,
    method: "email",
    callbackUrl,
    jar: new ResponseCookies(),
  });
}
