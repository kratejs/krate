import { AsyncLocalStorage } from "node:async_hooks";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryAdapter } from "./adapters/memory";
import { defaultCookies } from "./cookies";
import { defineAuth } from "./index";
import { clearDiscoveryCache } from "./oauth-flow";
import { discord } from "./providers/catalog";
import { resetRateLimits } from "./ratelimit";
import { ORIGIN, SECRET } from "./test-utils";

const actionStore = new AsyncLocalStorage<any>();
const requestStore = new AsyncLocalStorage<any>();
(globalThis as any)[Symbol.for("krate.action.store")] = actionStore;
(globalThis as any)[Symbol.for("krate.request.store")] = requestStore;

interface Jar {
  values: Map<string, string>;
  sets: Array<{ name: string; value: string; options: any }>;
  deletes: string[];
}

// An action or route scope: the request's cookies in, the cookies it sets out.
function scope(cookies: Record<string, string> = {}, url = "/__krate/action/x") {
  const jar: Jar = { values: new Map(Object.entries(cookies)), sets: [], deletes: [] };
  const header = Object.entries(cookies).map(([k, v]) => `${k}=${encodeURIComponent(v)}`).join("; ");
  const ctx = {
    url,
    method: "POST",
    headers: new Headers({ host: "localhost:3000", cookie: header, accept: "application/json", "content-type": "application/json" }),
    cookies: {
      get: (name: string) => jar.values.get(name),
      set: (name: string, value: string, options: any) => {
        jar.sets.push({ name, value, options });
        jar.values.set(name, value);
      },
      delete: (name: string) => {
        jar.deletes.push(name);
        jar.values.delete(name);
      },
    },
  };
  return { jar, run: <T>(fn: () => T) => actionStore.run(ctx, fn) };
}

function discordAuth(extra: Record<string, unknown> = {}) {
  const adapter = new MemoryAdapter();
  const auth = defineAuth(() => ({
    secret: SECRET,
    adapter,
    session: { strategy: "database" },
    cookies: { prefix: "authjs" },
    providers: [
      discord({
        clientId: "discord-id",
        clientSecret: "discord-secret",
        issuer: "https://discord.com",
        authorization: { params: { scope: "identify guilds" } },
      }),
    ],
    ...extra,
  }));
  return { auth, adapter };
}

function stubDiscord() {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string | URL) => {
      const url = new URL(String(input));
      if (url.pathname === "/api/oauth2/token") return Response.json({ access_token: "at", token_type: "Bearer", scope: "identify guilds" });
      if (url.pathname === "/api/users/@me") return Response.json({ id: "123456789012345678", username: "ada", global_name: "Ada", avatar: null, discriminator: "0", email: "ada@example.com", verified: true });
      return new Response("not found", { status: 404 });
    }),
  );
}

async function catchRedirect(run: () => Promise<unknown>): Promise<any> {
  try {
    await run();
  } catch (err) {
    return err;
  }
  throw new Error("expected a redirect");
}

beforeEach(() => {
  resetRateLimits();
  clearDiscoveryCache();
});
afterEach(() => vi.unstubAllGlobals());

describe("server-side signIn and signOut", () => {
  it("signIn in a server action starts the OAuth redirect and sets the flow cookies", async () => {
    const { auth } = discordAuth();
    const s = scope();
    const err = await catchRedirect(() => s.run(() => auth.signIn("discord", { redirectTo: "/signin/done" })));
    const target = new URL(err.__krateRedirect);
    expect(err.__krateRedirectExternal).toBe(true);
    expect(target.origin + target.pathname).toBe("https://discord.com/api/oauth2/authorize");
    expect(target.searchParams.get("scope")).toBe("identify guilds");
    expect(target.searchParams.get("client_id")).toBe("discord-id");
    expect(target.searchParams.get("redirect_uri")).toBe(`${ORIGIN}/api/auth/callback/discord`);
    expect(target.searchParams.has("nonce")).toBe(false);
    expect(s.jar.sets.map((c) => c.name).sort()).toEqual(["authjs.pkce.code_verifier", "authjs.state"]);
    expect(s.jar.sets.every((c) => c.options.httpOnly === true && c.options.sameSite === "Lax")).toBe(true);

    // The callback the provider redirects to finishes the sign-in with those cookies.
    stubDiscord();
    const cookie = s.jar.sets.map((c) => `${c.name}=${encodeURIComponent(c.value)}`).join("; ");
    const res = await auth.handlers.GET(new Request(`${ORIGIN}/api/auth/callback/discord?code=c&state=${target.searchParams.get("state")}`, { headers: { cookie } }));
    expect(res.headers.get("location")).toBe(`${ORIGIN}/signin/done`);
    expect(res.headers.getSetCookie().some((l) => l.startsWith("authjs.session-token="))).toBe(true);
  });

  it("signOut clears the session cookie and redirects to redirectTo", async () => {
    const { auth, adapter } = discordAuth();
    const user = await adapter.createUser({ name: "Ada", email: "ada@example.com", emailVerified: null, image: null });
    await adapter.createSession({ sessionToken: "tok-1", userId: user.id, expires: new Date(Date.now() + 60_000) });
    const s = scope({ "authjs.session-token": "tok-1" });
    const err = await catchRedirect(() => s.run(() => auth.signOut({ redirectTo: "/bye" })));
    expect(err.__krateRedirect).toBe("/bye");
    expect(err.__krateRedirectExternal).toBeUndefined();
    expect(s.jar.sets.find((c) => c.name === "authjs.session-token")?.options.maxAge).toBe(0);
    expect(await adapter.getSessionAndUser!("tok-1")).toBeNull();
  });

  it("redirect:false returns the target and signIn outside a writable scope is an error", async () => {
    const { auth } = discordAuth();
    const url = await scope().run(() => auth.signIn("discord", { redirectTo: "/", redirect: false }));
    expect(url).toContain("https://discord.com/api/oauth2/authorize");
    await expect(auth.signIn("discord")).rejects.toThrow(/request scope/);
    await expect(requestStore.run({ url: "/", headers: new Headers() }, () => auth.signIn("discord"))).rejects.toThrow(/server component/);
  });
});

describe("auth() with no argument", () => {
  it("reads the session of the current scope and still accepts a Request", async () => {
    const { auth, adapter } = discordAuth();
    const user = await adapter.createUser({ name: "Ada", email: "ada@example.com", emailVerified: null, image: null });
    await adapter.createSession({ sessionToken: "tok-2", userId: user.id, expires: new Date(Date.now() + 60_000) });

    expect(await requestStore.run({ url: "/dashboard", headers: new Headers({ host: "localhost:3000", cookie: "authjs.session-token=tok-2" }) }, () => auth.auth())).toMatchObject({ user: { id: user.id, name: "Ada" } });
    expect(await scope({ "authjs.session-token": "tok-2" }).run(() => auth.auth())).toMatchObject({ user: { id: user.id } });
    expect(await requestStore.run({ url: "/", headers: new Headers({ host: "localhost:3000" }) }, () => auth.auth())).toBeNull();
    expect(await auth.auth(new Request(ORIGIN, { headers: { cookie: "authjs.session-token=tok-2" } }))).toMatchObject({ user: { id: user.id } });
    await expect(auth.auth()).rejects.toThrow(/request scope/);
  });
});

describe("discord with an issuer", () => {
  async function callbackWith(iss: string | null) {
    const { auth } = discordAuth();
    const s = scope();
    const err = await catchRedirect(() => s.run(() => auth.signIn("discord", { redirectTo: "/" })));
    const state = new URL(err.__krateRedirect).searchParams.get("state")!;
    const cookie = s.jar.sets.map((c) => `${c.name}=${encodeURIComponent(c.value)}`).join("; ");
    stubDiscord();
    const issParam = iss === null ? "" : `&iss=${encodeURIComponent(iss)}`;
    return auth.handlers.GET(new Request(`${ORIGIN}/api/auth/callback/discord?code=c&state=${state}${issParam}`, { headers: { cookie } }));
  }

  it("stays plain OAuth and validates the RFC 9207 iss parameter", async () => {
    expect(discord({ issuer: "https://discord.com" }).type).toBe("oauth");
    expect((await callbackWith("https://discord.com")).headers.get("location")).toBe(`${ORIGIN}/`);
    expect((await callbackWith(null)).headers.get("location")).toBe(`${ORIGIN}/`);
    expect((await callbackWith("https://evil.example")).headers.get("location")).toContain("error=OAuthCallback");
  });

  it("accepts a provider factory in providers, like Auth.js", () => {
    process.env.AUTH_DISCORD_ID = "env-id";
    try {
      const cfg = defineAuth({ secret: SECRET, providers: [discord] }).config();
      expect(cfg.providers.map((p) => p.id)).toEqual(["discord"]);
    } finally {
      delete process.env.AUTH_DISCORD_ID;
    }
  });

  it("keeps the provider endpoints when the app only sets authorization params", () => {
    const p = discord({ authorization: { params: { scope: "identify guilds" } } });
    expect(p.authorization.url).toBe("https://discord.com/api/oauth2/authorize");
    expect(p.authorization.params).toEqual({ scope: "identify guilds" });
  });
});

describe("cookies.prefix", () => {
  it("authjs reproduces the Auth.js v5 cookie names", () => {
    const plain = defaultCookies({ prefix: "authjs", secure: false });
    expect(Object.fromEntries(Object.entries(plain).map(([k, v]) => [k, v.name]))).toMatchObject({
      sessionToken: "authjs.session-token",
      callbackUrl: "authjs.callback-url",
      csrfToken: "authjs.csrf-token",
      pkceCodeVerifier: "authjs.pkce.code_verifier",
      state: "authjs.state",
    });
    const secure = defaultCookies({ prefix: "authjs", secure: true });
    expect(secure.sessionToken.name).toBe("__Secure-authjs.session-token");
    expect(secure.callbackUrl.name).toBe("__Secure-authjs.callback-url");
    expect(secure.csrfToken.name).toBe("__Host-authjs.csrf-token");
    expect(secure.pkceCodeVerifier.name).toBe("__Secure-authjs.pkce.code_verifier");
    expect(secure.state.name).toBe("__Secure-authjs.state");
  });
});
