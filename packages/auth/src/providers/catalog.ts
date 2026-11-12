import { defineOAuthProvider, type OAuthSpec, type ResolvedOAuthOptions } from "./oauth";
import type { EndpointContext, OAuthConfig, Provider, User } from "../types";

type Factory = ReturnType<typeof defineOAuthProvider>;

const base = (o: ResolvedOAuthOptions): string | undefined => o.issuer?.replace(/\/+$/, "");
const at = (path: string) => (o: ResolvedOAuthOptions): string | undefined => (base(o) ? `${base(o)}${path}` : undefined);
const join = (...parts: Array<unknown>): string | null => {
  const text = parts.filter((p) => typeof p === "string" && p.length > 0).join(" ").trim();
  return text.length > 0 ? text : null;
};

async function getJson(context: EndpointContext, url: string, headers: Record<string, string>, init: RequestInit = {}): Promise<any> {
  const res = await context.fetch(url, { ...init, headers: { accept: "application/json", ...headers } });
  if (!res.ok) throw new Error(`${new URL(url).host} responded with ${res.status}`);
  return res.json();
}

const bearer = (token: string | undefined): Record<string, string> => ({ authorization: `Bearer ${token}` });

function oidc(id: string, name: string, extra: Partial<OAuthSpec> = {}): Factory {
  return defineOAuthProvider({
    id,
    name,
    type: "oidc",
    issuer: (o) => o.issuer,
    scope: "openid profile email",
    profile: (p) => ({ id: p.sub, name: p.name ?? p.preferred_username ?? p.nickname ?? null, email: p.email ?? null, image: p.picture ?? null }),
    ...extra,
  });
}

export const github = defineOAuthProvider({
  id: "github",
  name: "GitHub",
  authorization: "https://github.com/login/oauth/authorize",
  token: "https://github.com/login/oauth/access_token",
  userinfo: "https://api.github.com/user",
  scope: "read:user user:email",
  brandColor: "#24292f",
  userinfoRequest: () => async (context) => {
    const headers = { ...bearer(context.tokens.access_token), accept: "application/vnd.github+json", "user-agent": "krate-auth" };
    const profile = await getJson(context, "https://api.github.com/user", headers);
    if (!profile.email) {
      const emails = await getJson(context, "https://api.github.com/user/emails", headers).catch(() => []);
      const list = Array.isArray(emails) ? emails : [];
      const chosen = list.find((e: any) => e.primary && e.verified) ?? list.find((e: any) => e.verified);
      if (chosen) {
        profile.email = chosen.email;
        profile.email_verified = true;
      }
    }
    return profile;
  },
  profile: (p) => ({ id: String(p.id), name: p.name ?? p.login ?? null, email: p.email ?? null, image: p.avatar_url ?? null, emailVerified: p.email_verified ? new Date() : null }),
});

export const google = defineOAuthProvider({
  id: "google",
  name: "Google",
  type: "oidc",
  issuer: "https://accounts.google.com",
  authorization: { url: "https://accounts.google.com/o/oauth2/v2/auth", params: { prompt: "consent", access_type: "offline", response_type: "code" } },
  scope: "openid email profile",
  brandColor: "#4285f4",
  profile: (p) => ({ id: p.sub, name: p.name ?? null, email: p.email ?? null, image: p.picture ?? null, emailVerified: p.email_verified ? new Date() : null }),
});

export const gitlab = defineOAuthProvider({
  id: "gitlab",
  name: "GitLab",
  authorization: (o) => `${base(o) ?? "https://gitlab.com"}/oauth/authorize`,
  token: (o) => `${base(o) ?? "https://gitlab.com"}/oauth/token`,
  userinfo: (o) => `${base(o) ?? "https://gitlab.com"}/api/v4/user`,
  type: "oauth",
  scope: "read_user",
  brandColor: "#fc6d26",
  profile: (p) => ({ id: String(p.id), name: p.name ?? p.username ?? null, email: p.email ?? null, image: p.avatar_url ?? null }),
});

export const bitbucket = defineOAuthProvider({
  id: "bitbucket",
  name: "Bitbucket",
  authorization: "https://bitbucket.org/site/oauth2/authorize",
  token: "https://bitbucket.org/site/oauth2/access_token",
  userinfo: "https://api.bitbucket.org/2.0/user",
  scope: "account email",
  userinfoRequest: () => async (context) => {
    const headers = bearer(context.tokens.access_token);
    const profile = await getJson(context, "https://api.bitbucket.org/2.0/user", headers);
    const emails = await getJson(context, "https://api.bitbucket.org/2.0/user/emails", headers).catch(() => ({ values: [] }));
    const primary = (emails.values ?? []).find((e: any) => e.is_primary && e.is_confirmed);
    if (primary) profile.email = primary.email;
    return profile;
  },
  profile: (p) => ({ id: p.uuid ?? p.account_id, name: p.display_name ?? p.nickname ?? null, email: p.email ?? null, image: p.links?.avatar?.href ?? null }),
});

export const discord = defineOAuthProvider({
  id: "discord",
  name: "Discord",
  authorization: "https://discord.com/api/oauth2/authorize",
  token: "https://discord.com/api/oauth2/token",
  userinfo: "https://discord.com/api/users/@me",
  scope: "identify email",
  brandColor: "#5865f2",
  profile: (p) => {
    let image: string;
    if (p.avatar === null || p.avatar === undefined) {
      const index = p.discriminator === "0" ? Number((BigInt(p.id) >> 22n) % 6n) : Number(p.discriminator) % 5;
      image = `https://cdn.discordapp.com/embed/avatars/${index}.png`;
    } else {
      image = `https://cdn.discordapp.com/avatars/${p.id}/${p.avatar}.${String(p.avatar).startsWith("a_") ? "gif" : "png"}`;
    }
    return { id: p.id, name: p.global_name ?? p.username ?? null, email: p.email ?? null, image, emailVerified: p.verified ? new Date() : null };
  },
});

export const facebook = defineOAuthProvider({
  id: "facebook",
  name: "Facebook",
  authorization: "https://www.facebook.com/v19.0/dialog/oauth",
  token: "https://graph.facebook.com/oauth/access_token",
  userinfo: { url: "https://graph.facebook.com/me", params: { fields: "id,name,email,picture" } },
  scope: "email",
  authMethod: "client_secret_post",
  brandColor: "#1877f2",
  profile: (p) => ({ id: p.id, name: p.name ?? null, email: p.email ?? null, image: p.picture?.data?.url ?? null }),
});

export const twitter = defineOAuthProvider({
  id: "twitter",
  name: "Twitter",
  authorization: "https://twitter.com/i/oauth2/authorize",
  token: "https://api.twitter.com/2/oauth2/token",
  userinfo: { url: "https://api.twitter.com/2/users/me", params: { "user.fields": "profile_image_url" } },
  scope: "users.read tweet.read offline.access",
  profile: (p) => {
    const d = p.data ?? p;
    return { id: d.id, name: d.name ?? d.username ?? null, email: null, image: d.profile_image_url ?? null };
  },
});

export const linkedin = oidc("linkedin", "LinkedIn", { issuer: "https://www.linkedin.com/oauth", authMethod: "client_secret_post", scope: "openid profile email" });
export const slack = oidc("slack", "Slack", { issuer: "https://slack.com" });
export const line = oidc("line", "LINE", { issuer: "https://access.line.me", authMethod: "client_secret_post" });
export const roblox = oidc("roblox", "Roblox", { issuer: "https://apis.roblox.com/oauth/", scope: "openid profile", authMethod: "client_secret_post" });
export const huggingface = oidc("huggingface", "Hugging Face", { issuer: "https://huggingface.co" });
export const orcid = oidc("orcid", "ORCID", { issuer: "https://orcid.org", scope: "openid" });

export const twitch = defineOAuthProvider({
  id: "twitch",
  name: "Twitch",
  type: "oidc",
  issuer: "https://id.twitch.tv/oauth2",
  authorization: { url: "https://id.twitch.tv/oauth2/authorize", params: { claims: JSON.stringify({ id_token: { email: null, picture: null, preferred_username: null } }) } },
  scope: "openid user:read:email",
  authMethod: "client_secret_post",
  profile: (p) => ({ id: p.sub, name: p.preferred_username ?? null, email: p.email ?? null, image: p.picture ?? null }),
});

export const microsoftEntraId = defineOAuthProvider({
  id: "microsoft-entra-id",
  name: "Microsoft Entra ID",
  type: "oidc",
  issuer: (o) => o.issuer ?? `https://login.microsoftonline.com/${(o.tenantId as string | undefined) ?? "common"}/v2.0`,
  scope: "openid profile email User.Read",
  skipIssuerCheck: (o) => !o.issuer && ["common", "organizations", "consumers"].includes(String(o.tenantId ?? "common")),
  authMethod: "client_secret_post",
  profile: (p) => ({ id: p.sub, name: p.name ?? null, email: p.email ?? p.preferred_username ?? null, image: null }),
});

export const azureAdB2c = oidc("azure-ad-b2c", "Azure AD B2C", { authMethod: "client_secret_post" });
export const auth0 = oidc("auth0", "Auth0");
export const authentik = oidc("authentik", "Authentik");
export const asgardeo = oidc("asgardeo", "Asgardeo");
export const cognito = oidc("cognito", "Cognito", { authMethod: "client_secret_post" });
export const duendeIdentityServer6 = oidc("duende-identityserver6", "IdentityServer6");
export const fusionauth = oidc("fusionauth", "FusionAuth");
export const keycloak = oidc("keycloak", "Keycloak");
export const kinde = oidc("kinde", "Kinde");
export const logto = oidc("logto", "Logto");
export const okta = oidc("okta", "Okta");
export const onelogin = oidc("onelogin", "OneLogin", { issuer: (o) => (base(o) ? `${base(o)}/oidc/2` : undefined) });
export const zitadel = oidc("zitadel", "ZITADEL");
export const descope = oidc("descope", "Descope");

export const apple = defineOAuthProvider({
  id: "apple",
  name: "Apple",
  type: "oidc",
  issuer: "https://appleid.apple.com",
  authorization: { url: "https://appleid.apple.com/auth/authorize", params: { response_mode: "form_post" } },
  token: "https://appleid.apple.com/auth/token",
  scope: "name email",
  authMethod: "client_secret_post",
  profile: (p) => ({ id: p.sub, name: join(p.name?.firstName, p.name?.lastName) ?? p.name ?? null, email: p.email ?? null, image: null, emailVerified: p.email_verified === true || p.email_verified === "true" ? new Date() : null }),
});

export const atlassian = defineOAuthProvider({
  id: "atlassian",
  name: "Atlassian",
  authorization: { url: "https://auth.atlassian.com/authorize", params: { audience: "api.atlassian.com", prompt: "consent" } },
  token: "https://auth.atlassian.com/oauth/token",
  userinfo: "https://api.atlassian.com/me",
  scope: "read:me",
  profile: (p) => ({ id: p.account_id, name: p.name ?? null, email: p.email ?? null, image: p.picture ?? null }),
});

export const box = defineOAuthProvider({
  id: "box",
  name: "Box",
  authorization: "https://account.box.com/api/oauth2/authorize",
  token: "https://api.box.com/oauth2/token",
  userinfo: "https://api.box.com/2.0/users/me",
  authMethod: "client_secret_post",
  profile: (p) => ({ id: p.id, name: p.name ?? null, email: p.login ?? null, image: p.avatar_url ?? null }),
});

export const coinbase = defineOAuthProvider({
  id: "coinbase",
  name: "Coinbase",
  authorization: "https://login.coinbase.com/oauth2/auth",
  token: "https://login.coinbase.com/oauth2/token",
  userinfo: "https://api.coinbase.com/v2/user",
  scope: "wallet:user:read wallet:user:email",
  authMethod: "client_secret_post",
  profile: (p) => ({ id: p.data?.id, name: p.data?.name ?? null, email: p.data?.email ?? null, image: p.data?.avatar_url ?? null }),
});

export const dribbble = defineOAuthProvider({
  id: "dribbble",
  name: "Dribbble",
  authorization: "https://dribbble.com/oauth/authorize",
  token: "https://dribbble.com/oauth/token",
  userinfo: "https://api.dribbble.com/v2/user",
  scope: "public",
  authMethod: "client_secret_post",
  profile: (p) => ({ id: String(p.id), name: p.name ?? p.login ?? null, email: p.email ?? null, image: p.avatar_url ?? null }),
});

export const dropbox = defineOAuthProvider({
  id: "dropbox",
  name: "Dropbox",
  authorization: { url: "https://www.dropbox.com/oauth2/authorize", params: { token_access_type: "offline" } },
  token: "https://api.dropboxapi.com/oauth2/token",
  scope: "account_info.read",
  authMethod: "client_secret_post",
  userinfoRequest: () => (context) =>
    getJson(context, "https://api.dropboxapi.com/2/users/get_current_account", bearer(context.tokens.access_token), { method: "POST" }),
  profile: (p) => ({ id: p.account_id, name: p.name?.display_name ?? null, email: p.email ?? null, image: p.profile_photo_url ?? null, emailVerified: p.email_verified ? new Date() : null }),
});

export const notion = defineOAuthProvider({
  id: "notion",
  name: "Notion",
  authorization: { url: "https://api.notion.com/v1/oauth/authorize", params: { owner: "user" } },
  token: "https://api.notion.com/v1/oauth/token",
  userinfoRequest: () => async ({ tokens }) => (tokens.owner as { user?: Record<string, any> } | undefined)?.user ?? {},
  profile: (p) => ({ id: p.id, name: p.name ?? null, email: p.person?.email ?? null, image: p.avatar_url ?? null }),
});

export const reddit = defineOAuthProvider({
  id: "reddit",
  name: "Reddit",
  authorization: { url: "https://www.reddit.com/api/v1/authorize", params: { duration: "permanent" } },
  token: "https://www.reddit.com/api/v1/access_token",
  scope: "identity",
  userinfoRequest: () => (context) =>
    getJson(context, "https://oauth.reddit.com/api/v1/me", { ...bearer(context.tokens.access_token), "user-agent": "krate-auth" }),
  profile: (p) => ({ id: p.id, name: p.name ?? null, email: null, image: typeof p.icon_img === "string" ? p.icon_img.split("?")[0] : null }),
});

export const patreon = defineOAuthProvider({
  id: "patreon",
  name: "Patreon",
  authorization: "https://www.patreon.com/oauth2/authorize",
  token: "https://www.patreon.com/api/oauth2/token",
  userinfo: { url: "https://www.patreon.com/api/oauth2/v2/identity", params: { "fields[user]": "full_name,email,image_url" } },
  scope: "identity identity[email]",
  authMethod: "client_secret_post",
  profile: (p) => ({ id: p.data?.id, name: p.data?.attributes?.full_name ?? null, email: p.data?.attributes?.email ?? null, image: p.data?.attributes?.image_url ?? null }),
});

export const spotify = defineOAuthProvider({
  id: "spotify",
  name: "Spotify",
  authorization: "https://accounts.spotify.com/authorize",
  token: "https://accounts.spotify.com/api/token",
  userinfo: "https://api.spotify.com/v1/me",
  scope: "user-read-email",
  profile: (p) => ({ id: p.id, name: p.display_name ?? null, email: p.email ?? null, image: p.images?.[0]?.url ?? null }),
});

export const strava = defineOAuthProvider({
  id: "strava",
  name: "Strava",
  authorization: { url: "https://www.strava.com/api/v3/oauth/authorize", params: { approval_prompt: "auto" } },
  token: "https://www.strava.com/api/v3/oauth/token",
  userinfo: "https://www.strava.com/api/v3/athlete",
  scope: "read",
  authMethod: "client_secret_post",
  profile: (p) => ({ id: String(p.id), name: join(p.firstname, p.lastname), email: null, image: p.profile ?? null }),
});

export const osu = defineOAuthProvider({
  id: "osu",
  name: "osu!",
  authorization: "https://osu.ppy.sh/oauth/authorize",
  token: "https://osu.ppy.sh/oauth/token",
  userinfo: "https://osu.ppy.sh/api/v2/me",
  scope: "identify",
  authMethod: "client_secret_post",
  profile: (p) => ({ id: String(p.id), name: p.username ?? null, email: null, image: p.avatar_url ?? null }),
});

export const pinterest = defineOAuthProvider({
  id: "pinterest",
  name: "Pinterest",
  authorization: "https://www.pinterest.com/oauth/",
  token: "https://api.pinterest.com/v5/oauth/token",
  userinfo: "https://api.pinterest.com/v5/user_account",
  scope: "user_accounts:read",
  profile: (p) => ({ id: p.username, name: p.username ?? null, email: null, image: p.profile_image ?? null }),
});

export const zoom = defineOAuthProvider({
  id: "zoom",
  name: "Zoom",
  authorization: "https://zoom.us/oauth/authorize",
  token: "https://zoom.us/oauth/token",
  userinfo: "https://api.zoom.us/v2/users/me",
  profile: (p) => ({ id: p.id, name: join(p.first_name, p.last_name), email: p.email ?? null, image: p.pic_url ?? null }),
});

export const zoho = defineOAuthProvider({
  id: "zoho",
  name: "Zoho",
  authorization: { url: "https://accounts.zoho.com/oauth/v2/auth", params: { access_type: "offline" } },
  token: "https://accounts.zoho.com/oauth/v2/token",
  userinfo: "https://accounts.zoho.com/oauth/user/info",
  scope: "AaaServer.profile.READ",
  authMethod: "client_secret_post",
  profile: (p) => ({ id: p.ZUID, name: join(p.First_Name, p.Last_Name) ?? p.Display_Name ?? null, email: p.Email ?? null, image: null }),
});

export const webex = defineOAuthProvider({
  id: "webex",
  name: "Webex",
  authorization: "https://webexapis.com/v1/authorize",
  token: "https://webexapis.com/v1/access_token",
  userinfo: "https://webexapis.com/v1/people/me",
  scope: "spark:kms spark:people_read",
  authMethod: "client_secret_post",
  profile: (p) => ({ id: p.id, name: p.displayName ?? null, email: p.emails?.[0] ?? null, image: p.avatar ?? null }),
});

export const wikimedia = defineOAuthProvider({
  id: "wikimedia",
  name: "Wikimedia",
  authorization: "https://meta.wikimedia.org/w/rest.php/oauth2/authorize",
  token: "https://meta.wikimedia.org/w/rest.php/oauth2/access_token",
  userinfo: "https://meta.wikimedia.org/w/rest.php/oauth2/resource/profile",
  authMethod: "client_secret_post",
  profile: (p) => ({ id: p.sub, name: p.username ?? null, email: p.email ?? null, image: null }),
});

export const salesforce = defineOAuthProvider({
  id: "salesforce",
  name: "Salesforce",
  authorization: { url: "https://login.salesforce.com/services/oauth2/authorize", params: { display: "page" } },
  token: "https://login.salesforce.com/services/oauth2/token",
  userinfo: "https://login.salesforce.com/services/oauth2/userinfo",
  scope: "openid email profile",
  authMethod: "client_secret_post",
  profile: (p) => ({ id: p.user_id, name: p.name ?? null, email: p.email ?? null, image: p.picture ?? null }),
});

export const hubspot = defineOAuthProvider({
  id: "hubspot",
  name: "HubSpot",
  authorization: "https://app.hubspot.com/oauth/authorize",
  token: "https://api.hubapi.com/oauth/v1/token",
  scope: "oauth",
  authMethod: "client_secret_post",
  userinfoRequest: () => (context) =>
    getJson(context, `https://api.hubapi.com/oauth/v1/access-tokens/${encodeURIComponent(String(context.tokens.access_token))}`, {}),
  profile: (p) => ({ id: String(p.user_id), name: p.user ?? null, email: p.user ?? null, image: null }),
});

export const netlify = defineOAuthProvider({
  id: "netlify",
  name: "Netlify",
  authorization: "https://app.netlify.com/authorize",
  token: "https://api.netlify.com/oauth/token",
  userinfo: "https://api.netlify.com/api/v1/user",
  authMethod: "client_secret_post",
  profile: (p) => ({ id: p.id, name: p.full_name ?? null, email: p.email ?? null, image: p.avatar_url ?? null }),
});

export const kakao = defineOAuthProvider({
  id: "kakao",
  name: "Kakao",
  authorization: "https://kauth.kakao.com/oauth/authorize",
  token: "https://kauth.kakao.com/oauth/token",
  userinfo: "https://kapi.kakao.com/v2/user/me",
  authMethod: "client_secret_post",
  profile: (p) => ({ id: String(p.id), name: p.kakao_account?.profile?.nickname ?? null, email: p.kakao_account?.email ?? null, image: p.kakao_account?.profile?.profile_image_url ?? null }),
});

export const naver = defineOAuthProvider({
  id: "naver",
  name: "Naver",
  authorization: "https://nid.naver.com/oauth2.0/authorize",
  token: "https://nid.naver.com/oauth2.0/token",
  userinfo: "https://openapi.naver.com/v1/nid/me",
  authMethod: "client_secret_post",
  profile: (p) => ({ id: p.response?.id, name: p.response?.name ?? p.response?.nickname ?? null, email: p.response?.email ?? null, image: p.response?.profile_image ?? null }),
});

export const yandex = defineOAuthProvider({
  id: "yandex",
  name: "Yandex",
  authorization: "https://oauth.yandex.com/authorize",
  token: "https://oauth.yandex.com/token",
  userinfo: { url: "https://login.yandex.ru/info", params: { format: "json" } },
  scope: "login:email login:info login:avatar",
  authMethod: "client_secret_post",
  profile: (p) => ({
    id: p.id,
    name: p.real_name ?? p.display_name ?? null,
    email: p.default_email ?? null,
    image: p.is_avatar_empty ? null : `https://avatars.yandex.net/get-yapic/${p.default_avatar_id}/islands-200`,
  }),
});

export const mastodon = defineOAuthProvider({
  id: "mastodon",
  name: "Mastodon",
  type: "oauth",
  authorization: at("/oauth/authorize"),
  token: at("/oauth/token"),
  userinfo: at("/api/v1/accounts/verify_credentials"),
  scope: "read:accounts",
  authMethod: "client_secret_post",
  profile: (p) => ({ id: p.id, name: p.display_name || p.username || null, email: null, image: p.avatar ?? null }),
});

export const mattermost = defineOAuthProvider({
  id: "mattermost",
  name: "Mattermost",
  type: "oauth",
  authorization: at("/oauth/authorize"),
  token: at("/oauth/access_token"),
  userinfo: at("/api/v4/users/me"),
  profile: (p) => ({ id: p.id, name: p.username ?? null, email: p.email ?? null, image: null }),
});

export const workos = defineOAuthProvider({
  id: "workos",
  name: "WorkOS",
  authorization: (o) => {
    const params: Record<string, string> = {};
    if (o.connection) params.connection = String(o.connection);
    else if (o.organization) params.organization = String(o.organization);
    return { url: "https://api.workos.com/sso/authorize", params };
  },
  token: "https://api.workos.com/sso/token",
  userinfo: "https://api.workos.com/sso/profile",
  authMethod: "client_secret_post",
  profile: (p) => ({ id: p.id, name: join(p.first_name, p.last_name), email: p.email ?? null, image: null }),
});

export const fortyTwoSchool = defineOAuthProvider({
  id: "42-school",
  name: "42 School",
  authorization: { url: "https://api.intra.42.fr/oauth/authorize", params: { scope: "public" } },
  token: "https://api.intra.42.fr/oauth/token",
  userinfo: "https://api.intra.42.fr/v2/me",
  authMethod: "client_secret_post",
  profile: (p) => ({ id: String(p.id), name: p.usual_full_name ?? p.login ?? null, email: p.email ?? null, image: p.image?.link ?? null }),
});

export const catalog: Record<string, (options?: any) => Provider> = {
  "42-school": fortyTwoSchool,
  apple,
  asgardeo,
  atlassian,
  auth0,
  authentik,
  "azure-ad-b2c": azureAdB2c,
  bitbucket,
  box,
  cognito,
  coinbase,
  descope,
  discord,
  dribbble,
  dropbox,
  "duende-identityserver6": duendeIdentityServer6,
  facebook,
  fusionauth,
  github,
  gitlab,
  google,
  hubspot,
  huggingface,
  kakao,
  keycloak,
  kinde,
  line,
  linkedin,
  logto,
  mastodon,
  mattermost,
  "microsoft-entra-id": microsoftEntraId,
  naver,
  netlify,
  notion,
  okta,
  onelogin,
  orcid,
  osu,
  patreon,
  pinterest,
  reddit,
  roblox,
  salesforce,
  slack,
  spotify,
  strava,
  twitch,
  twitter,
  webex,
  wikimedia,
  workos,
  yandex,
  zitadel,
  zoho,
  zoom,
};

export type { OAuthConfig, User };
