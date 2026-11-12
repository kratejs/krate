export interface CookieOptions {
  httpOnly?: boolean;
  sameSite?: "lax" | "strict" | "none";
  path?: string;
  secure?: boolean;
  maxAge?: number;
  expires?: Date;
  domain?: string;
}

export type CookieKind =
  | "sessionToken"
  | "callbackUrl"
  | "csrfToken"
  | "pkceCodeVerifier"
  | "state"
  | "nonce"
  | "pendingTwoFactor"
  | "webauthnChallenge";

export interface CookieSpec {
  name: string;
  options: CookieOptions;
}

export type CookieSet = Record<CookieKind, CookieSpec>;

export interface CookieSettings {
  prefix?: string;
  secure?: boolean;
  sameSite?: "lax" | "strict" | "none";
  domain?: string;
}

const CHUNK_SIZE = 3933;

export function parseCookies(header: string | null | undefined): Record<string, string> {
  const out: Record<string, string> = {};
  if (!header) return out;
  for (const part of header.split(";")) {
    const idx = part.indexOf("=");
    if (idx < 0) continue;
    const name = part.slice(0, idx).trim();
    if (!name) continue;
    let value = part.slice(idx + 1).trim();
    if (value.startsWith('"') && value.endsWith('"') && value.length >= 2) value = value.slice(1, -1);
    try {
      out[name] = decodeURIComponent(value);
    } catch {
      out[name] = value;
    }
  }
  return out;
}

export function serializeCookie(name: string, value: string, options: CookieOptions = {}): string {
  let out = `${name}=${encodeURIComponent(value)}`;
  out += `; Path=${options.path ?? "/"}`;
  if (options.maxAge !== undefined) out += `; Max-Age=${Math.floor(options.maxAge)}`;
  if (options.expires) out += `; Expires=${options.expires.toUTCString()}`;
  if (options.domain) out += `; Domain=${options.domain}`;
  if (options.httpOnly) out += "; HttpOnly";
  if (options.secure) out += "; Secure";
  if (options.sameSite) out += `; SameSite=${options.sameSite[0]!.toUpperCase()}${options.sameSite.slice(1)}`;
  return out;
}

export function defaultCookies(settings: CookieSettings = {}): CookieSet {
  const secure = settings.secure ?? false;
  const prefix = settings.prefix || "krate-auth";
  const sameSite = settings.sameSite ?? "lax";
  const securePrefix = secure ? "__Secure-" : "";
  const hostPrefix = secure && !settings.domain ? "__Host-" : securePrefix;
  const base: CookieOptions = { httpOnly: true, sameSite, path: "/", secure };
  if (settings.domain) base.domain = settings.domain;
  const short: CookieOptions = { ...base, maxAge: 900 };
  return {
    sessionToken: { name: `${securePrefix}${prefix}.session-token`, options: { ...base } },
    callbackUrl: { name: `${securePrefix}${prefix}.callback-url`, options: { ...base } },
    csrfToken: { name: `${hostPrefix}${prefix}.csrf-token`, options: { ...base, sameSite: sameSite === "none" ? "none" : "lax" } },
    pkceCodeVerifier: { name: `${securePrefix}${prefix}.pkce.code_verifier`, options: { ...short } },
    state: { name: `${securePrefix}${prefix}.state`, options: { ...short } },
    nonce: { name: `${securePrefix}${prefix}.nonce`, options: { ...short } },
    pendingTwoFactor: { name: `${securePrefix}${prefix}.pending-2fa`, options: { ...short } },
    webauthnChallenge: { name: `${securePrefix}${prefix}.webauthn-challenge`, options: { ...short } },
  };
}

export class ResponseCookies {
  private entries: string[] = [];

  set(name: string, value: string, options?: CookieOptions): void {
    this.entries.push(serializeCookie(name, value, options));
  }

  delete(name: string, options?: CookieOptions): void {
    this.entries.push(serializeCookie(name, "", { ...options, maxAge: 0, expires: new Date(0) }));
  }

  setChunked(name: string, value: string, options: CookieOptions, existing: Record<string, string>): void {
    const wanted = new Set<string>();
    if (value.length <= CHUNK_SIZE) {
      this.set(name, value, options);
      wanted.add(name);
    } else {
      let index = 0;
      for (let i = 0; i < value.length; i += CHUNK_SIZE) {
        const chunkName = `${name}.${index++}`;
        this.set(chunkName, value.slice(i, i + CHUNK_SIZE), options);
        wanted.add(chunkName);
      }
    }
    for (const key of Object.keys(existing)) {
      if ((key === name || key.startsWith(`${name}.`)) && !wanted.has(key)) this.delete(key, options);
    }
  }

  deleteChunked(name: string, options: CookieOptions, existing: Record<string, string>): void {
    for (const key of Object.keys(existing)) {
      if (key === name || key.startsWith(`${name}.`)) this.delete(key, options);
    }
  }

  apply(headers: Headers): void {
    for (const entry of this.entries) headers.append("set-cookie", entry);
  }

  list(): string[] {
    return [...this.entries];
  }
}

export function readChunked(cookies: Record<string, string>, name: string): string | undefined {
  if (cookies[name] !== undefined) return cookies[name];
  const pieces: string[] = [];
  for (let i = 0; ; i++) {
    const piece = cookies[`${name}.${i}`];
    if (piece === undefined) break;
    pieces.push(piece);
  }
  return pieces.length > 0 ? pieces.join("") : undefined;
}
