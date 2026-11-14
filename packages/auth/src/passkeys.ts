import { AuthError, type RequestContext } from "./config.js";
import { ResponseCookies } from "./cookies.js";
import { b64urlEncode, randomId, randomToken, utf8 } from "./bytes.js";
import { sha256Hex } from "./crypto.js";
import { jsonResponse } from "./redirect.js";
import { readSession, requestCookies, requireSession } from "./session.js";
import { assertCsrf, completeSignIn, limit } from "./signin.js";
import type { Adapter, Authenticator } from "./types.js";
import {
  SUPPORTED_ALGORITHMS,
  WebAuthnError,
  verifyAuthentication,
  verifyRegistration,
  type AuthenticationResponseJSON,
  type RegistrationResponseJSON,
} from "./webauthn.js";

type PasskeyAdapter = Adapter & Required<Pick<Adapter, "getAuthenticator" | "createAuthenticator" | "updateAuthenticatorCounter" | "listAuthenticatorsByUserId" | "deleteAuthenticator">>;

interface ChallengeState {
  challenge: string;
  kind: "register" | "authenticate";
  uid?: string;
  email?: string | null;
  name?: string | null;
}

function adapterFor(ctx: RequestContext): PasskeyAdapter {
  const adapter = ctx.config.adapter;
  if (!ctx.config.passkeys.enabled) throw new AuthError("PasskeysDisabled", "passkeys are not enabled", 404);
  if (!adapter?.getAuthenticator || !adapter.createAuthenticator || !adapter.updateAuthenticatorCounter || !adapter.listAuthenticatorsByUserId || !adapter.deleteAuthenticator) {
    throw new AuthError("Configuration", "passkeys need an adapter with authenticator storage", 500);
  }
  return adapter as PasskeyAdapter;
}

function relyingParty(ctx: RequestContext) {
  const { passkeys, twoFactor } = ctx.config;
  const hostname = new URL(ctx.baseUrl).hostname;
  return {
    id: passkeys.rpId ?? hostname,
    name: passkeys.rpName ?? twoFactor.issuer,
    origins: [ctx.baseUrl, ...(passkeys.origins ?? [])],
  };
}

async function readJson(ctx: RequestContext): Promise<Record<string, any>> {
  const body = (await ctx.request.json().catch(() => null)) as Record<string, any> | null;
  if (!body || typeof body !== "object") throw new AuthError("InvalidRequest", "a JSON body is required", 400);
  return body;
}

function challengeKey(ctx: RequestContext, kind: ChallengeState["kind"]): { identifier: string; hash: (challenge: string) => string } {
  return { identifier: `webauthn:${kind}`, hash: (challenge) => sha256Hex(`${challenge}${ctx.secret}`) };
}

async function storeChallenge(ctx: RequestContext, jar: ResponseCookies, state: ChallengeState): Promise<void> {
  const spec = ctx.cookies.webauthnChallenge;
  const adapter = adapterFor(ctx);
  if (!adapter.createVerificationToken) throw new AuthError("Configuration", "passkeys need an adapter with verification tokens", 500);
  const key = challengeKey(ctx, state.kind);
  await adapter.createVerificationToken({ identifier: key.identifier, token: key.hash(state.challenge), expires: new Date(Date.now() + 300_000) });
  const token = await ctx.config.encode({ token: { ...state }, secret: ctx.secrets, salt: spec.name, maxAge: 300 });
  jar.set(spec.name, token, { ...spec.options, maxAge: 300 });
}

async function takeChallenge(ctx: RequestContext, jar: ResponseCookies, kind: ChallengeState["kind"]): Promise<ChallengeState> {
  const spec = ctx.cookies.webauthnChallenge;
  const raw = requestCookies(ctx.request)[spec.name];
  jar.delete(spec.name, spec.options);
  const claims = raw ? await ctx.config.decode({ token: raw, secret: ctx.secrets, salt: spec.name }) : null;
  if (!claims || claims.kind !== kind || typeof claims.challenge !== "string") {
    throw new AuthError("ChallengeExpired", "the passkey request expired, start again", 400);
  }
  const adapter = adapterFor(ctx);
  const key = challengeKey(ctx, kind);
  const consumed = await adapter.useVerificationToken?.({ identifier: key.identifier, token: key.hash(claims.challenge) });
  if (!consumed || consumed.expires.valueOf() < Date.now()) {
    throw new AuthError("ChallengeExpired", "the passkey request expired or was already used, start again", 400);
  }
  return claims as unknown as ChallengeState;
}

function failure(ctx: RequestContext, err: unknown): never {
  if (err instanceof WebAuthnError) {
    ctx.config.logger.warn("PasskeyVerificationFailed", { code: err.code, message: err.message });
    throw new AuthError("PasskeyError", "passkey verification failed", 401);
  }
  throw err;
}

export async function handlePasskey(ctx: RequestContext, segments: string[]): Promise<Response | null> {
  const [, a, b] = segments;
  const method = ctx.request.method.toUpperCase();
  if (a === "list" && method === "GET") return list(ctx);
  if (method !== "POST") return null;
  if (a === "register" && b === "options") return registerOptions(ctx);
  if (a === "register" && b === "verify") return registerVerify(ctx);
  if (a === "authenticate" && b === "options") return authenticateOptions(ctx);
  if (a === "authenticate" && b === "verify") return authenticateVerify(ctx);
  if (a === "delete") return remove(ctx);
  return null;
}

async function list(ctx: RequestContext): Promise<Response> {
  const adapter = adapterFor(ctx);
  const { session } = await requireSession(ctx);
  const items = await adapter.listAuthenticatorsByUserId(session.user.id);
  return jsonResponse({
    passkeys: items.map((a) => ({
      credentialID: a.credentialID,
      name: a.name ?? null,
      deviceType: a.credentialDeviceType,
      backedUp: a.credentialBackedUp,
      transports: a.transports ? a.transports.split(",") : [],
      createdAt: a.createdAt ?? null,
    })),
  });
}

async function registerOptions(ctx: RequestContext): Promise<Response> {
  const adapter = adapterFor(ctx);
  const body = await readJson(ctx);
  assertCsrf(ctx, body.csrfToken);
  limit(ctx, "passkey-register-options");
  const rp = relyingParty(ctx);
  const { passkeys } = ctx.config;
  const current = await readSession(ctx);
  let userHandle: string;
  let userName: string;
  let displayName: string;
  let exclude: Authenticator[] = [];
  const state: ChallengeState = { challenge: randomToken(32), kind: "register" };

  if (current.session) {
    const user = current.session.user;
    state.uid = user.id;
    userHandle = user.id;
    userName = String(user.email ?? user.name ?? user.id);
    displayName = String(user.name ?? user.email ?? user.id);
    exclude = await adapter.listAuthenticatorsByUserId(user.id);
  } else {
    if (passkeys.allowSignUp === false) throw new AuthError("SignUpDisabled", "creating accounts with a passkey is disabled", 403);
    const email = typeof body.email === "string" && body.email.trim() ? body.email.trim().toLowerCase() : null;
    if (email) {
      if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) throw new AuthError("InvalidEmail", "that email address is not valid", 400);
      if (await adapter.getUserByEmail(email)) throw new AuthError("EmailTaken", "an account with that email already exists; sign in and add a passkey from your account", 409);
    }
    const name = typeof body.name === "string" && body.name.trim() ? body.name.trim().slice(0, 100) : null;
    state.email = email;
    state.name = name;
    userHandle = randomId();
    userName = email ?? name ?? "passkey-user";
    displayName = name ?? email ?? "Passkey user";
  }

  const jar = new ResponseCookies();
  await storeChallenge(ctx, jar, state);
  return jsonResponse(
    {
      rp: { id: rp.id, name: rp.name },
      user: { id: b64urlEncode(utf8(userHandle)), name: userName, displayName },
      challenge: state.challenge,
      pubKeyCredParams: SUPPORTED_ALGORITHMS.map((alg) => ({ type: "public-key", alg })),
      timeout: passkeys.timeout,
      attestation: passkeys.attestation,
      authenticatorSelection: { residentKey: passkeys.residentKey, requireResidentKey: passkeys.residentKey === "required", userVerification: passkeys.userVerification },
      excludeCredentials: exclude.map((a) => ({ type: "public-key", id: a.credentialID, transports: a.transports ? a.transports.split(",") : undefined })),
    },
    { jar },
  );
}

async function registerVerify(ctx: RequestContext): Promise<Response> {
  const adapter = adapterFor(ctx);
  const body = await readJson(ctx);
  assertCsrf(ctx, body.csrfToken);
  limit(ctx, "passkey-register-verify");
  const jar = new ResponseCookies();
  const state = await takeChallenge(ctx, jar, "register");
  const rp = relyingParty(ctx);
  const { passkeys } = ctx.config;

  let verified;
  try {
    verified = verifyRegistration({
      credential: body.credential as RegistrationResponseJSON,
      challenge: state.challenge,
      origins: rp.origins,
      rpId: rp.id,
      requireUserVerification: passkeys.userVerification === "required",
    });
  } catch (err) {
    failure(ctx, err);
  }
  if (await adapter.getAuthenticator(verified.credentialID)) throw new AuthError("AlreadyRegistered", "that passkey is already registered", 409);

  const label = typeof body.passkeyName === "string" && body.passkeyName.trim() ? body.passkeyName.trim().slice(0, 100) : null;
  const make = (userId: string): Authenticator => ({
    credentialID: verified.credentialID,
    userId,
    providerAccountId: verified.credentialID,
    credentialPublicKey: verified.publicKey,
    counter: verified.counter,
    credentialDeviceType: verified.deviceType,
    credentialBackedUp: verified.backedUp,
    transports: verified.transports.length > 0 ? verified.transports.join(",") : null,
    name: label,
  });

  if (state.uid) {
    const current = await readSession(ctx);
    if (current.session?.user.id !== state.uid) throw new AuthError("Unauthorized", "the session changed during registration", 401);
    await adapter.createAuthenticator(make(state.uid));
    if (ctx.config.events.passkeyRegistered) await ctx.config.events.passkeyRegistered({ user: current.session.user, credentialID: verified.credentialID });
    return jsonResponse({ verified: true, credentialID: verified.credentialID }, { jar });
  }

  const user = await adapter.createUser({ email: state.email ?? null, name: state.name ?? null, emailVerified: null });
  try {
    await adapter.createAuthenticator(make(user.id));
    await adapter.linkAccount({ userId: user.id, type: "webauthn", provider: "passkey", providerAccountId: verified.credentialID });
  } catch (err) {
    await adapter.deleteUser?.(user.id);
    throw err;
  }
  if (ctx.config.events.createUser) await ctx.config.events.createUser({ user });
  if (ctx.config.events.passkeyRegistered) await ctx.config.events.passkeyRegistered({ user, credentialID: verified.credentialID });
  return completeSignIn(ctx, {
    user,
    account: { type: "webauthn", provider: "passkey", providerAccountId: verified.credentialID, userId: user.id },
    isNewUser: true,
    method: "passkey",
    callbackUrl: typeof body.callbackUrl === "string" ? body.callbackUrl : undefined,
    skipTwoFactor: true,
    jar,
  });
}

async function authenticateOptions(ctx: RequestContext): Promise<Response> {
  adapterFor(ctx);
  const body = await readJson(ctx);
  assertCsrf(ctx, body.csrfToken);
  limit(ctx, "passkey-auth-options");
  const rp = relyingParty(ctx);
  const state: ChallengeState = { challenge: randomToken(32), kind: "authenticate" };
  const jar = new ResponseCookies();
  await storeChallenge(ctx, jar, state);
  return jsonResponse(
    { challenge: state.challenge, rpId: rp.id, timeout: ctx.config.passkeys.timeout, userVerification: ctx.config.passkeys.userVerification, allowCredentials: [] },
    { jar },
  );
}

async function authenticateVerify(ctx: RequestContext): Promise<Response> {
  const adapter = adapterFor(ctx);
  const body = await readJson(ctx);
  assertCsrf(ctx, body.csrfToken);
  limit(ctx, "passkey-auth-verify");
  const jar = new ResponseCookies();
  const state = await takeChallenge(ctx, jar, "authenticate");
  const credential = body.credential as AuthenticationResponseJSON | undefined;
  if (!credential || typeof credential.id !== "string") throw new AuthError("InvalidRequest", "a credential is required", 400);
  const stored = await adapter.getAuthenticator(credential.id);
  if (!stored) {
    ctx.config.logger.warn("PasskeyUnknownCredential", { id: credential.id });
    throw new AuthError("PasskeyError", "passkey verification failed", 401);
  }
  const rp = relyingParty(ctx);
  let result;
  try {
    result = verifyAuthentication({
      credential,
      challenge: state.challenge,
      origins: rp.origins,
      rpId: rp.id,
      requireUserVerification: ctx.config.passkeys.userVerification === "required",
      publicKey: stored.credentialPublicKey,
      storedCounter: stored.counter,
    });
  } catch (err) {
    failure(ctx, err);
  }
  await adapter.updateAuthenticatorCounter(stored.credentialID, result.counter);
  const user = await adapter.getUser(stored.userId);
  if (!user) throw new AuthError("PasskeyError", "passkey verification failed", 401);
  return completeSignIn(ctx, {
    user,
    account: { type: "webauthn", provider: "passkey", providerAccountId: stored.credentialID, userId: user.id },
    method: "passkey",
    callbackUrl: typeof body.callbackUrl === "string" ? body.callbackUrl : undefined,
    skipTwoFactor: result.userVerified,
    jar,
  });
}

async function remove(ctx: RequestContext): Promise<Response> {
  const adapter = adapterFor(ctx);
  const body = await readJson(ctx);
  assertCsrf(ctx, body.csrfToken);
  const { session } = await requireSession(ctx);
  const id = typeof body.credentialID === "string" ? body.credentialID : "";
  const stored = id ? await adapter.getAuthenticator(id) : null;
  if (!stored || stored.userId !== session.user.id) throw new AuthError("NotFound", "no such passkey", 404);
  await adapter.deleteAuthenticator(id);
  return jsonResponse({ deleted: true });
}
