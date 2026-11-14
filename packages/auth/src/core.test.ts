import { beforeEach, describe, expect, it } from "vitest";
import { credentials } from "./providers/credentials";
import { resetRateLimits } from "./ratelimit";
import { ORIGIN, makeClient } from "./test-utils";

const users: Record<string, { id: string; name: string; email: string; password: string; role?: string }> = {
  "ada@example.com": { id: "u1", name: "Ada", email: "ada@example.com", password: "correct horse", role: "admin" },
};

function passwordProvider() {
  return credentials({
    async authorize(creds) {
      const user = users[creds.email ?? ""];
      if (!user || user.password !== creds.password) return null;
      return { id: user.id, name: user.name, email: user.email, role: user.role };
    },
  });
}

beforeEach(() => resetRateLimits());

describe("session endpoints", () => {
  it("returns null without a session", async () => {
    const c = makeClient({ providers: [passwordProvider()] });
    const res = await c.request("/api/auth/session");
    expect(res.status).toBe(200);
    expect(await res.json()).toBeNull();
    expect(res.headers.get("cache-control")).toBe("no-store");
  });

  it("lists providers", async () => {
    const c = makeClient({ providers: [passwordProvider()] });
    const body = (await (await c.request("/api/auth/providers")).json()) as Record<string, any>;
    expect(body.credentials).toMatchObject({ id: "credentials", type: "credentials", callbackUrl: `${ORIGIN}/api/auth/callback/credentials` });
  });

  it("issues a csrf token bound to a cookie and reuses it", async () => {
    const c = makeClient({ providers: [passwordProvider()] });
    const first = await c.csrf();
    expect(first).toHaveLength(43);
    expect(await c.csrf()).toBe(first);
    expect([...c.jar.keys()]).toContain("krate-auth.csrf-token");
  });

  it("404s on unknown routes and other bases", async () => {
    const c = makeClient({ providers: [passwordProvider()] });
    expect((await c.request("/api/auth/nothing")).status).toBe(404);
    expect((await c.request("/other")).status).toBe(404);
  });

  it("fails with a clear error when no secret is configured", async () => {
    const prev = process.env.AUTH_SECRET;
    delete process.env.AUTH_SECRET;
    delete process.env.NEXTAUTH_SECRET;
    const c = makeClient({ secret: "", providers: [] });
    const res = await c.request("/api/auth/session");
    expect(res.status).toBe(500);
    expect(((await res.json()) as any).error).toBe("MissingSecret");
    if (prev) process.env.AUTH_SECRET = prev;
  });
});

describe("credentials sign in", () => {
  it("signs in, exposes the session and signs out", async () => {
    const c = makeClient({ providers: [passwordProvider()] });
    const res = await c.post("/api/auth/callback/credentials", { email: "ada@example.com", password: "correct horse", callbackUrl: "/dashboard" });
    expect(res.status).toBe(302);
    expect(res.headers.get("location")).toBe(`${ORIGIN}/dashboard`);
    expect(c.jar.has("krate-auth.session-token")).toBe(true);

    const session = await c.session();
    expect(session.user).toMatchObject({ id: "u1", name: "Ada", email: "ada@example.com", role: "admin" });
    expect(new Date(session.expires).getTime()).toBeGreaterThan(Date.now());

    const out = await c.post("/api/auth/signout", { callbackUrl: "/" });
    expect(out.status).toBe(302);
    expect(c.jar.has("krate-auth.session-token")).toBe(false);
    expect(await c.session()).toBeNull();
  });

  it("rejects bad credentials without a session", async () => {
    const c = makeClient({ providers: [passwordProvider()] });
    const res = await c.post("/api/auth/callback/credentials", { email: "ada@example.com", password: "nope" });
    expect(res.status).toBe(302);
    expect(res.headers.get("location")).toContain("error=CredentialsSignin");
    expect(c.jar.has("krate-auth.session-token")).toBe(false);
  });

  it("rejects a missing or forged csrf token", async () => {
    const c = makeClient({ providers: [passwordProvider()] });
    await c.csrf();
    const forged = await c.request("/api/auth/callback/credentials", { method: "POST", form: { email: "ada@example.com", password: "correct horse", csrfToken: "forged" } });
    expect(forged.headers.get("location")).toContain("error=MissingCSRF");
    const missing = await c.request("/api/auth/callback/credentials", { method: "POST", form: { email: "ada@example.com", password: "correct horse" } });
    expect(missing.headers.get("location")).toContain("error=MissingCSRF");
    expect(c.jar.has("krate-auth.session-token")).toBe(false);
  });

  it("rejects cross-origin posts", async () => {
    const c = makeClient({ providers: [passwordProvider()] });
    const res = await c.post("/api/auth/callback/credentials", { email: "ada@example.com", password: "correct horse" }, { headers: { origin: "https://evil.example" } });
    expect(res.headers.get("location")).toContain("error=InvalidOrigin");
    expect(c.jar.has("krate-auth.session-token")).toBe(false);
  });

  it("returns json when the client asks for it", async () => {
    const c = makeClient({ providers: [passwordProvider()] });
    const res = await c.post("/api/auth/callback/credentials", { email: "ada@example.com", password: "correct horse" }, { json: true, headers: { "x-auth-return-redirect": "1" } });
    expect(res.status).toBe(200);
    expect(await res.json()).toMatchObject({ url: `${ORIGIN}/` });
    const bad = await c.post("/api/auth/callback/credentials", { email: "ada@example.com", password: "x" }, { json: true, headers: { "x-auth-return-redirect": "1" } });
    expect(((await bad.json()) as any).error).toBe("CredentialsSignin");
  });

  it("refuses open redirects", async () => {
    for (const evil of ["https://evil.example/x", "//evil.example", "/\\evil.example", "javascript:alert(1)"]) {
      const c = makeClient({ providers: [passwordProvider()] });
      const res = await c.post("/api/auth/callback/credentials", { email: "ada@example.com", password: "correct horse", callbackUrl: evil });
      expect(res.headers.get("location")).toBe(`${ORIGIN}/`);
    }
  });

  it("allows configured redirect origins", async () => {
    const c = makeClient({ providers: [passwordProvider()], redirects: { allowed: ["https://app.example.com"] } });
    const res = await c.post("/api/auth/callback/credentials", { email: "ada@example.com", password: "correct horse", callbackUrl: "https://app.example.com/home" });
    expect(res.headers.get("location")).toBe("https://app.example.com/home");
  });

  it("rate limits repeated attempts", async () => {
    const c = makeClient({ providers: [passwordProvider()], rateLimit: { max: 3, window: 60 } });
    let last: Response | undefined;
    for (let i = 0; i < 5; i++) last = await c.post("/api/auth/callback/credentials", { email: "ada@example.com", password: "bad" });
    expect(last!.headers.get("location")).toContain("error=RateLimited");
  });

  it("honours signIn, jwt and session callbacks", async () => {
    const c = makeClient({
      providers: [passwordProvider()],
      callbacks: {
        signIn: ({ user }) => user.email !== "blocked@example.com",
        jwt: ({ token, user }) => (user ? { ...token, plan: "pro" } : token),
        session: ({ session, token }) => ({ ...session, plan: token?.plan }),
      },
    });
    await c.post("/api/auth/callback/credentials", { email: "ada@example.com", password: "correct horse" });
    expect(await c.session()).toMatchObject({ plan: "pro", user: { id: "u1" } });

    users["blocked@example.com"] = { id: "u2", name: "B", email: "blocked@example.com", password: "pw" };
    const d = makeClient({ providers: [passwordProvider()], callbacks: { signIn: ({ user }) => user.email !== "blocked@example.com" } });
    const denied = await d.post("/api/auth/callback/credentials", { email: "blocked@example.com", password: "pw" });
    expect(denied.headers.get("location")).toContain("error=AccessDenied");
    expect(d.jar.has("krate-auth.session-token")).toBe(false);
  });

  it("fires signIn and signOut events", async () => {
    const seen: string[] = [];
    const c = makeClient({
      providers: [passwordProvider()],
      events: { signIn: ({ user }) => void seen.push(`in:${user.id}`), signOut: ({ session }) => void seen.push(`out:${session?.user.id}`) },
    });
    await c.post("/api/auth/callback/credentials", { email: "ada@example.com", password: "correct horse" });
    await c.post("/api/auth/signout");
    expect(seen).toEqual(["in:u1", "out:u1"]);
  });

  it("rejects a tampered session cookie and another app's secret", async () => {
    const c = makeClient({ providers: [passwordProvider()] });
    await c.post("/api/auth/callback/credentials", { email: "ada@example.com", password: "correct horse" });
    const token = c.jar.get("krate-auth.session-token")!;
    c.jar.set("krate-auth.session-token", token.slice(0, -3) + "AAA");
    expect(await c.session()).toBeNull();

    const other = makeClient({ providers: [passwordProvider()], secret: "a-completely-different-secret-value-123" });
    other.jar.set("krate-auth.session-token", token);
    expect(await other.session()).toBeNull();
  });

  it("refreshes the session cookie after updateAge and supports session.update", async () => {
    const c = makeClient({
      providers: [passwordProvider()],
      session: { updateAge: 0 },
      callbacks: { jwt: ({ token, trigger, session }) => (trigger === "update" ? { ...token, nick: (session as any)?.nick } : token), session: ({ session, token }) => ({ ...session, nick: token?.nick }) },
    });
    await c.post("/api/auth/callback/credentials", { email: "ada@example.com", password: "correct horse" });
    const before = c.jar.get("krate-auth.session-token");
    const res = await c.request("/api/auth/session");
    expect(res.headers.getSetCookie().some((s) => s.startsWith("krate-auth.session-token="))).toBe(true);
    expect(c.jar.get("krate-auth.session-token")).not.toBe(before);
    const csrfToken = await c.csrf();
    const updated = await c.request("/api/auth/session", { method: "POST", json: { csrfToken, data: { nick: "countess" } } });
    expect(updated.status).toBe(200);
    expect(((await updated.json()) as any).nick).toBe("countess");
    expect((await c.session()).nick).toBe("countess");
  });
});

describe("pages", () => {
  it("renders an escaped sign in page with a csrf token", async () => {
    const c = makeClient({ providers: [passwordProvider()] });
    const res = await c.request("/api/auth/signin?callbackUrl=%2Fx%22%3E%3Cscript%3E&error=CredentialsSignin");
    const html = await res.text();
    expect(res.headers.get("content-type")).toContain("text/html");
    expect(res.headers.get("content-security-policy")).toContain("default-src 'none'");
    expect(html).toContain('name="csrfToken"');
    expect(html).toContain("Sign in failed");
    expect(html).not.toContain("<script>");
  });

  it("redirects to a custom sign in page", async () => {
    const c = makeClient({ providers: [passwordProvider()], pages: { signIn: "/login" } });
    const res = await c.request("/api/auth/signin?callbackUrl=%2Fhome");
    expect(res.headers.get("location")).toBe(`${ORIGIN}/login?callbackUrl=${encodeURIComponent(ORIGIN + "/home")}`);
  });

  it("renders the sign out and error pages", async () => {
    const c = makeClient({ providers: [passwordProvider()] });
    expect(await (await c.request("/api/auth/signout")).text()).toContain("Sign out");
    const err = await c.request("/api/auth/error?error=AccessDenied");
    expect(err.status).toBe(403);
    expect(await err.text()).toContain("permission");
  });
});

describe("declarative config", () => {
  it("merges the injected config under programmatic options", async () => {
    const c = makeClient(
      { providers: [passwordProvider()], pages: { error: "/oops" } },
      { basePath: "/api/auth", pages: { signIn: "/login", error: "/ignored" }, session: { maxAge: 120 }, redirects: { signedIn: "/home" } },
    );
    const cfg = c.auth.config();
    expect(cfg.pages).toEqual({ signIn: "/login", error: "/oops" });
    expect(cfg.maxAge).toBe(120);
    const res = await c.post("/api/auth/callback/credentials", { email: "ada@example.com", password: "correct horse" });
    expect(res.headers.get("location")).toBe(`${ORIGIN}/home`);
  });

  it("can be disabled", async () => {
    const c = makeClient({ providers: [passwordProvider()] }, { enabled: false });
    expect((await c.request("/api/auth/session")).status).toBe(404);
  });

  it("auth() reads the session from a request", async () => {
    const c = makeClient({ providers: [passwordProvider()] });
    await c.post("/api/auth/callback/credentials", { email: "ada@example.com", password: "correct horse" });
    const req = new Request(`${ORIGIN}/dashboard`, { headers: { cookie: c.cookieHeader() } });
    expect((await c.auth.auth(req))?.user.id).toBe("u1");
    expect(await c.auth.auth(new Request(`${ORIGIN}/dashboard`))).toBeNull();
  });
});
