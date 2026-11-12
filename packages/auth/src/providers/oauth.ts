import { readEnv } from "../env";
import type { Awaitable, EndpointContext, OAuthConfig, OAuthTokens, User } from "../types";

export interface OAuthUserOptions {
  clientId?: string;
  clientSecret?: string;
  issuer?: string;
  name?: string;
  scope?: string;
  authorization?: string | { url?: string; params?: Record<string, string> };
  token?: string | { url?: string; params?: Record<string, string> };
  userinfo?: string | { url?: string; params?: Record<string, string> };
  checks?: Array<"pkce" | "state" | "nonce" | "none">;
  client?: { token_endpoint_auth_method?: "client_secret_basic" | "client_secret_post" | "none" };
  profile?: (profile: Record<string, any>, tokens: OAuthTokens) => Awaitable<User>;
  allowDangerousEmailAccountLinking?: boolean;
  wellKnown?: string;
  [key: string]: unknown;
}

export interface ResolvedOAuthOptions extends OAuthUserOptions {
  clientId?: string;
  clientSecret?: string;
  issuer?: string;
}

type Resolvable<T> = T | ((options: ResolvedOAuthOptions) => T);

export interface OAuthSpec {
  id: string;
  name: string;
  type?: "oauth" | "oidc";
  issuer?: Resolvable<string | undefined>;
  authorization?: Resolvable<string | { url: string; params?: Record<string, string> } | undefined>;
  token?: Resolvable<string | { url: string; params?: Record<string, string> } | undefined>;
  userinfo?: Resolvable<string | { url: string; params?: Record<string, string> } | undefined>;
  wellKnown?: Resolvable<string | undefined>;
  scope?: string;
  checks?: Array<"pkce" | "state" | "nonce" | "none">;
  authMethod?: "client_secret_basic" | "client_secret_post" | "none";
  profile?: (profile: Record<string, any>, tokens: OAuthTokens) => Awaitable<User>;
  userinfoFromIdToken?: boolean;
  skipIssuerCheck?: Resolvable<boolean>;
  brandColor?: string;
  userinfoRequest?: (options: ResolvedOAuthOptions) => ((context: EndpointContext) => Awaitable<Record<string, any>>) | undefined;
}

export function envKey(id: string): string {
  return id.toUpperCase().replace(/[^A-Z0-9]+/g, "_");
}

export function envCredentials(id: string): { clientId?: string; clientSecret?: string; issuer?: string } {
  const key = envKey(id);
  return {
    clientId: readEnv(`AUTH_${key}_ID`, `${key}_CLIENT_ID`),
    clientSecret: readEnv(`AUTH_${key}_SECRET`, `${key}_CLIENT_SECRET`),
    issuer: readEnv(`AUTH_${key}_ISSUER`, `${key}_ISSUER`),
  };
}

function resolve<T>(value: Resolvable<T>, options: ResolvedOAuthOptions): T {
  return typeof value === "function" ? (value as (o: ResolvedOAuthOptions) => T)(options) : value;
}

function endpoint(
  value: string | { url?: string; params?: Record<string, string> } | undefined,
): { url?: string; params?: Record<string, string> } {
  if (value === undefined) return {};
  return typeof value === "string" ? { url: value } : { ...value };
}

export function defineOAuthProvider(spec: OAuthSpec): (options?: OAuthUserOptions) => OAuthConfig {
  return (userOptions: OAuthUserOptions = {}): OAuthConfig => {
    const env = envCredentials(spec.id);
    const options: ResolvedOAuthOptions = {
      ...userOptions,
      clientId: userOptions.clientId ?? env.clientId,
      clientSecret: userOptions.clientSecret ?? env.clientSecret,
      issuer: userOptions.issuer ?? env.issuer,
    };
    const authorization = endpoint(userOptions.authorization ?? resolve(spec.authorization, options));
    const tokenEndpoint = endpoint(userOptions.token ?? resolve(spec.token, options));
    const userinfo = endpoint(userOptions.userinfo ?? resolve(spec.userinfo, options));
    const scope = userOptions.scope ?? spec.scope;
    if (scope) authorization.params = { ...(authorization.params ?? {}), scope };
    const issuer = options.issuer ?? resolve(spec.issuer, options);
    const wellKnown = userOptions.wellKnown ?? resolve(spec.wellKnown, options);
    const type = spec.type ?? (issuer || wellKnown ? "oidc" : "oauth");
    return {
      id: spec.id,
      name: userOptions.name ?? spec.name,
      type,
      issuer,
      wellKnown,
      authorization,
      token: tokenEndpoint,
      userinfo: userinfo.url || spec.userinfoRequest ? { ...userinfo, request: spec.userinfoRequest?.(options) } : undefined,
      skipIssuerCheck: spec.skipIssuerCheck === undefined ? undefined : resolve(spec.skipIssuerCheck, options),
      clientId: options.clientId,
      clientSecret: options.clientSecret,
      checks: userOptions.checks ?? spec.checks ?? (type === "oidc" ? ["pkce", "state", "nonce"] : ["pkce", "state"]),
      client: userOptions.client ?? (spec.authMethod ? { token_endpoint_auth_method: spec.authMethod } : undefined),
      profile: userOptions.profile ?? spec.profile ?? defaultProfile,
      allowDangerousEmailAccountLinking: userOptions.allowDangerousEmailAccountLinking,
      userinfoFromIdToken: spec.userinfoFromIdToken,
      style: spec.brandColor ? { brandColor: spec.brandColor } : undefined,
      options: userOptions,
    };
  };
}

export function defaultProfile(profile: Record<string, any>): User {
  return {
    id: String(profile.sub ?? profile.id ?? ""),
    name: profile.name ?? profile.preferred_username ?? profile.nickname ?? null,
    email: profile.email ?? null,
    image: profile.picture ?? profile.avatar_url ?? null,
  };
}

export function genericOAuth(options: OAuthUserOptions & { id: string; name?: string; type?: "oauth" | "oidc" }): OAuthConfig {
  const { id, name, type, ...rest } = options;
  const env = envCredentials(id);
  const issuer = rest.issuer ?? env.issuer;
  const resolvedType = type ?? (issuer || rest.wellKnown ? "oidc" : "oauth");
  const authorization = endpoint(rest.authorization);
  if (rest.scope) authorization.params = { ...(authorization.params ?? {}), scope: rest.scope };
  else if (resolvedType === "oidc" && !authorization.params?.scope) {
    authorization.params = { ...(authorization.params ?? {}), scope: "openid profile email" };
  }
  return {
    id,
    name: name ?? id,
    type: resolvedType,
    issuer,
    wellKnown: rest.wellKnown,
    authorization,
    token: endpoint(rest.token),
    userinfo: rest.userinfo ? endpoint(rest.userinfo) : undefined,
    clientId: rest.clientId ?? env.clientId,
    clientSecret: rest.clientSecret ?? env.clientSecret,
    checks: rest.checks ?? (resolvedType === "oidc" ? ["pkce", "state", "nonce"] : ["pkce", "state"]),
    client: rest.client,
    profile: rest.profile ?? defaultProfile,
    allowDangerousEmailAccountLinking: rest.allowDangerousEmailAccountLinking,
    options: rest,
  };
}
