import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryAdapter } from "./adapters/memory";
import { b64urlDecode, b64urlEncode } from "./bytes";
import {
  configureAuthClient,
  deletePasskey,
  getCsrfToken,
  getProviders,
  getSession,
  isPasskeySupported,
  listPasskeys,
  registerPasskey,
  signIn,
  signInWithPasskey,
  signOut,
  twoFactor,
  updateSession,
} from "./client";
import { credentials } from "./providers/credentials";
import { genericOAuth } from "./providers/oauth";
import { passkey } from "./providers/passkey";
import { resetRateLimits } from "./ratelimit";
import { ORIGIN, makeClient, type Client } from "./test-utils";
import { VirtualAuthenticator } from "./test-authenticator";
import { totp } from "./totp";
import type { AuthOptions } from "./types";

const navigated: string[] = [];

function wire(c: Client) {
  configureAuthClient({
    basePath: "/api/auth",
    baseUrl: ORIGIN,
    navigate: (url) => void navigated.push(url),
    fetch: (async (input: string | URL | Request, init?: RequestInit) => {
      const url = new URL(String(input));
      return c.request(url.pathname + url.search, init ?? {});
    }) as typeof fetch,
  });
}

function buffer(b64: string): ArrayBuffer {
  return b64urlDecode(b64).slice().buffer;
}

function installAuthenticator(authn: VirtualAuthenticator, behaviour: { cancel?: boolean } = {}) {
  const credentialsStub = {
    async create({ publicKey }: any) {
      if (behaviour.cancel) throw Object.assign(new Error("denied"), { name: "NotAllowedError" });
      const json = authn.create({ challenge: b64urlEncode(new Uint8Array(publicKey.challenge)), rp: publicKey.rp });
      return {
        id: json.id,
        rawId: buffer(json.rawId!),
        type: "public-key",
        authenticatorAttachment: "platform",
        getClientExtensionResults: () => ({}),
        response: { clientDataJSON: buffer(json.response.clientDataJSON), attestationObject: buffer(json.response.attestationObject), getTransports: () => ["internal"] },
      };
    },
    async get({ publicKey }: any) {
      if (behaviour.cancel) throw Object.assign(new Error("denied"), { name: "NotAllowedError" });
      const json = authn.get({ challenge: b64urlEncode(new Uint8Array(publicKey.challenge)), rpId: publicKey.rpId });
      return {
        id: json.id,
        rawId: buffer(json.rawId!),
        type: "public-key",
        getClientExtensionResults: () => ({}),
        response: { clientDataJSON: buffer(json.response.clientDataJSON), authenticatorData: buffer(json.response.authenticatorData), signature: buffer(json.response.signature), userHandle: null },
      };
    },
  };
  vi.stubGlobal("navigator", { credentials: credentialsStub });
  vi.stubGlobal("PublicKeyCredential", class {});
}

beforeEach(() => {
  resetRateLimits();
  navigated.length = 0;
});
afterEach(() => vi.unstubAllGlobals());

function base(extra: AuthOptions = {}) {
  const adapter = MemoryAdapter();
  const c = makeClient({
    adapter,
    session: { strategy: "jwt" },
    twoFactor: { enabled: true },
    providers: [
      credentials({ authorize: async (creds) => (creds.password === "pw" ? adapter.getUserByEmail(creds.email ?? "") : null) }),
      genericOAuth({ id: "idp", clientId: "c", clientSecret: "s", authorization: "https://idp.test/authorize", token: "https://idp.test/token", userinfo: "https://idp.test/me" }),
      passkey(),
    ],
    ...extra,
  });
  wire(c);
  return { c, adapter };
}

describe("session helpers", () => {
  it("reads the csrf token, providers and session", async () => {
    base();
    expect(await getCsrfToken()).toHaveLength(43);
    const providers = await getProviders();
    expect(Object.keys(providers).sort()).toEqual(["credentials", "idp", "passkey"]);
    expect(await getSession()).toBeNull();
  });
});

describe("signIn and signOut", () => {
  it("signs in with credentials and navigates", async () => {
    const { adapter } = base();
    await adapter.createUser({ email: "ada@example.com", name: "Ada", emailVerified: null });
    const result = await signIn("credentials", { email: "ada@example.com", password: "pw", callbackUrl: "/dash" });
    expect(result).toMatchObject({ ok: true, error: null, url: `${ORIGIN}/dash`, twoFactorRequired: false });
    expect(navigated).toEqual([`${ORIGIN}/dash`]);
    expect((await getSession()).user.email).toBe("ada@example.com");
  });

  it("reports credential errors without navigating", async () => {
    const { adapter } = base();
    await adapter.createUser({ email: "ada@example.com", emailVerified: null });
    const result = await signIn("credentials", { email: "ada@example.com", password: "wrong" });
    expect(result.ok).toBe(false);
    expect(result.error).toBe("CredentialsSignin");
    expect(navigated).toEqual([]);
    expect(await getSession()).toBeNull();
  });

  it("can skip the redirect", async () => {
    const { adapter } = base();
    await adapter.createUser({ email: "ada@example.com", emailVerified: null });
    const result = await signIn("credentials", { email: "ada@example.com", password: "pw", redirect: false });
    expect(result.ok).toBe(true);
    expect(navigated).toEqual([]);
  });

  it("starts an oauth flow by navigating to the provider", async () => {
    base();
    const result = await signIn("idp", { callbackUrl: "/after" });
    expect(result.ok).toBe(true);
    const target = new URL(navigated[0]!);
    expect(target.origin + target.pathname).toBe("https://idp.test/authorize");
    expect(target.searchParams.get("redirect_uri")).toBe(`${ORIGIN}/api/auth/callback/idp`);
  });

  it("falls back to the sign in page for unknown providers", async () => {
    base();
    const result = await signIn("nope");
    expect(result).toMatchObject({ ok: false, error: "UnknownProvider" });
    expect(navigated).toEqual(["/api/auth/signin"]);
  });

  it("signs out and updates the session", async () => {
    const { adapter } = base({ callbacks: { jwt: ({ token, trigger, session }) => (trigger === "update" ? { ...token, nick: (session as any).nick } : token), session: ({ session, token }) => ({ ...session, nick: token?.nick }) } });
    await adapter.createUser({ email: "ada@example.com", emailVerified: null });
    await signIn("credentials", { email: "ada@example.com", password: "pw", redirect: false });
    expect((await updateSession({ nick: "countess" }))?.nick).toBe("countess");
    const out = await signOut({ callbackUrl: "/bye" });
    expect(out.url).toBe(`${ORIGIN}/bye`);
    expect(navigated.at(-1)).toBe(`${ORIGIN}/bye`);
    expect(await getSession()).toBeNull();
  });
});

describe("two-factor client", () => {
  it("enrolls, challenges and verifies through the client", async () => {
    const { adapter, c } = base();
    await adapter.createUser({ email: "ada@example.com", emailVerified: null });
    await signIn("credentials", { email: "ada@example.com", password: "pw", redirect: false });
    expect((await twoFactor.status()).data).toMatchObject({ enabled: false });
    const setup = await twoFactor.setup();
    expect(setup.ok).toBe(true);
    expect((await twoFactor.enable("000000")).error).toBe("InvalidCode");
    const enabled = await twoFactor.enable(totp(setup.data.secret, { digits: 6, period: 30, algorithm: "SHA1" }));
    expect(enabled.ok).toBe(true);
    expect(enabled.data.backupCodes).toHaveLength(10);
    expect((await twoFactor.status()).data).toMatchObject({ enabled: true, backupCodesRemaining: 10 });

    await signOut({ redirect: false });
    c.jar.clear();
    const attempt = await signIn("credentials", { email: "ada@example.com", password: "pw", callbackUrl: "/dash" });
    expect(attempt.twoFactorRequired).toBe(true);
    expect(navigated.at(-1)).toContain("/api/auth/2fa?callbackUrl=");
    expect(await getSession()).toBeNull();

    expect((await twoFactor.verify("000000", { redirect: false })).ok).toBe(false);
    const verified = await twoFactor.verify(enabled.data.backupCodes[0]!);
    expect(verified.ok).toBe(true);
    expect(navigated.at(-1)).toBe(`${ORIGIN}/dash`);
    expect((await getSession()).user.twoFactorEnabled).toBe(true);

    const disabled = await twoFactor.disable(enabled.data.backupCodes[1]!);
    expect(disabled.ok).toBe(true);
    expect((await twoFactor.status()).data.enabled).toBe(false);
  });
});

describe("passkey client", () => {
  it("registers, lists, signs in and deletes with a platform authenticator", async () => {
    const { c } = base();
    const authn = new VirtualAuthenticator();
    installAuthenticator(authn);
    expect(isPasskeySupported()).toBe(true);

    const registered = await registerPasskey({ email: "ada@example.com", name: "Ada", passkeyName: "Laptop", callbackUrl: "/welcome" });
    expect(registered).toMatchObject({ ok: true, url: `${ORIGIN}/welcome` });
    expect(navigated.at(-1)).toBe(`${ORIGIN}/welcome`);
    expect((await getSession()).user.email).toBe("ada@example.com");
    const listed = await listPasskeys();
    expect(listed).toHaveLength(1);
    expect(listed[0]).toMatchObject({ name: "Laptop", deviceType: "singleDevice", transports: ["internal"] });

    c.jar.clear();
    expect(await getSession()).toBeNull();
    const signedIn = await signInWithPasskey({ callbackUrl: "/home" });
    expect(signedIn).toMatchObject({ ok: true, url: `${ORIGIN}/home` });
    expect((await getSession()).user.email).toBe("ada@example.com");

    expect(await deletePasskey(listed[0]!.credentialID)).toBe(true);
    expect(await listPasskeys()).toEqual([]);
  });

  it("reports a cancelled ceremony", async () => {
    base();
    installAuthenticator(new VirtualAuthenticator(), { cancel: true });
    expect(await registerPasskey({ email: "x@example.com" })).toEqual({ ok: false, error: "Cancelled" });
    expect(await signInWithPasskey()).toEqual({ ok: false, error: "Cancelled" });
  });

  it("surfaces server errors from the options step", async () => {
    const { adapter } = base();
    await adapter.createUser({ email: "taken@example.com", emailVerified: null });
    installAuthenticator(new VirtualAuthenticator());
    expect(await registerPasskey({ email: "taken@example.com" })).toEqual({ ok: false, error: "EmailTaken" });
  });

  it("detects missing browser support", () => {
    vi.stubGlobal("navigator", {});
    expect(isPasskeySupported()).toBe(false);
  });
});
