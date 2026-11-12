import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryAdapter } from "./adapters/memory";
import { b64urlEncode, hex } from "./bytes";
import { sha256Bytes } from "./crypto";
import { clearDiscoveryCache } from "./oauth-flow";
import { genericOAuth } from "./providers/oauth";
import { email } from "./providers/email";
import { resetRateLimits } from "./ratelimit";
import { ORIGIN, makeClient, type Client } from "./test-utils";
import type { AuthOptions } from "./types";

interface IdpCall {
  url: URL;
  init: RequestInit;
  body: URLSearchParams;
}

interface Idp {
  calls: IdpCall[];
  profile: Record<string, any>;
  tokenResponse: Record<string, any> | (() => Record<string, any>);
  tokenStatus: number;
  idToken?: (nonce: string | undefined, clientId: string) => string;
  discovery?: Record<string, any>;
  userinfoStatus: number;
}

function jwt(claims: Record<string, unknown>): string {
  return [b64urlEncode(JSON.stringify({ alg: "RS256" })), b64urlEncode(JSON.stringify(claims)), "sig"].join(".");
}

function installIdp(): Idp {
  const idp: Idp = {
    calls: [],
    profile: { id: "gh-42", name: "Ada Lovelace", email: "ada@example.com", avatar: "https://img.test/ada.png" },
    tokenResponse: { access_token: "at-1", token_type: "Bearer", expires_in: 3600, refresh_token: "rt-1", scope: "read:user" },
    tokenStatus: 200,
    userinfoStatus: 200,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string | URL, init: RequestInit = {}) => {
      const url = new URL(String(input));
      const raw = typeof init.body === "string" ? init.body : "";
      idp.calls.push({ url, init, body: new URLSearchParams(raw) });
      if (url.pathname === "/.well-known/openid-configuration") {
        return Response.json(idp.discovery ?? {});
      }
      if (url.pathname === "/token") {
        const base = typeof idp.tokenResponse === "function" ? idp.tokenResponse() : idp.tokenResponse;
        const body = new URLSearchParams(raw);
        const response = { ...base } as Record<string, any>;
        if (idp.idToken) response.id_token = idp.idToken(lastNonce, body.get("client_id") ?? "client-id");
        return new Response(JSON.stringify(response), { status: idp.tokenStatus, headers: { "content-type": "application/json" } });
      }
      if (url.pathname === "/userinfo") {
        return new Response(JSON.stringify(idp.profile), { status: idp.userinfoStatus, headers: { "content-type": "application/json" } });
      }
      return new Response("not found", { status: 404 });
    }),
  );
  return idp;
}

let lastNonce: string | undefined;

function idpProvider(extra: Record<string, unknown> = {}) {
  return genericOAuth({
    id: "idp",
    name: "Test IdP",
    clientId: "client-id",
    clientSecret: "client-secret",
    authorization: "https://idp.test/authorize",
    token: "https://idp.test/token",
    userinfo: "https://idp.test/userinfo",
    scope: "read:user user:email",
    profile: (p) => ({ id: p.id, name: p.name, email: p.email, image: p.avatar }),
    ...extra,
  });
}

async function begin(c: Client, provider = "idp", callbackUrl = "/dashboard"): Promise<URL> {
  const res = await c.post(`/api/auth/signin/${provider}`, { callbackUrl });
  expect(res.status).toBe(302);
  const target = new URL(res.headers.get("location")!);
  lastNonce = target.searchParams.get("nonce") ?? undefined;
  return target;
}

async function callback(c: Client, url: URL, overrides: Record<string, string> = {}, provider = "idp"): Promise<Response> {
  const params = new URLSearchParams({ code: "auth-code", state: url.searchParams.get("state") ?? "", ...overrides });
  for (const [k, v] of [...params]) if (v === "") params.delete(k);
  return c.request(`/api/auth/callback/${provider}?${params}`);
}

beforeEach(() => {
  resetRateLimits();
  clearDiscoveryCache();
  lastNonce = undefined;
});
afterEach(() => vi.unstubAllGlobals());

describe("oauth sign in", () => {
  it("runs the full authorization code flow with PKCE and state", async () => {
    const idp = installIdp();
    const c = makeClient({ providers: [idpProvider()] });
    const url = await begin(c);
    expect(url.origin + url.pathname).toBe("https://idp.test/authorize");
    expect(url.searchParams.get("client_id")).toBe("client-id");
    expect(url.searchParams.get("redirect_uri")).toBe(`${ORIGIN}/api/auth/callback/idp`);
    expect(url.searchParams.get("response_type")).toBe("code");
    expect(url.searchParams.get("scope")).toBe("read:user user:email");
    expect(url.searchParams.get("code_challenge_method")).toBe("S256");
    expect(url.searchParams.get("state")).toHaveLength(32);
    expect(c.jar.has("krate-auth.state")).toBe(true);
    expect(c.jar.get("krate-auth.state")).not.toContain(url.searchParams.get("state")!);

    const res = await callback(c, url);
    expect(res.status).toBe(302);
    expect(res.headers.get("location")).toBe(`${ORIGIN}/dashboard`);

    const token = idp.calls.find((call) => call.url.pathname === "/token")!;
    const verifier = token.body.get("code_verifier")!;
    expect(b64urlEncode(sha256Bytes(verifier))).toBe(url.searchParams.get("code_challenge"));
    expect(token.body.get("code")).toBe("auth-code");
    expect(token.body.get("grant_type")).toBe("authorization_code");
    expect(new Headers(token.init.headers).get("authorization")).toBe(`Basic ${btoa("client-id:client-secret")}`);
    const info = idp.calls.find((call) => call.url.pathname === "/userinfo")!;
    expect(new Headers(info.init.headers).get("authorization")).toBe("Bearer at-1");

    expect((await c.session()).user).toMatchObject({ id: "gh-42", name: "Ada Lovelace", email: "ada@example.com", image: "https://img.test/ada.png" });
    expect(c.jar.has("krate-auth.state")).toBe(false);
    expect(c.jar.has("krate-auth.pkce.code_verifier")).toBe(false);
  });

  it("supports client_secret_post", async () => {
    const idp = installIdp();
    const c = makeClient({ providers: [idpProvider({ client: { token_endpoint_auth_method: "client_secret_post" } })] });
    await callback(c, await begin(c));
    const token = idp.calls.find((call) => call.url.pathname === "/token")!;
    expect(token.body.get("client_id")).toBe("client-id");
    expect(token.body.get("client_secret")).toBe("client-secret");
    expect(new Headers(token.init.headers).get("authorization")).toBeNull();
  });

  it("rejects a state mismatch, missing state cookie and provider errors", async () => {
    installIdp();
    const c = makeClient({ providers: [idpProvider()] });
    const url = await begin(c);
    const bad = await callback(c, url, { state: "attacker-state" });
    expect(bad.headers.get("location")).toContain("error=OAuthCallback");
    expect(await c.session()).toBeNull();

    const fresh = makeClient({ providers: [idpProvider()] });
    const noCookie = await fresh.request(`/api/auth/callback/idp?code=x&state=${url.searchParams.get("state")}`);
    expect(noCookie.headers.get("location")).toContain("error=OAuthCallback");

    const c2 = makeClient({ providers: [idpProvider()] });
    const u2 = await begin(c2);
    const denied = await callback(c2, u2, { error: "access_denied", code: "" });
    expect(denied.headers.get("location")).toContain("error=AccessDenied");
  });

  it("fails cleanly when the token endpoint or userinfo errors", async () => {
    const idp = installIdp();
    idp.tokenStatus = 400;
    idp.tokenResponse = { error: "invalid_grant" };
    const c = makeClient({ providers: [idpProvider()] });
    const res = await callback(c, await begin(c));
    expect(res.headers.get("location")).toContain("error=OAuthCallback");
    expect(await c.session()).toBeNull();

    idp.tokenStatus = 200;
    idp.tokenResponse = { access_token: "at" };
    idp.userinfoStatus = 500;
    const c2 = makeClient({ providers: [idpProvider()] });
    const res2 = await callback(c2, await begin(c2));
    expect(res2.headers.get("location")).toContain("error=OAuthCallback");
  });

  it("rejects a profile without an id", async () => {
    const idp = installIdp();
    idp.profile = { name: "No Id" };
    const c = makeClient({ providers: [idpProvider()] });
    const res = await callback(c, await begin(c));
    expect(res.headers.get("location")).toContain("error=OAuthCallback");
  });

  it("explains a missing client id", async () => {
    installIdp();
    const c = makeClient({ providers: [idpProvider({ clientId: undefined })] });
    const res = await c.post("/api/auth/signin/idp", {}, { json: true, headers: { "x-auth-return-redirect": "1" } });
    expect(res.status).toBe(500);
    expect(((await res.json()) as any).message).toContain("AUTH_IDP_ID");
  });

  it("parses urlencoded token responses", async () => {
    const idp = installIdp();
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL) => {
        const url = new URL(String(input));
        if (url.pathname === "/token") return new Response("access_token=at-9&token_type=bearer&scope=user", { headers: { "content-type": "application/x-www-form-urlencoded" } });
        return Response.json(idp.profile);
      }),
    );
    const c = makeClient({ providers: [idpProvider()] });
    const res = await callback(c, await begin(c));
    expect(res.headers.get("location")).toBe(`${ORIGIN}/dashboard`);
  });

  it("uses the userinfo.request hook", async () => {
    installIdp();
    const provider = idpProvider({
      userinfo: {
        url: "https://idp.test/userinfo",
        request: async ({ tokens }: any) => ({ id: "custom-1", name: `token:${tokens.access_token}`, email: null }),
      },
    });
    const c = makeClient({ providers: [provider] });
    await callback(c, await begin(c));
    expect((await c.session()).user).toMatchObject({ id: "custom-1", name: "token:at-1" });
  });

  it("only sends users back to safe callback urls", async () => {
    installIdp();
    const c = makeClient({ providers: [idpProvider()] });
    const url = await begin(c, "idp", "https://evil.example/steal");
    const res = await callback(c, url);
    expect(res.headers.get("location")).toBe(`${ORIGIN}/`);
  });

  it("requires a csrf token to start", async () => {
    installIdp();
    const c = makeClient({ providers: [idpProvider()] });
    const res = await c.request("/api/auth/signin/idp", { method: "POST", form: {} });
    expect(res.headers.get("location")).toContain("error=MissingCSRF");
  });
});

describe("oidc", () => {
  const issuer = "https://idp.test";

  function oidc(extra: Record<string, unknown> = {}) {
    return genericOAuth({ id: "idp", type: "oidc", issuer, clientId: "client-id", clientSecret: "s", ...extra });
  }

  it("discovers endpoints and validates the id_token", async () => {
    const idp = installIdp();
    idp.discovery = { issuer, authorization_endpoint: `${issuer}/authorize`, token_endpoint: `${issuer}/token`, userinfo_endpoint: `${issuer}/userinfo` };
    idp.idToken = (nonce, clientId) => jwt({ iss: issuer, aud: clientId, sub: "oidc-7", nonce, email: "oidc@example.com", name: "Oidc User", exp: Math.floor(Date.now() / 1000) + 600 });
    const c = makeClient({ providers: [oidc()] });
    const url = await begin(c);
    expect(url.origin + url.pathname).toBe(`${issuer}/authorize`);
    expect(url.searchParams.get("scope")).toBe("openid profile email");
    expect(url.searchParams.get("nonce")).toBeTruthy();
    const res = await callback(c, url);
    expect(res.headers.get("location")).toBe(`${ORIGIN}/dashboard`);
    expect((await c.session()).user).toMatchObject({ id: "oidc-7", email: "oidc@example.com", name: "Oidc User" });

    await begin(makeClient({ providers: [oidc()] }));
    expect(idp.calls.filter((call) => call.url.pathname.endsWith("openid-configuration"))).toHaveLength(1);
  });

  it("rejects id_tokens with a wrong nonce, audience, issuer or expiry", async () => {
    const idp = installIdp();
    idp.discovery = { issuer, authorization_endpoint: `${issuer}/authorize`, token_endpoint: `${issuer}/token` };
    const future = Math.floor(Date.now() / 1000) + 600;
    const cases: Array<(nonce: string | undefined) => Record<string, unknown>> = [
      () => ({ iss: issuer, aud: "client-id", sub: "1", nonce: "forged", exp: future }),
      (nonce) => ({ iss: issuer, aud: "someone-else", sub: "1", nonce, exp: future }),
      (nonce) => ({ iss: "https://evil.test", aud: "client-id", sub: "1", nonce, exp: future }),
      (nonce) => ({ iss: issuer, aud: "client-id", sub: "1", nonce, exp: 1000 }),
    ];
    for (const make of cases) {
      idp.idToken = (nonce) => jwt(make(nonce));
      const c = makeClient({ providers: [oidc()] });
      const res = await callback(c, await begin(c));
      expect(res.headers.get("location")).toContain("error=OAuthCallback");
      expect(await c.session()).toBeNull();
    }
  });
});

describe("account linking with an adapter", () => {
  it("creates a user and links the account on first sign in", async () => {
    installIdp();
    const adapter = MemoryAdapter();
    const c = makeClient({ providers: [idpProvider()], adapter, session: { strategy: "jwt" } });
    const res = await callback(c, await begin(c));
    expect(res.status).toBe(302);
    expect(adapter.store.users.size).toBe(1);
    expect(adapter.store.accounts).toHaveLength(1);
    expect(adapter.store.accounts[0]).toMatchObject({ provider: "idp", providerAccountId: "gh-42", access_token: "at-1", refresh_token: "rt-1" });
    const user = [...adapter.store.users.values()][0]!;
    expect((await c.session()).user.id).toBe(user.id);

    const again = makeClient({ providers: [idpProvider()], adapter, session: { strategy: "jwt" } });
    await callback(again, await begin(again));
    expect(adapter.store.users.size).toBe(1);
    expect(adapter.store.accounts).toHaveLength(1);
  });

  it("refuses to link an unverified email to an existing account", async () => {
    installIdp();
    const adapter = MemoryAdapter();
    await adapter.createUser({ email: "ada@example.com", emailVerified: null, name: "Existing" });
    const c = makeClient({ providers: [idpProvider()], adapter });
    const res = await callback(c, await begin(c));
    expect(res.headers.get("location")).toContain("error=OAuthAccountNotLinked");
    expect(await c.session()).toBeNull();
    expect(adapter.store.accounts).toHaveLength(0);
  });

  it("links by email only when explicitly allowed", async () => {
    installIdp();
    for (const options of [{ providers: [idpProvider({ allowDangerousEmailAccountLinking: true })] }, { providers: [idpProvider()], linking: { automatic: true } }] as AuthOptions[]) {
      const adapter = MemoryAdapter();
      const existing = await adapter.createUser({ email: "ada@example.com", emailVerified: null, name: "Existing" });
      const c = makeClient({ ...options, adapter });
      const res = await callback(c, await begin(c));
      expect(res.headers.get("location")).toBe(`${ORIGIN}/dashboard`);
      expect(adapter.store.users.size).toBe(1);
      expect(adapter.store.accounts[0]!.userId).toBe(existing.id);
    }
  });

  it("links another provider to the signed in user", async () => {
    installIdp();
    const adapter = MemoryAdapter();
    const owner = await adapter.createUser({ email: "owner@example.com", emailVerified: new Date(), name: "Owner" });
    const c = makeClient({ providers: [idpProvider()], adapter, session: { strategy: "jwt" }, callbacks: {} });
    const { issueSession } = await import("./session");
    const { ResponseCookies } = await import("./cookies");
    const { createContext } = await import("./config");
    const jar = new ResponseCookies();
    const ctx = createContext(c.auth.config(), new Request(`${ORIGIN}/`));
    await issueSession(ctx, jar, { user: owner });
    const headers = new Headers();
    jar.apply(headers);
    c.absorb(new Response(null, { headers }));
    const res = await callback(c, await begin(c));
    expect(res.status).toBe(302);
    expect(adapter.store.users.size).toBe(1);
    expect(adapter.store.accounts[0]).toMatchObject({ userId: owner.id, provider: "idp" });
    expect((await c.session()).user.id).toBe(owner.id);
  });

  it("fires createUser, linkAccount and signIn events", async () => {
    installIdp();
    const seen: string[] = [];
    const c = makeClient({
      providers: [idpProvider()],
      adapter: MemoryAdapter(),
      events: {
        createUser: () => void seen.push("create"),
        linkAccount: () => void seen.push("link"),
        signIn: ({ isNewUser }) => void seen.push(`signin:${isNewUser}`),
      },
    });
    await callback(c, await begin(c));
    expect(seen).toEqual(["create", "link", "signin:true"]);
  });

  it("redirects new users to pages.newUser", async () => {
    installIdp();
    const c = makeClient({ providers: [idpProvider()], adapter: MemoryAdapter(), pages: { newUser: "/welcome" } });
    const res = await callback(c, await begin(c));
    expect(res.headers.get("location")).toBe(`${ORIGIN}/welcome`);
  });
});

describe("database sessions", () => {
  it("stores the session server side and removes it on sign out", async () => {
    installIdp();
    const adapter = MemoryAdapter();
    const c = makeClient({ providers: [idpProvider()], adapter, session: { strategy: "database" } });
    await callback(c, await begin(c));
    const token = c.jar.get("krate-auth.session-token")!;
    expect(token).toMatch(/^[0-9a-f-]{36}$/);
    expect(adapter.store.sessions.has(token)).toBe(true);
    expect((await c.session()).user.email).toBe("ada@example.com");

    await c.post("/api/auth/signout");
    expect(adapter.store.sessions.size).toBe(0);
    expect(await c.session()).toBeNull();
  });

  it("drops expired and unknown sessions", async () => {
    installIdp();
    const adapter = MemoryAdapter();
    const c = makeClient({ providers: [idpProvider()], adapter, session: { strategy: "database" } });
    await callback(c, await begin(c));
    const token = c.jar.get("krate-auth.session-token")!;
    adapter.store.sessions.get(token)!.expires = new Date(Date.now() - 1000);
    expect(await c.session()).toBeNull();
    expect(adapter.store.sessions.has(token)).toBe(false);

    c.jar.set("krate-auth.session-token", "unknown-token");
    expect(await c.session()).toBeNull();
    expect(c.jar.has("krate-auth.session-token")).toBe(false);
  });

  it("extends the expiry after updateAge", async () => {
    installIdp();
    const adapter = MemoryAdapter();
    const c = makeClient({ providers: [idpProvider()], adapter, session: { strategy: "database", maxAge: 3600, updateAge: 60 } });
    await callback(c, await begin(c));
    const token = c.jar.get("krate-auth.session-token")!;
    adapter.store.sessions.get(token)!.expires = new Date(Date.now() + 600 * 1000);
    await c.session();
    expect(adapter.store.sessions.get(token)!.expires.valueOf()).toBeGreaterThan(Date.now() + 3000 * 1000);
  });
});

describe("email magic links", () => {
  function emailSetup(extra: AuthOptions = {}) {
    const sent: Array<{ identifier: string; url: string; token: string }> = [];
    const adapter = MemoryAdapter();
    const provider = email({ sendVerificationRequest: ({ identifier, url, token }) => void sent.push({ identifier, url, token }) });
    const c = makeClient({ providers: [provider], adapter, session: { strategy: "jwt" }, ...extra });
    return { c, adapter, sent };
  }

  it("sends a link, stores only a hash and signs the user in once", async () => {
    const { c, adapter, sent } = emailSetup();
    const res = await c.post("/api/auth/signin/email", { email: "  Ada@Example.COM ", callbackUrl: "/inbox" });
    expect(res.headers.get("location")).toBe(`${ORIGIN}/api/auth/verify-request`);
    expect(sent).toHaveLength(1);
    expect(sent[0]!.identifier).toBe("ada@example.com");
    expect(adapter.store.tokens).toHaveLength(1);
    expect(adapter.store.tokens[0]!.token).not.toBe(sent[0]!.token);
    expect(adapter.store.tokens[0]!.token).toBe(hex(sha256Bytes(`${sent[0]!.token}${"test-secret-with-plenty-of-entropy-0123456789"}`)));

    const link = new URL(sent[0]!.url);
    const done = await c.request(link.pathname + link.search);
    expect(done.headers.get("location")).toBe(`${ORIGIN}/inbox`);
    expect((await c.session()).user.email).toBe("ada@example.com");
    expect(adapter.store.users.size).toBe(1);
    expect([...adapter.store.users.values()][0]!.emailVerified).toBeInstanceOf(Date);

    const replay = makeClient({ providers: [email({ sendVerificationRequest: () => {} })], adapter, session: { strategy: "jwt" } });
    const again = await replay.request(link.pathname + link.search);
    expect(again.headers.get("location")).toContain("error=Verification");
    expect(await replay.session()).toBeNull();
  });

  it("rejects expired, tampered and mismatched links", async () => {
    const { c, adapter, sent } = emailSetup();
    await c.post("/api/auth/signin/email", { email: "ada@example.com" });
    const link = new URL(sent[0]!.url);

    const tampered = new URL(link);
    tampered.searchParams.set("token", "wrong");
    expect((await c.request(tampered.pathname + tampered.search)).headers.get("location")).toContain("error=Verification");

    const otherEmail = new URL(link);
    otherEmail.searchParams.set("email", "mallory@example.com");
    expect((await c.request(otherEmail.pathname + otherEmail.search)).headers.get("location")).toContain("error=Verification");

    adapter.store.tokens[0]!.expires = new Date(Date.now() - 1000);
    expect((await c.request(link.pathname + link.search)).headers.get("location")).toContain("error=Verification");
    expect(await c.session()).toBeNull();
  });

  it("rejects invalid addresses and send failures", async () => {
    const { c } = emailSetup();
    const bad = await c.post("/api/auth/signin/email", { email: "not-an-email" });
    expect(bad.headers.get("location")).toContain("error=EmailSignin");
    const failing = makeClient({
      providers: [
        email({
          sendVerificationRequest: () => {
            throw new Error("smtp down");
          },
        }),
      ],
      adapter: MemoryAdapter(),
    });
    const res = await failing.post("/api/auth/signin/email", { email: "ada@example.com" });
    expect(res.headers.get("location")).toContain("error=EmailSignin");
  });

  it("lets the signIn callback block unknown addresses", async () => {
    const { c, sent } = emailSetup({ callbacks: { signIn: ({ email, user }) => (email?.verificationRequest ? user.email === "ada@example.com" : true) } });
    const denied = await c.post("/api/auth/signin/email", { email: "stranger@example.com" });
    expect(denied.headers.get("location")).toContain("error=AccessDenied");
    expect(sent).toHaveLength(0);
    await c.post("/api/auth/signin/email", { email: "ada@example.com" });
    expect(sent).toHaveLength(1);
  });

  it("needs an adapter", async () => {
    const c = makeClient({ providers: [email({ sendVerificationRequest: () => {} })] });
    const res = await c.post("/api/auth/signin/email", { email: "ada@example.com" }, { json: true, headers: { "x-auth-return-redirect": "1" } });
    expect(res.status).toBe(500);
  });
});
