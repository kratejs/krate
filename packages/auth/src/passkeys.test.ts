import { beforeEach, describe, expect, it } from "vitest";
import { MemoryAdapter } from "./adapters/memory";
import { b64urlDecode, b64urlEncode } from "./bytes";
import { encodeCbor } from "./cbor";
import { credentials } from "./providers/credentials";
import { passkey } from "./providers/passkey";
import { resetRateLimits } from "./ratelimit";
import { Client, ORIGIN, makeClient } from "./test-utils";
import { VirtualAuthenticator, type Alg, type Tamper } from "./test-authenticator";
import { totp } from "./totp";
import type { AuthOptions } from "./types";

beforeEach(() => resetRateLimits());

function setup(extra: AuthOptions = {}) {
  const adapter = MemoryAdapter();
  const c = makeClient({ providers: [passkey()], adapter, session: { strategy: "jwt" }, ...extra });
  return { c, adapter };
}

async function jpost(c: Client, path: string, body: Record<string, unknown> = {}): Promise<Response> {
  const csrfToken = await c.csrf();
  return c.request(path, { method: "POST", json: { ...body, csrfToken } });
}

async function register(c: Client, authn: VirtualAuthenticator, body: Record<string, unknown> = {}, tamper: Tamper = {}) {
  const optionsRes = await jpost(c, "/api/auth/passkey/register/options", body);
  const options = (await optionsRes.json()) as any;
  if (optionsRes.status !== 200) return { optionsRes, options, verifyRes: undefined as Response | undefined };
  const credential = authn.create(options, tamper);
  const verifyRes = await jpost(c, "/api/auth/passkey/register/verify", { credential, ...body });
  return { optionsRes, options, verifyRes };
}

async function login(c: Client, authn: VirtualAuthenticator, tamper: Tamper = {}, extra: Record<string, unknown> = {}) {
  const options = (await (await jpost(c, "/api/auth/passkey/authenticate/options")).json()) as any;
  const credential = authn.get(options, tamper);
  return { options, credential, res: await jpost(c, "/api/auth/passkey/authenticate/verify", { credential, ...extra }) };
}

describe("registration", () => {
  it("creates an account with a passkey and signs the user in", async () => {
    const { c, adapter } = setup();
    const authn = new VirtualAuthenticator({ multiDevice: true, backedUp: true });
    const { options, optionsRes, verifyRes } = await register(c, authn, { email: "Ada@Example.com", name: "Ada" });
    expect(optionsRes.status).toBe(200);
    expect(options.rp).toEqual({ id: "localhost", name: "Krate" });
    expect(options.challenge).toHaveLength(43);
    expect(options.pubKeyCredParams.map((p: any) => p.alg)).toEqual([-7, -8, -257]);
    expect(options.authenticatorSelection).toMatchObject({ residentKey: "required", userVerification: "preferred" });
    expect(options.excludeCredentials).toEqual([]);

    expect(verifyRes!.status).toBe(200);
    expect(((await verifyRes!.json()) as any).url).toBe(`${ORIGIN}/`);
    const user = await adapter.getUserByEmail("ada@example.com");
    expect(user?.name).toBe("Ada");
    const stored = await adapter.getAuthenticator!(authn.id);
    expect(stored).toMatchObject({ userId: user!.id, counter: 0, credentialDeviceType: "multiDevice", credentialBackedUp: true, transports: "internal,hybrid" });
    expect(await adapter.getUserByAccount({ provider: "passkey", providerAccountId: authn.id })).toMatchObject({ id: user!.id });
    expect((await c.session()).user).toMatchObject({ id: user!.id, email: "ada@example.com" });
  });

  it("works without an email", async () => {
    const { c, adapter } = setup();
    const { verifyRes } = await register(c, new VirtualAuthenticator(), { name: "Anon" });
    expect(verifyRes!.status).toBe(200);
    expect(adapter.store.users.size).toBe(1);
  });

  for (const alg of [-7, -8, -257] as Alg[]) {
    it(`registers and authenticates with algorithm ${alg}`, async () => {
      const { c, adapter } = setup();
      const authn = new VirtualAuthenticator({ alg });
      expect((await register(c, authn, { email: `alg${Math.abs(alg)}@example.com` })).verifyRes!.status).toBe(200);
      const fresh = makeClient({ providers: [passkey()], adapter, session: { strategy: "jwt" } });
      const { res } = await login(fresh, authn);
      expect(res.status).toBe(200);
      expect((await fresh.session()).user.email).toBe(`alg${Math.abs(alg)}@example.com`);
      expect((await adapter.getAuthenticator!(authn.id))?.counter).toBe(1);
    });
  }

  it("adds a passkey to the signed in user and excludes existing ones", async () => {
    const { c, adapter } = setup({
      providers: [credentials({ authorize: async (creds) => adapter.getUserByEmail(creds.email ?? "") }), passkey()],
    });
    const user = await adapter.createUser({ email: "owner@example.com", name: "Owner", emailVerified: null });
    await c.post("/api/auth/callback/credentials", { email: "owner@example.com", password: "x" });
    const first = new VirtualAuthenticator();
    const r1 = await register(c, first, { passkeyName: "Laptop" });
    expect(r1.verifyRes!.status).toBe(200);
    expect(((await r1.verifyRes!.json()) as any).credentialID).toBe(first.id);
    expect(adapter.store.users.size).toBe(1);

    const second = new VirtualAuthenticator();
    const r2 = await register(c, second, { passkeyName: "Phone" });
    expect(r2.options.excludeCredentials.map((e: any) => e.id)).toEqual([first.id]);
    expect(r2.options.user.name).toBe("owner@example.com");

    const listed = (await (await c.request("/api/auth/passkey/list")).json()) as any;
    expect(listed.passkeys.map((p: any) => p.name).sort()).toEqual(["Laptop", "Phone"]);
    expect((await adapter.listAuthenticatorsByUserId!(user.id)).length).toBe(2);
  });

  it("rejects an email that already has an account", async () => {
    const { c, adapter } = setup();
    await adapter.createUser({ email: "taken@example.com", emailVerified: null });
    const { optionsRes, options } = await register(c, new VirtualAuthenticator(), { email: "taken@example.com" });
    expect(optionsRes.status).toBe(409);
    expect(options.error).toBe("EmailTaken");
    expect((await register(c, new VirtualAuthenticator(), { email: "bad" })).optionsRes.status).toBe(400);
  });

  it("can disable sign up", async () => {
    const { c } = setup({ passkeys: { allowSignUp: false } });
    expect((await register(c, new VirtualAuthenticator(), { email: "x@example.com" })).optionsRes.status).toBe(403);
  });

  const attacks: Array<[string, Tamper]> = [
    ["a wrong origin", { origin: "https://evil.example" }],
    ["a wrong relying party", { rpId: "evil.example" }],
    ["a wrong challenge", { challenge: "A".repeat(43) }],
    ["the wrong ceremony type", { type: "webauthn.get" }],
    ["a mismatched credential id", { idOverride: b64urlEncode(new Uint8Array(32).fill(7)) }],
    ["a non-empty none statement", { attStmt: new Map([["x", 1]]) }],
    ["missing attested data", { noAttested: true }],
  ];
  for (const [label, tamper] of attacks) {
    it(`rejects ${label}`, async () => {
      const { c, adapter } = setup();
      const { verifyRes } = await register(c, new VirtualAuthenticator(), { email: "a@example.com" }, tamper);
      expect(verifyRes!.status).toBeGreaterThanOrEqual(400);
      expect(adapter.store.users.size).toBe(0);
      expect(adapter.store.authenticators.size).toBe(0);
      expect(await c.session()).toBeNull();
    });
  }

  it("rejects unsupported key algorithms", async () => {
    const { c, adapter } = setup();
    const bad = encodeCbor(new Map<any, any>([[1, 2], [3, -36], [-1, 2], [-2, new Uint8Array(32)], [-3, new Uint8Array(32)]]));
    const { verifyRes } = await register(c, new VirtualAuthenticator(), {}, { coseOverride: bad });
    expect(verifyRes!.status).toBe(401);
    expect(adapter.store.authenticators.size).toBe(0);
  });

  it("requires user verification when configured", async () => {
    const { c, adapter } = setup({ passkeys: { userVerification: "required" } });
    const { verifyRes } = await register(c, new VirtualAuthenticator({ uv: false }));
    expect(verifyRes!.status).toBe(401);
    expect(adapter.store.authenticators.size).toBe(0);
    const ok = await register(makeClient({ providers: [passkey()], adapter, passkeys: { userVerification: "required" } }), new VirtualAuthenticator({ uv: true }));
    expect(ok.verifyRes!.status).toBe(200);
  });

  it("accepts a configured extra origin and rp id", async () => {
    const { c } = setup({ passkeys: { rpId: "example.com", origins: ["https://app.example.com"], rpName: "Example" } });
    const authn = new VirtualAuthenticator({ rpId: "example.com", origin: "https://app.example.com" });
    const { options, verifyRes } = await register(c, authn);
    expect(options.rp).toEqual({ id: "example.com", name: "Example" });
    expect(verifyRes!.status).toBe(200);
  });

  it("cannot reuse a challenge or register the same credential twice", async () => {
    const { c, adapter } = setup();
    const authn = new VirtualAuthenticator();
    const options = (await (await jpost(c, "/api/auth/passkey/register/options", { email: "a@example.com" })).json()) as any;
    const challengeCookie = c.jar.get("krate-auth.webauthn-challenge")!;
    const credential = authn.create(options);
    expect((await jpost(c, "/api/auth/passkey/register/verify", { credential })).status).toBe(200);
    expect(c.jar.has("krate-auth.webauthn-challenge")).toBe(false);

    c.jar.set("krate-auth.webauthn-challenge", challengeCookie);
    const replay = await jpost(c, "/api/auth/passkey/register/verify", { credential });
    expect(replay.status).toBe(400);
    expect(((await replay.json()) as any).error).toBe("ChallengeExpired");
    expect(adapter.store.users.size).toBe(1);

    c.jar.delete("krate-auth.webauthn-challenge");
    const noCookie = await jpost(c, "/api/auth/passkey/register/verify", { credential });
    expect(noCookie.status).toBe(400);
    expect(((await noCookie.json()) as any).error).toBe("ChallengeExpired");
  });

  it("requires a csrf token", async () => {
    const { c } = setup();
    const res = await c.request("/api/auth/passkey/register/options", { method: "POST", json: { csrfToken: "nope" } });
    expect(res.status).toBe(403);
  });
});

describe("authentication", () => {
  async function registered(extra: AuthOptions = {}, authnOptions = {}) {
    const { c, adapter } = setup(extra);
    const authn = new VirtualAuthenticator(authnOptions);
    await register(c, authn, { email: "ada@example.com" });
    const fresh = () => makeClient({ providers: [passkey()], adapter, session: { strategy: "jwt" }, ...extra });
    return { adapter, authn, fresh };
  }

  it("signs in with a discoverable credential and bumps the counter", async () => {
    const { fresh, authn, adapter } = await registered();
    const c = fresh();
    const { options, res } = await login(c, authn, {}, { callbackUrl: "/home" });
    expect(options.allowCredentials).toEqual([]);
    expect(options.rpId).toBe("localhost");
    expect(res.status).toBe(200);
    expect(((await res.json()) as any).url).toBe(`${ORIGIN}/home`);
    expect((await c.session()).user.email).toBe("ada@example.com");
    expect((await adapter.getAuthenticator!(authn.id))?.counter).toBe(1);
    const { res: second } = await login(fresh(), authn);
    expect(second.status).toBe(200);
    expect((await adapter.getAuthenticator!(authn.id))?.counter).toBe(2);
  });

  it("rejects an unknown credential", async () => {
    const { fresh } = await registered();
    const stranger = new VirtualAuthenticator();
    const c = fresh();
    const { res } = await login(c, stranger);
    expect(res.status).toBe(401);
    expect(await c.session()).toBeNull();
  });

  const attacks: Array<[string, Tamper]> = [
    ["a wrong origin", { origin: "https://evil.example" }],
    ["a wrong relying party", { rpId: "evil.example" }],
    ["a wrong challenge", { challenge: "B".repeat(43) }],
    ["the wrong ceremony type", { type: "webauthn.create" }],
  ];
  for (const [label, tamper] of attacks) {
    it(`rejects ${label}`, async () => {
      const { fresh, authn } = await registered();
      const c = fresh();
      const { res } = await login(c, authn, tamper);
      expect(res.status).toBe(401);
      expect(await c.session()).toBeNull();
    });
  }

  it("rejects a signature made with another key", async () => {
    const { fresh, authn } = await registered();
    const attacker = new VirtualAuthenticator();
    const c = fresh();
    const { res } = await login(c, authn, { signWith: attacker });
    expect(res.status).toBe(401);
    expect(await c.session()).toBeNull();
  });

  it("detects a replayed assertion through the signature counter", async () => {
    const { fresh, authn, adapter } = await registered();
    const first = await login(fresh(), authn);
    expect(first.res.status).toBe(200);
    const replay = fresh();
    const options = (await (await jpost(replay, "/api/auth/passkey/authenticate/options")).json()) as any;
    const stale = authn.get(options, { counter: 1 });
    const res = await jpost(replay, "/api/auth/passkey/authenticate/verify", { credential: stale });
    expect(res.status).toBe(401);
    expect(await replay.session()).toBeNull();
    expect((await adapter.getAuthenticator!(authn.id))?.counter).toBe(1);
  });

  it("allows authenticators that never increment their counter, but never reuses a challenge", async () => {
    const { fresh, authn } = await registered();
    for (let i = 0; i < 2; i++) {
      const c = fresh();
      const options = (await (await jpost(c, "/api/auth/passkey/authenticate/options")).json()) as any;
      const res = await jpost(c, "/api/auth/passkey/authenticate/verify", { credential: authn.get(options, { counter: 0 }) });
      expect(res.status).toBe(200);
    }
  });

  it("consumes the challenge so an assertion cannot be replayed", async () => {
    const { fresh, authn } = await registered();
    const c = fresh();
    const options = (await (await jpost(c, "/api/auth/passkey/authenticate/options")).json()) as any;
    const challengeCookie = c.jar.get("krate-auth.webauthn-challenge")!;
    const credential = authn.get(options);
    expect((await jpost(c, "/api/auth/passkey/authenticate/verify", { credential })).status).toBe(200);
    const other = fresh();
    other.jar.set("krate-auth.webauthn-challenge", challengeCookie);
    const replayed = await jpost(other, "/api/auth/passkey/authenticate/verify", { credential });
    expect(replayed.status).toBe(400);
    expect(((await replayed.json()) as any).error).toBe("ChallengeExpired");
    expect(other.jar.has("krate-auth.session-token")).toBe(false);
    const bare = fresh();
    expect((await jpost(bare, "/api/auth/passkey/authenticate/verify", { credential })).status).toBe(400);
  });

  it("enforces user verification when required", async () => {
    const { c, adapter } = setup({ passkeys: { userVerification: "required" } });
    const authn = new VirtualAuthenticator({ uv: true });
    await register(c, authn);
    const fresh = makeClient({ providers: [passkey()], adapter, passkeys: { userVerification: "required" } });
    const { res } = await login(fresh, authn, { uv: false });
    expect(res.status).toBe(401);
  });

  it("keeps challenge cookies from being swapped between ceremonies", async () => {
    const { fresh, authn } = await registered();
    const c = fresh();
    await jpost(c, "/api/auth/passkey/register/options", { email: "other@example.com" });
    const options = { challenge: "irrelevant", rpId: "localhost" };
    const res = await jpost(c, "/api/auth/passkey/authenticate/verify", { credential: authn.get(options) });
    expect(res.status).toBe(400);
  });

  it("works with database sessions", async () => {
    const { c, adapter } = setup({ session: { strategy: "database" } });
    const authn = new VirtualAuthenticator();
    await register(c, authn, { email: "db@example.com" });
    expect(adapter.store.sessions.size).toBe(1);
    const fresh = makeClient({ providers: [passkey()], adapter, session: { strategy: "database" } });
    expect((await login(fresh, authn)).res.status).toBe(200);
    expect((await fresh.session()).user.email).toBe("db@example.com");
  });

  it("fires the passkeyRegistered event", async () => {
    const seen: string[] = [];
    const { c } = setup({ events: { passkeyRegistered: ({ credentialID }) => void seen.push(credentialID) } });
    const authn = new VirtualAuthenticator();
    await register(c, authn);
    expect(seen).toEqual([authn.id]);
  });
});

describe("management", () => {
  it("deletes only the owner's passkeys", async () => {
    const adapter = MemoryAdapter();
    const provider = credentials({ authorize: async (creds) => adapter.getUserByEmail(creds.email ?? "") });
    const options: AuthOptions = { providers: [provider, passkey()], adapter, session: { strategy: "jwt" } };
    const alice = makeClient(options);
    const bob = makeClient(options);
    await adapter.createUser({ email: "alice@example.com", emailVerified: null });
    await adapter.createUser({ email: "bob@example.com", emailVerified: null });
    await alice.post("/api/auth/callback/credentials", { email: "alice@example.com", password: "x" });
    await bob.post("/api/auth/callback/credentials", { email: "bob@example.com", password: "x" });
    const aliceKey = new VirtualAuthenticator();
    await register(alice, aliceKey);
    const denied = await jpost(bob, "/api/auth/passkey/delete", { credentialID: aliceKey.id });
    expect(denied.status).toBe(404);
    expect(await adapter.getAuthenticator!(aliceKey.id)).not.toBeNull();
    const removed = await jpost(alice, "/api/auth/passkey/delete", { credentialID: aliceKey.id });
    expect(removed.status).toBe(200);
    expect(await adapter.getAuthenticator!(aliceKey.id)).toBeNull();
    expect((await alice.request("/api/auth/passkey/list")).status).toBe(200);
    expect((await makeClient(options).request("/api/auth/passkey/list")).status).toBe(401);
  });

  it("is unavailable when not enabled and needs an adapter", async () => {
    const off = makeClient({ providers: [], adapter: MemoryAdapter() });
    expect((await jpost(off, "/api/auth/passkey/authenticate/options")).status).toBe(404);
    const noAdapter = makeClient({ providers: [passkey()] });
    expect((await jpost(noAdapter, "/api/auth/passkey/authenticate/options")).status).toBe(500);
  });

  it("is enabled by the passkey provider or the config flag", async () => {
    expect(makeClient({ providers: [passkey()], adapter: MemoryAdapter() }).auth.config().passkeys.enabled).toBe(true);
    expect(makeClient({ providers: [], adapter: MemoryAdapter(), passkeys: { enabled: true } }).auth.config().passkeys.enabled).toBe(true);
    expect(makeClient({ providers: [], adapter: MemoryAdapter() }).auth.config().passkeys.enabled).toBe(false);
  });
});

describe("with two-factor authentication", () => {
  async function enrolled() {
    const adapter = MemoryAdapter();
    const base: AuthOptions = { providers: [passkey()], adapter, session: { strategy: "jwt" }, twoFactor: { enabled: true } };
    const c = makeClient(base);
    const authn = new VirtualAuthenticator({ uv: true });
    await register(c, authn, { email: "ada@example.com" });
    const user = (await adapter.getUserByEmail("ada@example.com"))!;
    const setup2fa = (await (await jpost(c, "/api/auth/2fa/setup")).json()) as { secret: string };
    expect((await jpost(c, "/api/auth/2fa/enable", { code: totp(setup2fa.secret, { digits: 6, period: 30, algorithm: "SHA1" }) })).status).toBe(200);
    return { base, authn, user };
  }

  it("treats a user-verified passkey as a second factor", async () => {
    const { base, authn } = await enrolled();
    const c = makeClient(base);
    const { res } = await login(c, authn, { uv: true });
    expect(res.status).toBe(200);
    expect(c.jar.has("krate-auth.session-token")).toBe(true);
    expect(c.jar.has("krate-auth.pending-2fa")).toBe(false);
  });

  it("still asks for a code when the passkey did not verify the user", async () => {
    const { base, authn } = await enrolled();
    const c = makeClient(base);
    const { res } = await login(c, authn, { uv: false });
    expect(res.status).toBe(200);
    expect(((await res.json()) as any).twoFactorRequired).toBe(true);
    expect(c.jar.has("krate-auth.session-token")).toBe(false);
    expect(c.jar.has("krate-auth.pending-2fa")).toBe(true);
  });
});

describe("encoding", () => {
  it("serves base64url ids the browser can decode", async () => {
    const { c } = setup();
    const options = (await (await jpost(c, "/api/auth/passkey/register/options", { email: "a@example.com" })).json()) as any;
    expect(b64urlDecode(options.challenge)).toHaveLength(32);
    expect(new TextDecoder().decode(b64urlDecode(options.user.id))).toMatch(/^[0-9a-f-]{36}$/);
  });
});
