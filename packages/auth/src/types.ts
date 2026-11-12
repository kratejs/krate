import type { CookieOptions } from "./cookies";
import type { Secret, TokenClaims } from "./crypto";

export type Awaitable<T> = T | Promise<T>;

export interface User {
  id?: string;
  name?: string | null;
  email?: string | null;
  image?: string | null;
  role?: string | null;
  [key: string]: unknown;
}

export interface AdapterUser extends User {
  id: string;
  email: string | null;
  emailVerified: Date | null;
  twoFactorEnabled?: boolean;
}

export type AccountType = "oauth" | "oidc" | "email" | "credentials" | "webauthn";

export interface Account {
  userId?: string;
  type: AccountType;
  provider: string;
  providerAccountId: string;
  refresh_token?: string | null;
  access_token?: string | null;
  expires_at?: number | null;
  token_type?: string | null;
  scope?: string | null;
  id_token?: string | null;
  session_state?: string | null;
  [key: string]: unknown;
}

export interface AdapterSession {
  sessionToken: string;
  userId: string;
  expires: Date;
}

export interface VerificationToken {
  identifier: string;
  token: string;
  expires: Date;
}

export interface Authenticator {
  credentialID: string;
  userId: string;
  providerAccountId: string;
  credentialPublicKey: string;
  counter: number;
  credentialDeviceType: "singleDevice" | "multiDevice";
  credentialBackedUp: boolean;
  transports?: string | null;
  name?: string | null;
  createdAt?: Date;
}

export interface TwoFactorRecord {
  userId: string;
  secret: string;
  backupCodes: string[];
  confirmed: boolean;
  createdAt?: Date;
}

export interface Adapter {
  createUser(user: Omit<AdapterUser, "id">): Awaitable<AdapterUser>;
  getUser(id: string): Awaitable<AdapterUser | null>;
  getUserByEmail(email: string): Awaitable<AdapterUser | null>;
  getUserByAccount(account: Pick<Account, "provider" | "providerAccountId">): Awaitable<AdapterUser | null>;
  updateUser(user: Partial<AdapterUser> & Pick<AdapterUser, "id">): Awaitable<AdapterUser>;
  deleteUser?(userId: string): Awaitable<void>;
  linkAccount(account: Account & { userId: string }): Awaitable<Account | void>;
  unlinkAccount?(account: Pick<Account, "provider" | "providerAccountId">): Awaitable<void>;
  getAccount?(providerAccountId: string, provider: string): Awaitable<Account | null>;
  listAccounts?(userId: string): Awaitable<Account[]>;
  createSession?(session: AdapterSession): Awaitable<AdapterSession>;
  getSessionAndUser?(sessionToken: string): Awaitable<{ session: AdapterSession; user: AdapterUser } | null>;
  updateSession?(session: Partial<AdapterSession> & Pick<AdapterSession, "sessionToken">): Awaitable<AdapterSession | null | void>;
  deleteSession?(sessionToken: string): Awaitable<void>;
  createVerificationToken?(token: VerificationToken): Awaitable<VerificationToken | null | void>;
  useVerificationToken?(params: { identifier: string; token: string }): Awaitable<VerificationToken | null>;
  getAuthenticator?(credentialID: string): Awaitable<Authenticator | null>;
  listAuthenticatorsByUserId?(userId: string): Awaitable<Authenticator[]>;
  createAuthenticator?(authenticator: Authenticator): Awaitable<Authenticator>;
  updateAuthenticatorCounter?(credentialID: string, counter: number): Awaitable<Authenticator>;
  deleteAuthenticator?(credentialID: string): Awaitable<void>;
  getTwoFactor?(userId: string): Awaitable<TwoFactorRecord | null>;
  upsertTwoFactor?(record: TwoFactorRecord): Awaitable<TwoFactorRecord>;
  deleteTwoFactor?(userId: string): Awaitable<void>;
}

export interface Session {
  user: {
    id: string;
    name?: string | null;
    email?: string | null;
    image?: string | null;
    role?: string | null;
    twoFactorEnabled?: boolean;
    [key: string]: unknown;
  };
  expires: string;
  [key: string]: unknown;
}

export interface EndpointContext {
  tokens: OAuthTokens;
  provider: OAuthConfig;
  fetch: typeof fetch;
}

export interface Endpoint {
  url?: string;
  params?: Record<string, string>;
  request?: (context: EndpointContext) => Awaitable<Record<string, any>>;
}

export interface OAuthConfig {
  id: string;
  name: string;
  type: "oauth" | "oidc";
  issuer?: string;
  wellKnown?: string;
  authorization: Endpoint;
  token: Endpoint;
  userinfo?: Endpoint;
  clientId?: string;
  clientSecret?: string;
  checks?: Array<"pkce" | "state" | "nonce" | "none">;
  client?: { token_endpoint_auth_method?: "client_secret_basic" | "client_secret_post" | "none" };
  profile?: (profile: Record<string, any>, tokens: OAuthTokens) => Awaitable<User>;
  allowDangerousEmailAccountLinking?: boolean;
  userinfoFromIdToken?: boolean;
  style?: { brandColor?: string };
  options?: Record<string, unknown>;
}

export interface OAuthTokens {
  access_token?: string;
  token_type?: string;
  expires_in?: number;
  expires_at?: number;
  refresh_token?: string;
  id_token?: string;
  scope?: string;
  session_state?: string;
  [key: string]: unknown;
}

export interface CredentialField {
  label?: string;
  type?: string;
  placeholder?: string;
}

export interface CredentialsConfig {
  id: string;
  name: string;
  type: "credentials";
  credentials: Record<string, CredentialField>;
  authorize: (credentials: Record<string, string>, request: Request) => Awaitable<User | null | undefined | false>;
}

export interface SendVerificationParams {
  identifier: string;
  url: string;
  token: string;
  expires: Date;
  provider: EmailConfig;
  request: Request;
}

export interface EmailConfig {
  id: string;
  name: string;
  type: "email";
  from?: string;
  maxAge: number;
  sendVerificationRequest: (params: SendVerificationParams) => Awaitable<void>;
  generateVerificationToken?: () => Awaitable<string>;
  normalizeIdentifier?: (identifier: string) => string;
  options?: Record<string, unknown>;
}

export interface PasskeyProviderConfig {
  id: "passkey";
  name: string;
  type: "passkey";
}

export type Provider = OAuthConfig | CredentialsConfig | EmailConfig | PasskeyProviderConfig;

export interface SignInCallbackParams {
  user: User | AdapterUser;
  account: Account | null;
  profile?: Record<string, any>;
  email?: { verificationRequest?: boolean };
  credentials?: Record<string, string>;
}

export interface JwtCallbackParams {
  token: TokenClaims;
  user?: User | AdapterUser;
  account?: Account | null;
  profile?: Record<string, any>;
  trigger?: "signIn" | "signUp" | "update";
  isNewUser?: boolean;
  session?: unknown;
}

export interface SessionCallbackParams {
  session: Session;
  token?: TokenClaims;
  user?: AdapterUser;
  trigger?: "update";
  newSession?: unknown;
}

export interface Callbacks {
  signIn?: (params: SignInCallbackParams) => Awaitable<boolean | string>;
  redirect?: (params: { url: string; baseUrl: string }) => Awaitable<string>;
  jwt?: (params: JwtCallbackParams) => Awaitable<TokenClaims | null>;
  session?: (params: SessionCallbackParams) => Awaitable<Session>;
  authorized?: (params: { request: Request; auth: Session | null }) => Awaitable<boolean | Response | undefined | void>;
}

export interface Events {
  signIn?: (message: { user: User; account: Account | null; profile?: Record<string, any>; isNewUser?: boolean }) => Awaitable<void>;
  signOut?: (message: { session?: Session | null; token?: TokenClaims | null }) => Awaitable<void>;
  createUser?: (message: { user: User }) => Awaitable<void>;
  updateUser?: (message: { user: User }) => Awaitable<void>;
  linkAccount?: (message: { user: User; account: Account; profile?: Record<string, any> }) => Awaitable<void>;
  session?: (message: { session: Session; token?: TokenClaims }) => Awaitable<void>;
  twoFactorEnabled?: (message: { user: User }) => Awaitable<void>;
  twoFactorDisabled?: (message: { user: User }) => Awaitable<void>;
  passkeyRegistered?: (message: { user: User; credentialID: string }) => Awaitable<void>;
}

export interface Logger {
  debug: (code: string, meta?: unknown) => void;
  warn: (code: string, meta?: unknown) => void;
  error: (code: string, meta?: unknown) => void;
}

export interface PagesOptions {
  signIn?: string;
  signOut?: string;
  error?: string;
  verifyRequest?: string;
  newUser?: string;
  twoFactor?: string;
}

export interface SessionOptions {
  strategy?: "jwt" | "database";
  maxAge?: number;
  updateAge?: number;
  generateSessionToken?: () => string;
}

export interface JwtOptions {
  maxAge?: number;
  encode?: (params: { token?: TokenClaims; secret: Secret; salt: string; maxAge?: number }) => Awaitable<string>;
  decode?: (params: { token?: string; secret: Secret; salt: string }) => Awaitable<TokenClaims | null>;
}

export interface TwoFactorOptions {
  enabled?: boolean;
  required?: boolean;
  issuer?: string;
  digits?: 6 | 7 | 8;
  period?: number;
  algorithm?: "SHA1" | "SHA256" | "SHA512";
  window?: number;
  backupCodes?: number;
}

export interface PasskeyOptions {
  enabled?: boolean;
  rpName?: string;
  rpId?: string;
  origins?: string[];
  userVerification?: "required" | "preferred" | "discouraged";
  residentKey?: "required" | "preferred" | "discouraged";
  attestation?: "none" | "indirect" | "direct" | "enterprise";
  timeout?: number;
}

export interface ProtectRule {
  source: string;
  roles?: string[];
  redirect?: string;
  status?: 401 | 403 | 404;
}

export interface RateLimitOptions {
  enabled?: boolean;
  max?: number;
  window?: number;
}

export interface AuthOptions {
  secret?: Secret;
  providers?: Provider[];
  adapter?: Adapter;
  basePath?: string;
  trustHost?: boolean;
  debug?: boolean;
  logger?: Partial<Logger>;
  session?: SessionOptions;
  jwt?: JwtOptions;
  pages?: PagesOptions;
  callbacks?: Callbacks;
  events?: Events;
  cookies?: { prefix?: string; secure?: boolean; sameSite?: "lax" | "strict" | "none"; domain?: string };
  twoFactor?: TwoFactorOptions;
  passkeys?: PasskeyOptions;
  protect?: ProtectRule[];
  rateLimit?: RateLimitOptions;
  redirects?: { signedIn?: string; signedOut?: string; allowed?: string[] };
  linking?: { automatic?: boolean };
  enabled?: boolean;
}

export interface DeclarativeProvider {
  id: string;
  type?: "oauth" | "oidc" | "credentials" | "email" | "passkey";
  name?: string;
  enabled?: boolean;
  options?: Record<string, any>;
}

export interface DeclarativeConfig extends Omit<AuthOptions, "providers" | "adapter" | "callbacks" | "events" | "logger" | "secret" | "jwt"> {
  providers?: DeclarativeProvider[];
  adapter?: { dialect?: "sqlite" | "postgres" | "mysql"; tables?: Record<string, string> };
}

export type { CookieOptions, Secret, TokenClaims };
