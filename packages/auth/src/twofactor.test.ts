import { beforeEach, describe, expect, it } from "vitest";
import { MemoryAdapter } from "./adapters/memory";
import { credentials } from "./providers/credentials";
import { resetRateLimits } from "./ratelimit";
import { ORIGIN, makeClient, type Client } from "./test-utils";
import { totp } from "./totp";
import type { AuthOptions } from "./types";

beforeEach(() => resetRateLimits());

const opts = { digits: 6, period: 30, algorithm: "SHA1" as const };

function setup(extra: AuthOptions = {}) {
  const adapter = MemoryAdapter();
  const provider = credentials({
    async authorize(creds) {
      const user = await adapter.getUserByEmail(creds.email ?? "");
      return user && creds.password === "pw" ? user : null;
    },
  });
  const c = makeClient({ providers: [provider], adapter, session: { strategy: "jwt" }, twoFactor: { enabled: true, issuer: "Krate Test" }, ...extra });
  return { c, adapter };
}

async function signIn(c: Client, email = "ada@example.com"): Promise<Response> {
  return c.post("/api/auth/callback/credentials", { email, password: "pw", callbackUrl: "/dash" });
}

async function enroll(c: Client, adapter: ReturnType<typeof MemoryAdapter>) {
  const user = await adapter.createUser({ email: "ada@example.com", name: "Ada", emailVerified: null });
  await signIn(c);
  const setupRes = await c.post("/api/auth/2fa/setup", {}, { json: true });
  const info = (await setupRes.json()) as { secret: string; otpauthUrl: string };
  const code = totp(info.secret, opts);
  const enable = await c.post("/api/auth/2fa/enable", { code }, { json: true });
  const body = (await enable.json()) as { enabled: boolean; backupCodes: string[] };
  return { user, secret: info.secret, backupCodes: body.backupCodes, enable, info, lastCode: code };
}

describe("enrollment", () => {
  it("returns a secret and otpauth url, then enables with a valid code", async () => {
    const { c, adapter } = setup();
    const { info, enable, backupCodes, user } = await enroll(c, adapter);
    expect(info.secret).toMatch(/^[A-Z2-7]{32}$/);
    expect(info.otpauthUrl).toContain("otpauth://totp/Krate%20Test:ada%40example.com");
    expect(enable.status).toBe(200);
    expect(backupCodes).toHaveLength(10);
    const record = await adapter.getTwoFactor!(user.id);
    expect(record?.confirmed).toBe(true);
    expect(record?.secret).not.toContain(info.secret);
    expect(record?.backupCodes.some((h) => backupCodes.some((p) => h.includes(p)))).toBe(false);
    expect((await adapter.getUser(user.id))?.twoFactorEnabled).toBe(true);
    expect((await c.session()).user.twoFactorEnabled).toBe(true);
  });

  it("requires a session and a csrf token", async () => {
    const { c } = setup();
    const anon = await c.post("/api/auth/2fa/setup", {}, { json: true });
    expect(anon.status).toBe(401);
    const forged = await c.request("/api/auth/2fa/setup", { method: "POST", json: { csrfToken: "x" } });
    expect(forged.status).toBe(403);
  });

  it("rejects a wrong code and does not enable", async () => {
    const { c, adapter } = setup();
    await adapter.createUser({ email: "ada@example.com", emailVerified: null });
    await signIn(c);
    await c.post("/api/auth/2fa/setup", {}, { json: true });
    const bad = await c.post("/api/auth/2fa/enable", { code: "000000" }, { json: true });
    expect(bad.status).toBe(401);
    expect(((await bad.json()) as any).error).toBe("InvalidCode");
    const status = await (await c.request("/api/auth/2fa/status")).json();
    expect(status).toMatchObject({ enabled: false, pending: true });
  });

  it("cannot enable before setup or twice", async () => {
    const { c, adapter } = setup();
    await adapter.createUser({ email: "ada@example.com", emailVerified: null });
    await signIn(c);
    expect((await c.post("/api/auth/2fa/enable", { code: "123456" }, { json: true })).status).toBe(409);
    const { c: c2, adapter: a2 } = setup();
    await enroll(c2, a2);
    expect((await c2.post("/api/auth/2fa/setup", {}, { json: true })).status).toBe(409);
  });

  it("is unavailable when disabled", async () => {
    const { c, adapter } = setup({ twoFactor: { enabled: false } });
    await adapter.createUser({ email: "ada@example.com", emailVerified: null });
    await signIn(c);
    expect((await c.post("/api/auth/2fa/setup", {}, { json: true })).status).toBe(404);
  });

  it("needs an adapter with two-factor storage", async () => {
    const c = makeClient({ providers: [credentials({ authorize: () => ({ id: "u1", email: "a@x.io" }) })], twoFactor: { enabled: true } });
    await c.post("/api/auth/callback/credentials", { email: "a@x.io", password: "x" });
    const res = await c.post("/api/auth/2fa/setup", {}, { json: true });
    expect(res.status).toBe(500);
  });
});

describe("sign in with a second factor", () => {
  async function enrolledUser() {
    const { c, adapter } = setup();
    const info = await enroll(c, adapter);
    const fresh = makeClient({ providers: c.auth.config().providers, adapter, session: { strategy: "jwt" }, twoFactor: { enabled: true } });
    return { ...info, fresh, adapter };
  }

  it("holds the session back until a valid code is entered", async () => {
    const { fresh, secret } = await enrolledUser();
    const res = await signIn(fresh);
    expect(res.headers.get("location")).toContain("/api/auth/2fa?callbackUrl=");
    expect(fresh.jar.has("krate-auth.session-token")).toBe(false);
    expect(fresh.jar.has("krate-auth.pending-2fa")).toBe(true);
    expect(await fresh.session()).toBeNull();

    const step = Date.now() + 30_000;
    const code = totp(secret, opts, step);
    const original = Date.now;
    Date.now = () => step;
    try {
      const done = await fresh.post("/api/auth/2fa/verify", { code, callbackUrl: "/dash" });
      expect(done.status).toBe(302);
      expect(done.headers.get("location")).toBe(`${ORIGIN}/dash`);
    } finally {
      Date.now = original;
    }
    expect(fresh.jar.has("krate-auth.pending-2fa")).toBe(false);
    expect((await fresh.session()).user).toMatchObject({ email: "ada@example.com", twoFactorEnabled: true });
  });

  it("rejects a wrong code, keeps the challenge and never issues a session", async () => {
    const { fresh } = await enrolledUser();
    await signIn(fresh);
    const res = await fresh.post("/api/auth/2fa/verify", { code: "111111" });
    expect(res.headers.get("location")).toContain("error=InvalidCode");
    expect(fresh.jar.has("krate-auth.pending-2fa")).toBe(true);
    expect(await fresh.session()).toBeNull();
  });

  it("refuses to verify without a pending sign in", async () => {
    const { fresh } = await enrolledUser();
    const res = await fresh.post("/api/auth/2fa/verify", { code: "123456" }, { json: true });
    expect(res.status).toBe(401);
  });

  it("does not accept a code that was already used", async () => {
    const { fresh, c, secret } = await enrolledUser();
    void c;
    const step = Date.now() + 60_000;
    const original = Date.now;
    Date.now = () => step;
    try {
      const code = totp(secret, opts, step);
      await signIn(fresh);
      const first = await fresh.post("/api/auth/2fa/verify", { code });
      expect(first.status).toBe(302);
      expect(first.headers.get("location")).toBe(`${ORIGIN}/dash`);
      const second = makeClient({ providers: fresh.auth.config().providers, adapter: fresh.auth.config().adapter, session: { strategy: "jwt" }, twoFactor: { enabled: true } });
      await signIn(second);
      const replay = await second.post("/api/auth/2fa/verify", { code });
      expect(replay.headers.get("location")).toContain("error=InvalidCode");
      expect(await second.session()).toBeNull();
    } finally {
      Date.now = original;
    }
  });

  it("accepts a backup code exactly once", async () => {
    const { fresh, backupCodes, adapter, user } = await enrolledUser();
    await signIn(fresh);
    const ok = await fresh.post("/api/auth/2fa/verify", { code: backupCodes[0]!.toLowerCase() });
    expect(ok.status).toBe(302);
    expect((await fresh.session()).user.email).toBe("ada@example.com");
    expect((await adapter.getTwoFactor!(user.id))?.backupCodes).toHaveLength(9);

    const again = makeClient({ providers: fresh.auth.config().providers, adapter, session: { strategy: "jwt" }, twoFactor: { enabled: true } });
    await signIn(again);
    const replay = await again.post("/api/auth/2fa/verify", { code: backupCodes[0]! });
    expect(replay.headers.get("location")).toContain("error=InvalidCode");
  });

  it("locks out brute force attempts", async () => {
    const { fresh } = await enrolledUser();
    await signIn(fresh);
    let last: Response | undefined;
    for (let i = 0; i < 8; i++) last = await fresh.post("/api/auth/2fa/verify", { code: "000000" }, { json: true });
    expect(last!.status).toBe(429);
    expect(((await last!.json()) as any).error).toBe("RateLimited");
    expect(await fresh.session()).toBeNull();
  });

  it("skips the challenge for users without 2fa", async () => {
    const { c, adapter } = setup();
    await adapter.createUser({ email: "plain@example.com", emailVerified: null });
    const res = await signIn(c, "plain@example.com");
    expect(res.headers.get("location")).toBe(`${ORIGIN}/dash`);
    expect((await c.session()).user.twoFactorEnabled).toBe(false);
  });
});

describe("management", () => {
  it("reports status and remaining backup codes", async () => {
    const { c, adapter } = setup();
    await enroll(c, adapter);
    expect(await (await c.request("/api/auth/2fa/status")).json()).toMatchObject({ enabled: true, pending: false, backupCodesRemaining: 10, required: false });
  });

  it("disables with a valid code and restores plain sign in", async () => {
    const { c, adapter } = setup();
    const { user, backupCodes } = await enroll(c, adapter);
    const bad = await c.post("/api/auth/2fa/disable", { code: "000000" }, { json: true });
    expect(bad.status).toBe(401);
    expect(await adapter.getTwoFactor!(user.id)).not.toBeNull();
    const ok = await c.post("/api/auth/2fa/disable", { code: backupCodes[0]! }, { json: true });
    expect(ok.status).toBe(200);
    expect(await adapter.getTwoFactor!(user.id)).toBeNull();
    expect((await adapter.getUser(user.id))?.twoFactorEnabled).toBe(false);
    expect((await c.session()).user.twoFactorEnabled).toBe(false);
    const plain = makeClient({ providers: c.auth.config().providers, adapter, session: { strategy: "jwt" }, twoFactor: { enabled: true } });
    expect((await signIn(plain)).headers.get("location")).toBe(`${ORIGIN}/dash`);
  });

  it("regenerates backup codes only with a totp code", async () => {
    const { c, adapter } = setup();
    const { user, secret, backupCodes } = await enroll(c, adapter);
    const viaBackup = await c.post("/api/auth/2fa/backup-codes", { code: backupCodes[0]! }, { json: true });
    expect(viaBackup.status).toBe(401);
    const step = Date.now() + 90_000;
    const original = Date.now;
    Date.now = () => step;
    try {
      const fresh = await c.post("/api/auth/2fa/backup-codes", { code: totp(secret, opts, step) }, { json: true });
      expect(fresh.status).toBe(200);
      const body = (await fresh.json()) as { backupCodes: string[] };
      expect(body.backupCodes).toHaveLength(10);
      expect(body.backupCodes).not.toContain(backupCodes[0]);
    } finally {
      Date.now = original;
    }
    expect((await adapter.getTwoFactor!(user.id))?.backupCodes).toHaveLength(10);
  });

  it("flags users who must enroll", async () => {
    const { c, adapter } = setup({ twoFactor: { enabled: true, required: true } });
    await adapter.createUser({ email: "ada@example.com", emailVerified: null });
    await signIn(c);
    expect((await c.session()).user).toMatchObject({ twoFactorEnabled: false, twoFactorSetupRequired: true });
  });

  it("fires lifecycle events", async () => {
    const seen: string[] = [];
    const { c, adapter } = setup({ events: { twoFactorEnabled: () => void seen.push("on"), twoFactorDisabled: () => void seen.push("off") } });
    const { backupCodes } = await enroll(c, adapter);
    await c.post("/api/auth/2fa/disable", { code: backupCodes[0]! }, { json: true });
    expect(seen).toEqual(["on", "off"]);
  });

  it("works with database sessions", async () => {
    const adapter = MemoryAdapter();
    const provider = credentials({ authorize: async (creds) => adapter.getUserByEmail(creds.email ?? "") });
    const c = makeClient({ providers: [provider], adapter, session: { strategy: "database" }, twoFactor: { enabled: true } });
    await adapter.createUser({ email: "ada@example.com", emailVerified: null });
    await signIn(c);
    const info = (await (await c.post("/api/auth/2fa/setup", {}, { json: true })).json()) as { secret: string };
    expect((await c.post("/api/auth/2fa/enable", { code: totp(info.secret, opts) }, { json: true })).status).toBe(200);
    expect((await c.session()).user.twoFactorEnabled).toBe(true);
  });
});
