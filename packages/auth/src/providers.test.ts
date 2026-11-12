import { generateKeyPairSync, verify } from "node:crypto";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { b64urlDecode, b64urlDecodeString } from "./bytes";
import { catalog, apple, discord, github, gitlab, google, mastodon, microsoftEntraId, notion, reddit, twitter, workos, yandex } from "./providers/catalog";
import { appleClientSecret } from "./providers/apple-secret";
import { envKey } from "./providers/oauth";
import { resolveDeclarativeProvider } from "./providers/registry";
import type { OAuthConfig } from "./types";

const ISSUER_BASED = new Set([
  "asgardeo",
  "auth0",
  "authentik",
  "azure-ad-b2c",
  "cognito",
  "descope",
  "duende-identityserver6",
  "fusionauth",
  "keycloak",
  "kinde",
  "logto",
  "mastodon",
  "mattermost",
  "okta",
  "onelogin",
  "zitadel",
]);

const saved: Record<string, string | undefined> = {};
const envNames: string[] = [];

function setEnv(name: string, value: string) {
  envNames.push(name);
  saved[name] = process.env[name];
  process.env[name] = value;
}

afterEach(() => {
  for (const name of envNames.splice(0)) {
    if (saved[name] === undefined) delete process.env[name];
    else process.env[name] = saved[name];
  }
  vi.unstubAllGlobals();
});

describe("catalog", () => {
  it("ships a large set of providers", () => {
    expect(Object.keys(catalog).length).toBeGreaterThanOrEqual(55);
    for (const id of ["github", "google", "discord", "apple", "microsoft-entra-id", "twitter", "okta", "auth0", "keycloak", "spotify", "linkedin", "gitlab", "slack", "twitch", "facebook"]) {
      expect(catalog[id], id).toBeTypeOf("function");
    }
  });

  it.each(Object.keys(catalog))("%s builds a valid provider config", (id) => {
    const key = envKey(id);
    setEnv(`AUTH_${key}_ID`, "env-client-id");
    setEnv(`AUTH_${key}_SECRET`, "env-client-secret");
    if (ISSUER_BASED.has(id)) setEnv(`AUTH_${key}_ISSUER`, "https://idp.example.com/");
    const provider = catalog[id]!() as OAuthConfig;
    expect(provider.id).toBe(id);
    expect(provider.name).toBeTruthy();
    expect(["oauth", "oidc"]).toContain(provider.type);
    expect(provider.clientId).toBe("env-client-id");
    expect(provider.clientSecret).toBe("env-client-secret");
    expect(provider.checks?.length).toBeGreaterThan(0);
    expect(provider.profile).toBeTypeOf("function");
    if (provider.type === "oidc") {
      expect(Boolean(provider.issuer || provider.wellKnown), `${id} needs an issuer`).toBe(true);
    } else {
      expect(provider.authorization.url, `${id} authorization`).toMatch(/^https:\/\//);
      expect(provider.token.url, `${id} token`).toMatch(/^https:\/\//);
      expect(Boolean(provider.userinfo?.url || provider.userinfo?.request), `${id} profile source`).toBe(true);
    }
  });

  it("matches the provider ids the Go config validator knows about", () => {
    const here = dirname(fileURLToPath(import.meta.url));
    const source = readFileSync(resolve(here, "../../compiler/internal/config/auth.go"), "utf8");
    const block = /var AuthKnownProviders = \[\]string\{([\s\S]*?)\n\}/.exec(source)?.[1] ?? "";
    const known = new Set([...block.matchAll(/"([^"]+)"/g)].map((m) => m[1]!));
    for (const id of Object.keys(catalog)) expect(known.has(id), `${id} missing from AuthKnownProviders in auth.go`).toBe(true);
    for (const id of ["credentials", "email", "resend", "passkey"]) expect(known.has(id)).toBe(true);
    for (const id of known) {
      if (!["credentials", "email", "resend", "passkey"].includes(id)) expect(catalog[id], `${id} is listed in auth.go but not implemented`).toBeTypeOf("function");
    }
  });

  it("lets options override env credentials, scope and name", () => {
    setEnv("AUTH_GITHUB_ID", "from-env");
    const p = github({ clientId: "explicit", scope: "repo", name: "Code" }) as OAuthConfig;
    expect(p.clientId).toBe("explicit");
    expect(p.authorization.params?.scope).toBe("repo");
    expect(p.name).toBe("Code");
  });

  it("reads credentials without a provider prefix as a fallback", () => {
    setEnv("DISCORD_CLIENT_ID", "fallback-id");
    expect((discord() as OAuthConfig).clientId).toBe("fallback-id");
  });
});

describe("profile mapping", () => {
  const tokens = { access_token: "tok" };

  it("github prefers a verified primary email and falls back to /user/emails", async () => {
    const calls: string[] = [];
    const p = github({ clientId: "x", clientSecret: "y" }) as OAuthConfig;
    const hook = p.userinfo!.request!;
    const fetchStub = async (input: string | URL | Request) => {
      const url = String(input);
      calls.push(url);
      if (url.endsWith("/user")) return Response.json({ id: 7, login: "ada", name: null, email: null, avatar_url: "https://a/img" });
      return Response.json([
        { email: "old@example.com", primary: false, verified: true },
        { email: "ada@example.com", primary: true, verified: true },
      ]);
    };
    const raw = await hook({ tokens, provider: p, fetch: fetchStub as typeof fetch });
    const user = await p.profile!(raw, tokens);
    expect(calls).toHaveLength(2);
    expect(user).toMatchObject({ id: "7", name: "ada", email: "ada@example.com", image: "https://a/img" });
    expect(user.emailVerified).toBeInstanceOf(Date);
  });

  it("github surfaces api failures", async () => {
    const p = github({ clientId: "x" }) as OAuthConfig;
    await expect(p.userinfo!.request!({ tokens, provider: p, fetch: (async () => new Response("no", { status: 401 })) as typeof fetch })).rejects.toThrow("401");
  });

  it("google marks verified emails", async () => {
    const p = google({ clientId: "x" }) as OAuthConfig;
    const user = await p.profile!({ sub: "g1", name: "Ada", email: "a@x.io", picture: "p", email_verified: true }, tokens);
    expect(user).toMatchObject({ id: "g1", email: "a@x.io" });
    expect(user.emailVerified).toBeInstanceOf(Date);
    expect(p.authorization.params).toMatchObject({ access_type: "offline", prompt: "consent" });
    expect(p.issuer).toBe("https://accounts.google.com");
  });

  it("discord builds avatar urls for custom, animated and default avatars", async () => {
    const p = discord({ clientId: "x" }) as OAuthConfig;
    expect((await p.profile!({ id: "80351110224678912", username: "n", discriminator: "0", avatar: "abc" }, tokens)).image).toBe("https://cdn.discordapp.com/avatars/80351110224678912/abc.png");
    expect((await p.profile!({ id: "1", username: "n", discriminator: "0", avatar: "a_xyz" }, tokens)).image).toMatch(/\.gif$/);
    expect((await p.profile!({ id: "80351110224678912", username: "n", discriminator: "0", avatar: null }, tokens)).image).toMatch(/embed\/avatars\/\d\.png$/);
    expect((await p.profile!({ id: "5", username: "n", discriminator: "1234", avatar: null, global_name: "G" }, tokens)).name).toBe("G");
  });

  it("twitter unwraps the data envelope", async () => {
    const p = twitter({ clientId: "x" }) as OAuthConfig;
    expect(p.userinfo?.params).toEqual({ "user.fields": "profile_image_url" });
    expect(await p.profile!({ data: { id: "9", name: "N", username: "u", profile_image_url: "i" } }, tokens)).toMatchObject({ id: "9", name: "N", image: "i" });
  });

  it("gitlab supports self-hosted instances", () => {
    const hosted = gitlab({ clientId: "x", issuer: "https://git.corp.example/" }) as OAuthConfig;
    expect(hosted.authorization.url).toBe("https://git.corp.example/oauth/authorize");
    expect(hosted.userinfo?.url).toBe("https://git.corp.example/api/v4/user");
    expect(hosted.type).toBe("oauth");
    expect((gitlab({ clientId: "x" }) as OAuthConfig).token.url).toBe("https://gitlab.com/oauth/token");
  });

  it("mastodon derives endpoints from the instance", () => {
    const p = mastodon({ clientId: "x", issuer: "https://mastodon.social" }) as OAuthConfig;
    expect(p.token.url).toBe("https://mastodon.social/oauth/token");
    expect(p.type).toBe("oauth");
    expect((mastodon({ clientId: "x" }) as OAuthConfig).authorization.url).toBeUndefined();
  });

  it("entra only skips the issuer check for multi-tenant endpoints", () => {
    const common = microsoftEntraId({ clientId: "x" }) as OAuthConfig;
    expect(common.issuer).toBe("https://login.microsoftonline.com/common/v2.0");
    expect(common.skipIssuerCheck).toBe(true);
    const tenant = microsoftEntraId({ clientId: "x", tenantId: "abc-123" }) as OAuthConfig;
    expect(tenant.issuer).toBe("https://login.microsoftonline.com/abc-123/v2.0");
    expect(tenant.skipIssuerCheck).toBe(false);
  });

  it("apple posts the response and requests name and email", async () => {
    const p = apple({ clientId: "x" }) as OAuthConfig;
    expect(p.authorization.params).toMatchObject({ response_mode: "form_post", scope: "name email" });
    expect(await p.profile!({ sub: "a1", email: "a@privaterelay.appleid.com", email_verified: "true" }, tokens)).toMatchObject({ id: "a1", email: "a@privaterelay.appleid.com" });
  });

  it("notion takes the profile from the token response", async () => {
    const p = notion({ clientId: "x" }) as OAuthConfig;
    const raw = await p.userinfo!.request!({ tokens: { access_token: "t", owner: { user: { id: "n1", name: "N", avatar_url: "a", person: { email: "n@x.io" } } } }, provider: p, fetch });
    expect(await p.profile!(raw, tokens)).toMatchObject({ id: "n1", email: "n@x.io", image: "a" });
  });

  it("reddit sends a user agent and strips avatar query strings", async () => {
    const p = reddit({ clientId: "x" }) as OAuthConfig;
    let seen: Headers | undefined;
    const stub = async (_: string | URL | Request, init?: RequestInit) => {
      seen = new Headers(init?.headers);
      return Response.json({ id: "r1", name: "redditor", icon_img: "https://i.redd.it/a.png?width=256" });
    };
    const raw = await p.userinfo!.request!({ tokens, provider: p, fetch: stub as typeof fetch });
    expect(seen!.get("user-agent")).toBeTruthy();
    expect(seen!.get("authorization")).toBe("Bearer tok");
    expect((await p.profile!(raw, tokens)).image).toBe("https://i.redd.it/a.png");
  });

  it("yandex and workos build their urls", async () => {
    const y = yandex({ clientId: "x" }) as OAuthConfig;
    expect(await y.profile!({ id: "1", real_name: "R", default_email: "r@y.ru", default_avatar_id: "zz", is_avatar_empty: false }, tokens)).toMatchObject({ image: "https://avatars.yandex.net/get-yapic/zz/islands-200" });
    expect((await y.profile!({ id: "1", is_avatar_empty: true }, tokens)).image).toBeNull();
    expect((workos({ clientId: "x", connection: "conn_1" }) as OAuthConfig).authorization.params).toEqual({ connection: "conn_1" });
  });
});

describe("declarative providers", () => {
  it("resolves built-in ids with options", () => {
    setEnv("AUTH_GITHUB_ID", "decl-id");
    const p = resolveDeclarativeProvider({ id: "github", options: { scope: "repo" } }) as OAuthConfig;
    expect(p.clientId).toBe("decl-id");
    expect(p.authorization.params?.scope).toBe("repo");
  });

  it("renames providers", () => {
    expect(resolveDeclarativeProvider({ id: "github", name: "Company GitHub" })?.name).toBe("Company GitHub");
  });

  it("builds custom oauth and oidc providers from options", () => {
    const o = resolveDeclarativeProvider({ id: "acme", type: "oauth", options: { authorization: "https://acme.test/a", token: "https://acme.test/t", userinfo: "https://acme.test/u", clientId: "c" } }) as OAuthConfig;
    expect(o).toMatchObject({ id: "acme", type: "oauth", clientId: "c" });
    expect(o.authorization.url).toBe("https://acme.test/a");
    const i = resolveDeclarativeProvider({ id: "sso", type: "oidc", options: { issuer: "https://sso.test" } }) as OAuthConfig;
    expect(i.type).toBe("oidc");
    expect(i.authorization.params?.scope).toBe("openid profile email");
  });

  it("handles passkey and email providers and refuses credentials and unknown ids", () => {
    expect(resolveDeclarativeProvider({ id: "passkey" })?.type).toBe("passkey");
    expect(resolveDeclarativeProvider({ id: "email", type: "email" })?.type).toBe("email");
    expect(resolveDeclarativeProvider({ id: "credentials" })).toBeNull();
    expect(resolveDeclarativeProvider({ id: "no-such-provider" })).toBeNull();
  });
});

describe("apple client secret", () => {
  it("signs an ES256 jwt Apple accepts", () => {
    const { privateKey, publicKey } = generateKeyPairSync("ec", { namedCurve: "prime256v1" });
    const pem = privateKey.export({ type: "pkcs8", format: "pem" }).toString();
    const now = Date.now();
    const jwt = appleClientSecret({ teamId: "TEAM123", keyId: "KEY456", clientId: "com.example.app", privateKey: pem, now });
    const [h, p, s] = jwt.split(".") as [string, string, string];
    expect(JSON.parse(b64urlDecodeString(h))).toEqual({ alg: "ES256", kid: "KEY456", typ: "JWT" });
    const claims = JSON.parse(b64urlDecodeString(p));
    expect(claims).toMatchObject({ iss: "TEAM123", aud: "https://appleid.apple.com", sub: "com.example.app" });
    expect(claims.exp - claims.iat).toBe(15_777_000);
    const ok = verify("sha256", Buffer.from(`${h}.${p}`), { key: publicKey, dsaEncoding: "ieee-p1363" }, Buffer.from(b64urlDecode(s)));
    expect(ok).toBe(true);
  });

  it("accepts a single-line pem with escaped newlines and rejects other keys", () => {
    const { privateKey } = generateKeyPairSync("ec", { namedCurve: "prime256v1" });
    const pem = privateKey.export({ type: "pkcs8", format: "pem" }).toString().replace(/\n/g, "\\n");
    expect(appleClientSecret({ teamId: "T", keyId: "K", clientId: "c", privateKey: pem }).split(".")).toHaveLength(3);
    expect(() => appleClientSecret({ teamId: "T", keyId: "K", clientId: "c", privateKey: "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----" })).toThrow("PKCS8");
  });
});
