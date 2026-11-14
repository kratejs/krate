import { AuthError, type RequestContext } from "./config.js";
import { ResponseCookies } from "./cookies.js";
import { sealString, unsealString } from "./crypto.js";
import { errorRedirect, isJsonClient, jsonResponse, pagePath, redirectResponse, sanitizeRedirect } from "./redirect.js";
import { patchSessionClaims, requestCookies, requireSession } from "./session.js";
import { assertCsrf, finalizeSignIn, limit, parseBody, type PendingTwoFactor } from "./signin.js";
import { consumeBackupCode, generateBackupCodes, generateSecret, otpauthUrl, verifyTotp } from "./totp.js";
import type { Adapter, AdapterUser, TwoFactorRecord } from "./types.js";

const SEAL_PURPOSE = "krate-auth-2fa-secret";

function adapterFor(ctx: RequestContext): Adapter & Required<Pick<Adapter, "getTwoFactor" | "upsertTwoFactor" | "deleteTwoFactor">> {
  const adapter = ctx.config.adapter;
  if (!ctx.config.twoFactor.enabled) throw new AuthError("TwoFactorDisabled", "two-factor authentication is not enabled", 404);
  if (!adapter?.getTwoFactor || !adapter.upsertTwoFactor || !adapter.deleteTwoFactor) {
    throw new AuthError("Configuration", "two-factor authentication needs an adapter with two-factor storage", 500);
  }
  return adapter as ReturnType<typeof adapterFor>;
}

function openSecret(ctx: RequestContext, record: TwoFactorRecord): string {
  const secret = unsealString(record.secret, ctx.secrets, SEAL_PURPOSE);
  if (!secret) throw new AuthError("Configuration", "the stored two-factor secret cannot be decrypted with the configured AUTH_SECRET", 500);
  return secret;
}

function totpOptions(ctx: RequestContext) {
  const { digits, period, algorithm, window } = ctx.config.twoFactor;
  return { digits, period, algorithm, window };
}

function looksLikeTotp(ctx: RequestContext, code: string): boolean {
  return new RegExp(`^[0-9 ]{${ctx.config.twoFactor.digits},${ctx.config.twoFactor.digits + 3}}$`).test(code.trim());
}

interface CheckResult {
  ok: boolean;
  record: TwoFactorRecord;
  usedBackupCode: boolean;
}

async function checkCode(ctx: RequestContext, adapter: ReturnType<typeof adapterFor>, userId: string, code: string, allowBackup: boolean): Promise<CheckResult | null> {
  const record = await adapter.getTwoFactor(userId);
  if (!record || !record.confirmed) return null;
  if (looksLikeTotp(ctx, code)) {
    const counter = verifyTotp(code, openSecret(ctx, record), { ...totpOptions(ctx), lastUsedCounter: record.lastUsedCounter });
    if (counter === null) return { ok: false, record, usedBackupCode: false };
    const next = { ...record, lastUsedCounter: counter };
    await adapter.upsertTwoFactor(next);
    return { ok: true, record: next, usedBackupCode: false };
  }
  if (allowBackup) {
    const remaining = consumeBackupCode(code, record.backupCodes);
    if (remaining) {
      const next = { ...record, backupCodes: remaining };
      await adapter.upsertTwoFactor(next);
      return { ok: true, record: next, usedBackupCode: true };
    }
  }
  return { ok: false, record, usedBackupCode: false };
}

function describeAccount(user: { email?: unknown; name?: unknown; id: string }): string {
  return String(user.email ?? user.name ?? user.id);
}

async function setFlag(ctx: RequestContext, adapter: Adapter, userId: string, enabled: boolean): Promise<void> {
  await adapter.updateUser({ id: userId, twoFactorEnabled: enabled });
}

export async function handleTwoFactor(ctx: RequestContext, segments: string[]): Promise<Response | null> {
  const op = segments[1];
  const method = ctx.request.method.toUpperCase();
  if (!op) return null;
  if (op === "verify" && method === "POST") return verifyPending(ctx);
  if (op === "status" && method === "GET") return status(ctx);
  if (method !== "POST") return null;
  switch (op) {
    case "setup":
      return setup(ctx);
    case "enable":
      return enable(ctx);
    case "disable":
      return disable(ctx);
    case "backup-codes":
      return regenerate(ctx);
    default:
      return null;
  }
}

async function status(ctx: RequestContext): Promise<Response> {
  const adapter = adapterFor(ctx);
  const { session } = await requireSession(ctx);
  const record = await adapter.getTwoFactor(session.user.id);
  return jsonResponse({
    enabled: Boolean(record?.confirmed),
    pending: Boolean(record && !record.confirmed),
    backupCodesRemaining: record?.confirmed ? record.backupCodes.length : 0,
    required: ctx.config.twoFactor.required,
  });
}

async function setup(ctx: RequestContext): Promise<Response> {
  const adapter = adapterFor(ctx);
  const body = await parseBody(ctx.request);
  assertCsrf(ctx, body.csrfToken);
  const { session } = await requireSession(ctx);
  const existing = await adapter.getTwoFactor(session.user.id);
  if (existing?.confirmed) throw new AuthError("AlreadyEnabled", "two-factor authentication is already enabled; disable it first", 409);
  const options = ctx.config.twoFactor;
  const secret = generateSecret(options.algorithm);
  await adapter.upsertTwoFactor({ userId: session.user.id, secret: sealString(secret, ctx.secret, SEAL_PURPOSE), backupCodes: [], confirmed: false, lastUsedCounter: null });
  return jsonResponse({
    secret,
    otpauthUrl: otpauthUrl({ secret, account: describeAccount(session.user), issuer: options.issuer, digits: options.digits, period: options.period, algorithm: options.algorithm }),
    algorithm: options.algorithm,
    digits: options.digits,
    period: options.period,
  });
}

async function enable(ctx: RequestContext): Promise<Response> {
  const adapter = adapterFor(ctx);
  const body = await parseBody(ctx.request);
  assertCsrf(ctx, body.csrfToken);
  const { session } = await requireSession(ctx);
  limit(ctx, "2fa-enable", session.user.id, 5);
  const record = await adapter.getTwoFactor(session.user.id);
  if (!record) throw new AuthError("NotStarted", "start the setup first", 409);
  if (record.confirmed) throw new AuthError("AlreadyEnabled", "two-factor authentication is already enabled", 409);
  const counter = verifyTotp(body.code ?? "", openSecret(ctx, record), { ...totpOptions(ctx), lastUsedCounter: null });
  if (counter === null) throw new AuthError("InvalidCode", "that code is not valid", 401);
  const codes = generateBackupCodes(ctx.config.twoFactor.backupCodes);
  await adapter.upsertTwoFactor({ ...record, confirmed: true, backupCodes: codes.stored, lastUsedCounter: counter });
  await setFlag(ctx, adapter, session.user.id, true);
  const jar = new ResponseCookies();
  await patchSessionClaims(ctx, jar, { tfe: true, tfv: true });
  if (ctx.config.events.twoFactorEnabled) await ctx.config.events.twoFactorEnabled({ user: session.user });
  return jsonResponse({ enabled: true, backupCodes: codes.plain }, { jar });
}

async function disable(ctx: RequestContext): Promise<Response> {
  const adapter = adapterFor(ctx);
  const body = await parseBody(ctx.request);
  assertCsrf(ctx, body.csrfToken);
  const { session } = await requireSession(ctx);
  limit(ctx, "2fa-disable", session.user.id, 5);
  const result = await checkCode(ctx, adapter, session.user.id, body.code ?? "", true);
  if (!result?.ok) throw new AuthError("InvalidCode", "that code is not valid", 401);
  await adapter.deleteTwoFactor(session.user.id);
  await setFlag(ctx, adapter, session.user.id, false);
  const jar = new ResponseCookies();
  await patchSessionClaims(ctx, jar, { tfe: false });
  if (ctx.config.events.twoFactorDisabled) await ctx.config.events.twoFactorDisabled({ user: session.user });
  return jsonResponse({ enabled: false }, { jar });
}

async function regenerate(ctx: RequestContext): Promise<Response> {
  const adapter = adapterFor(ctx);
  const body = await parseBody(ctx.request);
  assertCsrf(ctx, body.csrfToken);
  const { session } = await requireSession(ctx);
  limit(ctx, "2fa-backup", session.user.id, 5);
  const result = await checkCode(ctx, adapter, session.user.id, body.code ?? "", false);
  if (!result?.ok) throw new AuthError("InvalidCode", "that code is not valid", 401);
  const codes = generateBackupCodes(ctx.config.twoFactor.backupCodes);
  await adapter.upsertTwoFactor({ ...result.record, backupCodes: codes.stored });
  return jsonResponse({ backupCodes: codes.plain });
}

async function verifyPending(ctx: RequestContext): Promise<Response> {
  const { config, cookies } = ctx;
  const adapter = adapterFor(ctx);
  const body = await parseBody(ctx.request);
  assertCsrf(ctx, body.csrfToken);
  const raw = requestCookies(ctx.request)[cookies.pendingTwoFactor.name];
  const pending = raw ? ((await config.decode({ token: raw, secret: ctx.secrets, salt: cookies.pendingTwoFactor.name })) as (PendingTwoFactor & Record<string, unknown>) | null) : null;
  if (!pending?.uid) throw new AuthError("SessionExpired", "the sign in expired, start again", 401);
  limit(ctx, "2fa-verify", pending.uid, 5);

  const fail = (): Response => {
    if (isJsonClient(ctx.request)) return jsonResponse({ error: "InvalidCode" }, { status: 401 });
    const target = new URL(pagePath(ctx, config.pages.twoFactor, "/2fa"));
    target.searchParams.set("error", "InvalidCode");
    target.searchParams.set("callbackUrl", pending.callbackUrl);
    return redirectResponse(ctx, target.toString());
  };

  const result = await checkCode(ctx, adapter, pending.uid, body.code ?? "", true);
  if (!result?.ok) return fail();
  const user = (await adapter.getUser(pending.uid)) as AdapterUser | null;
  if (!user) return errorRedirect(ctx, "Configuration");
  const jar = new ResponseCookies();
  await finalizeSignIn(ctx, jar, {
    user,
    account: pending.provider ? { type: pending.method === "oauth" ? "oauth" : pending.method === "email" ? "email" : "credentials", provider: pending.provider, providerAccountId: pending.providerAccountId ?? user.id } : null,
    isNewUser: pending.isNewUser,
    method: pending.method,
    enrolled: true,
    twoFactorVerified: true,
  });
  const destination = await sanitizeRedirect(ctx, pending.callbackUrl);
  return redirectResponse(ctx, destination, jar, result.usedBackupCode ? { usedBackupCode: true, backupCodesRemaining: result.record.backupCodes.length } : undefined);
}
