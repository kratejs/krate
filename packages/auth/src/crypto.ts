import { gcm } from "@noble/ciphers/aes";
import { hkdf } from "@noble/hashes/hkdf";
import { hmac } from "@noble/hashes/hmac";
import { sha256 } from "@noble/hashes/sha256";
import { b64urlDecode, b64urlDecodeString, b64urlEncode, concat, fromUtf8, hex, randomBytes, timingSafeEqual, utf8 } from "./bytes.js";

export type Secret = string | string[];

export function secretList(secret: Secret | undefined): string[] {
  if (!secret) return [];
  return (Array.isArray(secret) ? secret : [secret]).filter((s) => typeof s === "string" && s.length > 0);
}

export function deriveKey(secret: string, purpose: string): Uint8Array {
  return hkdf(sha256, utf8(secret), utf8(purpose), utf8(`Krate Auth Generated Encryption Key (${purpose})`), 32);
}

export function sha256Hex(input: string | Uint8Array): string {
  return hex(sha256(typeof input === "string" ? utf8(input) : input));
}

export function sha256Bytes(input: string | Uint8Array): Uint8Array {
  return sha256(typeof input === "string" ? utf8(input) : input);
}

export function hmacSha256(key: string | Uint8Array, data: string | Uint8Array): Uint8Array {
  return hmac(sha256, typeof key === "string" ? utf8(key) : key, typeof data === "string" ? utf8(data) : data);
}

export interface TokenClaims {
  iat?: number;
  exp?: number;
  jti?: string;
  [key: string]: unknown;
}

export interface EncodeParams {
  token: TokenClaims;
  secret: Secret;
  salt: string;
  maxAge?: number;
  now?: number;
}

export interface DecodeParams {
  token: string | undefined | null;
  secret: Secret;
  salt: string;
  now?: number;
}

const HEADER = b64urlEncode(JSON.stringify({ alg: "dir", enc: "A256GCM", typ: "JWT" }));

export async function encodeToken(params: EncodeParams): Promise<string> {
  const secrets = secretList(params.secret);
  if (secrets.length === 0) throw new Error("a secret is required to encode a token");
  const now = Math.floor((params.now ?? Date.now()) / 1000);
  const claims: TokenClaims = { ...params.token, iat: now };
  if (params.maxAge !== undefined) claims.exp = now + params.maxAge;
  claims.jti = claims.jti ?? b64urlEncode(randomBytes(12));
  const key = deriveKey(secrets[0]!, params.salt);
  const iv = randomBytes(12);
  const sealed = gcm(key, iv, utf8(HEADER)).encrypt(utf8(JSON.stringify(claims)));
  const ciphertext = sealed.subarray(0, sealed.length - 16);
  const tag = sealed.subarray(sealed.length - 16);
  return [HEADER, "", b64urlEncode(iv), b64urlEncode(ciphertext), b64urlEncode(tag)].join(".");
}

export async function decodeToken(params: DecodeParams): Promise<TokenClaims | null> {
  const { token } = params;
  if (!token || typeof token !== "string") return null;
  const parts = token.split(".");
  if (parts.length !== 5) return null;
  const [header, encryptedKey, ivPart, ctPart, tagPart] = parts as [string, string, string, string, string];
  if (encryptedKey !== "") return null;
  try {
    const parsed = JSON.parse(b64urlDecodeString(header)) as { alg?: string; enc?: string };
    if (parsed.alg !== "dir" || parsed.enc !== "A256GCM") return null;
    const iv = b64urlDecode(ivPart);
    const sealed = concat(b64urlDecode(ctPart), b64urlDecode(tagPart));
    for (const secret of secretList(params.secret)) {
      let plain: Uint8Array;
      try {
        plain = gcm(deriveKey(secret, params.salt), iv, utf8(header)).decrypt(sealed);
      } catch {
        continue;
      }
      const claims = JSON.parse(fromUtf8(plain)) as TokenClaims;
      const now = Math.floor((params.now ?? Date.now()) / 1000);
      if (typeof claims.exp === "number" && claims.exp <= now) return null;
      return claims;
    }
    return null;
  } catch {
    return null;
  }
}

export function sealString(plain: string, secret: string, purpose: string): string {
  const iv = randomBytes(12);
  const sealed = gcm(deriveKey(secret, purpose), iv).encrypt(utf8(plain));
  return b64urlEncode(concat(iv, sealed));
}

export function unsealString(value: string, secrets: Secret, purpose: string): string | null {
  let bytes: Uint8Array;
  try {
    bytes = b64urlDecode(value);
  } catch {
    return null;
  }
  if (bytes.length < 12 + 16) return null;
  const iv = bytes.subarray(0, 12);
  const sealed = bytes.subarray(12);
  for (const secret of secretList(secrets)) {
    try {
      return fromUtf8(gcm(deriveKey(secret, purpose), iv).decrypt(sealed));
    } catch {
      continue;
    }
  }
  return null;
}

export function createCsrfToken(secret: string): { token: string; cookieValue: string } {
  const token = b64urlEncode(randomBytes(32));
  const hash = sha256Hex(`${token}${secret}`);
  return { token, cookieValue: `${token}|${hash}` };
}

export function verifyCsrfCookie(cookieValue: string | undefined, secrets: Secret): { token: string; valid: boolean } | null {
  if (!cookieValue) return null;
  const [token, hash] = cookieValue.split("|");
  if (!token || !hash) return null;
  for (const secret of secretList(secrets)) {
    if (timingSafeEqual(hash, sha256Hex(`${token}${secret}`))) return { token, valid: true };
  }
  return { token, valid: false };
}
