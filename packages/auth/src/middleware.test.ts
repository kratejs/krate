import { beforeEach, describe, expect, it } from "vitest";
import { MemoryAdapter } from "./adapters/memory";
import { matchesPath } from "./match";
import { credentials } from "./providers/credentials";
import { resetRateLimits } from "./ratelimit";
import { ORIGIN, makeClient, type Client } from "./test-utils";
import type { AuthOptions, DeclarativeConfig } from "./types";

beforeEach(() => resetRateLimits());

describe("path patterns", () => {
  const cases: Array<[string, string, boolean]> = [
    ["/dashboard", "/dashboard", true],
    ["/dashboard", "/dashboard/", true],
    ["/dashboard", "/dashboard/settings", true],
    ["/dashboard", "/dashboards", false],
    ["/dashboard", "/", false],
    ["/dashboard/:path*", "/dashboard", true],
    ["/dashboard/:path*", "/dashboard/a/b", true],
    ["/dashboard/:path*", "/other", false],
    ["/api/**", "/api/users/1", true],
    ["/api/**", "/apiary", false],
    ["/users/:id", "/users/42", true],
    ["/users/:id", "/users/42/edit", true],
    ["/users/:id", "/users", false],
    ["/users/*", "/users/42", true],
    ["/users/*", "/users", false],
    ["/users/:id/settings", "/users/42/settings", true],
    ["/users/:id/settings", "/users/42/profile", false],
    ["/files/:rest+", "/files/a/b", true],
    ["/files/:rest+", "/files", false],
    ["/blog/:slug?", "/blog", true],
    ["/blog/:slug?", "/blog/hello", true],
    ["/a.b", "/a.b", true],
    ["/a.b", "/aXb", false],
    ["/", "/anything", true],
  ];
  for (const [pattern, path, expected] of cases) {
    it(`${pattern} ${expected ? "matches" : "does not match"} ${path}`, () => {
      expect(matchesPath(pattern, path)).toBe(expected);
    });
  }
});

function setup(options: AuthOptions = {}, declarative?: DeclarativeConfig) {
  const adapter = MemoryAdapter();
  const provider = credentials({
    authorize: async (creds) => {
      const user = await adapter.getUserByEmail(creds.email ?? "");
      return user && creds.password === "pw" ? user : null;
    },
  });
  const c = makeClient({ providers: [provider], adapter, session: { strategy: "jwt" }, ...options }, declarative);
  return { c, adapter };
}

async function signedIn(c: Client, adapter: ReturnType<typeof MemoryAdapter>, email: string, role?: string) {
  await adapter.createUser({ email, emailVerified: null, ...(role ? { role } : {}) });
  await c.post("/api/auth/callback/credentials", { email, password: "pw" });
}

const call = (c: Client, path: string, headers: Record<string, string> = {}) =>
  c.auth.middleware(new Request(ORIGIN + path, { headers: { ...headers, ...(c.cookieHeader() ? { cookie: c.cookieHeader() } : {}) } }));

describe("middleware", () => {
  it("lets unprotected paths through without touching the session", async () => {
    const { c } = setup({ protect: [{ source: "/dashboard" }] });
    expect(await call(c, "/")).toBeUndefined();
    expect(await call(c, "/blog/post")).toBeUndefined();
  });

  it("never blocks the auth routes themselves", async () => {
    const { c } = setup({ protect: [{ source: "/" }] });
    expect(await call(c, "/api/auth/signin")).toBeUndefined();
    expect(await call(c, "/api/auth/callback/credentials")).toBeUndefined();
  });

  it("redirects anonymous page requests to sign in with a callback", async () => {
    const { c } = setup({ protect: [{ source: "/dashboard" }] });
    const res = (await call(c, "/dashboard/settings?tab=billing"))!;
    expect(res.status).toBe(302);
    const location = new URL(res.headers.get("location")!);
    expect(location.pathname).toBe("/api/auth/signin");
    expect(location.searchParams.get("callbackUrl")).toBe("/dashboard/settings?tab=billing");
  });

  it("uses the configured sign in page or rule redirect", async () => {
    const { c } = setup({ pages: { signIn: "/login" }, protect: [{ source: "/dashboard" }, { source: "/admin", redirect: "/staff-login" }] });
    expect(new URL((await call(c, "/dashboard"))!.headers.get("location")!).pathname).toBe("/login");
    expect(new URL((await call(c, "/admin"))!.headers.get("location")!).pathname).toBe("/staff-login");
  });

  it("answers api requests with 401 json", async () => {
    const { c } = setup({ protect: [{ source: "/api/private/**" }] });
    const res = (await call(c, "/api/private/data"))!;
    expect(res.status).toBe(401);
    expect(await res.json()).toEqual({ error: "unauthorized" });
    const json = (await call(setup({ protect: [{ source: "/data" }] }).c, "/data", { accept: "application/json" }))!;
    expect(json.status).toBe(401);
  });

  it("honours a rule status for anonymous users", async () => {
    const { c } = setup({ protect: [{ source: "/secret", status: 404 }] });
    expect((await call(c, "/secret"))!.status).toBe(404);
  });

  it("allows a signed in user through", async () => {
    const { c, adapter } = setup({ protect: [{ source: "/dashboard" }] });
    await signedIn(c, adapter, "ada@example.com");
    expect(await call(c, "/dashboard")).toBeUndefined();
  });

  it("rejects a tampered or foreign session cookie", async () => {
    const { c, adapter } = setup({ protect: [{ source: "/dashboard" }] });
    await signedIn(c, adapter, "ada@example.com");
    const token = c.jar.get("krate-auth.session-token")!;
    c.jar.set("krate-auth.session-token", token.slice(0, -4) + "AAAA");
    expect((await call(c, "/dashboard"))!.status).toBe(302);
  });

  it("enforces roles", async () => {
    const { c, adapter } = setup({ protect: [{ source: "/admin", roles: ["admin", "owner"] }] });
    await signedIn(c, adapter, "user@example.com");
    expect((await call(c, "/admin"))!.status).toBe(403);

    const admin = setup({ protect: [{ source: "/admin", roles: ["admin"] }] });
    await signedIn(admin.c, admin.adapter, "root@example.com", "admin");
    expect(await call(admin.c, "/admin/users")).toBeUndefined();

    const hidden = setup({ protect: [{ source: "/admin", roles: ["admin"], status: 404 }] });
    await signedIn(hidden.c, hidden.adapter, "user@example.com");
    expect((await call(hidden.c, "/admin"))!.status).toBe(404);
  });

  it("uses the first matching rule", async () => {
    const { c, adapter } = setup({ protect: [{ source: "/app/public", status: 401 }, { source: "/app" }] });
    expect((await call(c, "/app/public"))!.status).toBe(401);
    expect((await call(c, "/app/private"))!.status).toBe(302);
    await signedIn(c, adapter, "ada@example.com");
    expect(await call(c, "/app/private")).toBeUndefined();
  });

  it("reads rules from the krate config", async () => {
    const { c } = setup({}, { protect: [{ source: "/account", redirect: "/signin" }] });
    const res = (await call(c, "/account"))!;
    expect(new URL(res.headers.get("location")!).pathname).toBe("/signin");
  });

  it("merges rules from config and code", async () => {
    const { c } = setup({ protect: [{ source: "/code-only" }] }, { protect: [{ source: "/config-only" }] });
    expect((await call(c, "/code-only"))!.status).toBe(302);
    expect((await call(c, "/config-only"))!.status).toBe(302);
    expect(await call(c, "/open")).toBeUndefined();
  });

  it("supports the authorized callback", async () => {
    const seen: Array<string | null> = [];
    const { c, adapter } = setup({
      callbacks: {
        authorized: ({ request, auth }) => {
          seen.push(auth?.user.email ?? null);
          const path = new URL(request.url).pathname;
          if (path.startsWith("/teapot")) return new Response("short and stout", { status: 418 });
          if (path.includes("/members")) return Boolean(auth);
          return true;
        },
      },
    });
    expect(await call(c, "/open")).toBeUndefined();
    expect((await call(c, "/teapot"))!.status).toBe(418);
    expect((await call(c, "/members"))!.status).toBe(302);
    expect((await call(c, "/api/members", { accept: "application/json" }))!.status).toBe(401);
    await signedIn(c, adapter, "ada@example.com");
    expect(await call(c, "/members")).toBeUndefined();
    expect(seen).toContain("ada@example.com");
  });

  it("fails closed when the adapter breaks under database sessions", async () => {
    const { c, adapter } = setup({ session: { strategy: "database" }, protect: [{ source: "/dashboard" }], logger: { error: () => {} } });
    await signedIn(c, adapter, "ada@example.com");
    expect(await call(c, "/dashboard")).toBeUndefined();
    adapter.getSessionAndUser = () => {
      throw new Error("db down");
    };
    expect((await call(c, "/dashboard"))!.status).toBe(503);
  });

  it("fails closed without a secret", async () => {
    delete process.env.AUTH_SECRET;
    const c = makeClient({ secret: "", providers: [], protect: [{ source: "/dashboard" }], logger: { error: () => {} } });
    expect((await call(c, "/dashboard"))!.status).toBe(503);
  });

  it("returns refreshed session cookies on pass-through", async () => {
    const { c, adapter } = setup({ protect: [{ source: "/dashboard" }], session: { strategy: "jwt", updateAge: 0 } });
    await signedIn(c, adapter, "ada@example.com");
    const res = (await call(c, "/dashboard"))!;
    expect(res.headers.get("x-middleware-next")).toBe("1");
    expect(res.headers.getSetCookie().some((s) => s.startsWith("krate-auth.session-token="))).toBe(true);
  });

  it("does nothing when disabled", async () => {
    const { c } = setup({ protect: [{ source: "/dashboard" }] }, { enabled: false });
    expect(await call(c, "/dashboard")).toBeUndefined();
  });
});
