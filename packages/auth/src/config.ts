import { defaultCookies, type CookieSet } from "./cookies.js";
import { decodeToken, encodeToken, secretList, type Secret } from "./crypto.js";
import { isProduction, readEnv } from "./env.js";
import { resolveDeclarativeProvider } from "./providers/registry.js";
import type {
  Adapter,
  AuthOptions,
  Callbacks,
  DeclarativeConfig,
  Events,
  Logger,
  PagesOptions,
  PasskeyOptions,
  Provider,
  ProtectRule,
  TwoFactorOptions,
} from "./types.js";

declare global {
  var __KRATE_AUTH_CONFIG__: DeclarativeConfig | null | undefined;
}

export class AuthError extends Error {
  readonly code: string;
  readonly status: number;

  constructor(code: string, message?: string, status = 400) {
    super(message ?? code);
    this.name = "AuthError";
    this.code = code;
    this.status = status;
  }
}

export interface ResolvedConfig {
  secret: Secret;
  providers: Provider[];
  adapter?: Adapter;
  basePath: string;
  trustHost: boolean;
  debug: boolean;
  logger: Logger;
  strategy: "jwt" | "database";
  maxAge: number;
  updateAge: number;
  jwtMaxAge: number;
  generateSessionToken: () => string;
  encode: (params: { token: Record<string, unknown>; secret: Secret; salt: string; maxAge?: number }) => Promise<string>;
  decode: (params: { token?: string; secret: Secret; salt: string }) => Promise<Record<string, unknown> | null>;
  pages: PagesOptions;
  callbacks: Callbacks;
  events: Events;
  cookieSettings: { prefix?: string; secure?: boolean; sameSite?: "lax" | "strict" | "none"; domain?: string };
  twoFactor: Required<Pick<TwoFactorOptions, "enabled" | "required" | "digits" | "period" | "algorithm" | "window" | "backupCodes">> & { issuer: string };
  passkeys: PasskeyOptions & { enabled: boolean; userVerification: "required" | "preferred" | "discouraged"; residentKey: "required" | "preferred" | "discouraged"; attestation: "none" | "indirect" | "direct" | "enterprise"; timeout: number };
  protect: ProtectRule[];
  rateLimit: { enabled: boolean; max: number; window: number };
  redirects: { signedIn?: string; signedOut?: string; allowed: string[] };
  linking: { automatic: boolean };
  enabled: boolean;
}

export interface RequestContext {
  config: ResolvedConfig;
  request: Request;
  url: URL;
  baseUrl: string;
  secure: boolean;
  cookies: CookieSet;
  secrets: string[];
  secret: string;
}

function makeLogger(debug: boolean, custom?: Partial<Logger>): Logger {
  const noop = () => {};
  const base: Logger = {
    debug: debug ? (code, meta) => console.log(`[auth][debug] ${code}`, meta ?? "") : noop,
    warn: (code, meta) => console.warn(`[auth][warn] ${code}`, meta ?? ""),
    error: (code, meta) => console.error(`[auth][error] ${code}`, meta ?? ""),
  };
  return { ...base, ...custom };
}

export function readDeclarative(): DeclarativeConfig {
  const injected = globalThis.__KRATE_AUTH_CONFIG__;
  return injected && typeof injected === "object" ? injected : {};
}

function mergeObject<T extends object>(base: T | undefined, over: T | undefined): T {
  return { ...(base ?? {}), ...(over ?? {}) } as T;
}

export function resolveConfig(options: AuthOptions = {}, declarative: DeclarativeConfig = readDeclarative()): ResolvedConfig {
  const secret: Secret | undefined = options.secret ?? readEnv("AUTH_SECRET", "NEXTAUTH_SECRET");
  const debug = options.debug ?? declarative.debug ?? readEnv("AUTH_DEBUG") === "true";
  const logger = makeLogger(debug, options.logger);

  const programmatic = options.providers ?? [];
  const taken = new Set(programmatic.map((p) => p.id));
  const fromConfig: Provider[] = [];
  for (const decl of declarative.providers ?? []) {
    if (decl.enabled === false || taken.has(decl.id)) continue;
    const resolved = resolveDeclarativeProvider(decl);
    if (resolved) fromConfig.push(resolved);
    else logger.warn("UnknownProvider", { id: decl.id });
  }
  const providers = [...fromConfig, ...programmatic];

  const adapter = options.adapter;
  const strategy = options.session?.strategy ?? declarative.session?.strategy ?? (adapter ? "database" : "jwt");
  const maxAge = options.session?.maxAge ?? declarative.session?.maxAge ?? 30 * 24 * 60 * 60;
  const updateAge = options.session?.updateAge ?? declarative.session?.updateAge ?? 24 * 60 * 60;

  const tf = mergeObject<TwoFactorOptions>(declarative.twoFactor, options.twoFactor);
  const pk = mergeObject<PasskeyOptions>(declarative.passkeys, options.passkeys);
  const rl = mergeObject(declarative.rateLimit, options.rateLimit);
  const redirects = mergeObject(declarative.redirects, options.redirects);

  return {
    secret: secret ?? "",
    providers,
    adapter,
    basePath: options.basePath ?? declarative.basePath ?? "/api/auth",
    trustHost: options.trustHost ?? declarative.trustHost ?? readEnv("AUTH_TRUST_HOST") === "true",
    debug,
    logger,
    strategy,
    maxAge,
    updateAge,
    jwtMaxAge: options.jwt?.maxAge ?? maxAge,
    generateSessionToken: options.session?.generateSessionToken ?? (() => globalThis.crypto.randomUUID()),
    encode: async (p) => (options.jwt?.encode ? options.jwt.encode({ ...p, token: p.token }) : encodeToken({ token: p.token, secret: p.secret, salt: p.salt, maxAge: p.maxAge })),
    decode: async (p) => (options.jwt?.decode ? options.jwt.decode(p) : decodeToken({ token: p.token, secret: p.secret, salt: p.salt })),
    pages: mergeObject(declarative.pages, options.pages),
    callbacks: options.callbacks ?? {},
    events: options.events ?? {},
    cookieSettings: mergeObject(declarative.cookies, options.cookies),
    twoFactor: {
      enabled: tf.enabled ?? false,
      required: tf.required ?? false,
      issuer: tf.issuer ?? "Krate",
      digits: tf.digits ?? 6,
      period: tf.period ?? 30,
      algorithm: (tf.algorithm?.toUpperCase() as "SHA1" | "SHA256" | "SHA512" | undefined) ?? "SHA1",
      window: tf.window ?? 1,
      backupCodes: tf.backupCodes ?? 10,
    },
    passkeys: {
      ...pk,
      enabled: pk.enabled ?? providers.some((p) => p.type === "passkey"),
      userVerification: pk.userVerification ?? "preferred",
      residentKey: pk.residentKey ?? "required",
      attestation: pk.attestation ?? "none",
      timeout: pk.timeout ?? 60000,
    },
    protect: [...(declarative.protect ?? []), ...(options.protect ?? [])],
    rateLimit: { enabled: rl.enabled ?? true, max: rl.max ?? 20, window: rl.window ?? 60 },
    redirects: { signedIn: redirects.signedIn, signedOut: redirects.signedOut, allowed: redirects.allowed ?? [] },
    linking: { automatic: options.linking?.automatic ?? declarative.linking?.automatic ?? false },
    enabled: options.enabled ?? declarative.enabled ?? true,
  };
}

export function createContext(config: ResolvedConfig, request: Request): RequestContext {
  const url = new URL(request.url);
  const secrets = secretList(config.secret);
  if (secrets.length === 0) {
    throw new AuthError("MissingSecret", "Set AUTH_SECRET (openssl rand -base64 33) or pass `secret` to defineAuth", 500);
  }
  const configured = readEnv("AUTH_URL", "NEXTAUTH_URL");
  let origin: string;
  if (configured) {
    origin = new URL(configured).origin;
  } else if (config.trustHost || !isProduction()) {
    origin = url.origin;
  } else {
    throw new AuthError("UntrustedHost", "Set AUTH_URL, or trustHost / AUTH_TRUST_HOST=true behind a trusted proxy", 500);
  }
  const secure = config.cookieSettings.secure ?? origin.startsWith("https://");
  return {
    config,
    request,
    url,
    baseUrl: origin,
    secure,
    cookies: defaultCookies({ ...config.cookieSettings, secure }),
    secrets,
    secret: secrets[0]!,
  };
}
