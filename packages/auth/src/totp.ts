import { hmac } from "@noble/hashes/hmac";
import { sha1 } from "@noble/hashes/sha1";
import { sha256 } from "@noble/hashes/sha256";
import { sha512 } from "@noble/hashes/sha512";
import { randomBytes, randomToken, timingSafeEqual, utf8 } from "./bytes.js";
import { sha256Hex } from "./crypto.js";

export type TotpAlgorithm = "SHA1" | "SHA256" | "SHA512";

export interface TotpOptions {
  digits: number;
  period: number;
  algorithm: TotpAlgorithm;
}

const ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

export function base32Encode(bytes: Uint8Array): string {
  let bits = 0;
  let value = 0;
  let out = "";
  for (const byte of bytes) {
    value = (value << 8) | byte;
    bits += 8;
    while (bits >= 5) {
      out += ALPHABET.charAt((value >>> (bits - 5)) & 31);
      bits -= 5;
    }
    value &= (1 << bits) - 1;
  }
  if (bits > 0) out += ALPHABET.charAt((value << (5 - bits)) & 31);
  return out;
}

export function base32Decode(input: string): Uint8Array {
  const clean = input.replace(/[\s=-]/g, "").toUpperCase();
  const out: number[] = [];
  let bits = 0;
  let value = 0;
  for (const ch of clean) {
    const idx = ALPHABET.indexOf(ch);
    if (idx < 0) throw new Error("invalid base32 input");
    value = (value << 5) | idx;
    bits += 5;
    if (bits >= 8) {
      out.push((value >>> (bits - 8)) & 0xff);
      bits -= 8;
      value &= (1 << bits) - 1;
    }
  }
  return Uint8Array.from(out);
}

const HASHES = { SHA1: sha1, SHA256: sha256, SHA512: sha512 } as const;

export function secretLength(algorithm: TotpAlgorithm): number {
  return algorithm === "SHA1" ? 20 : algorithm === "SHA256" ? 32 : 64;
}

export function generateSecret(algorithm: TotpAlgorithm = "SHA1"): string {
  return base32Encode(randomBytes(secretLength(algorithm)));
}

export function hotp(secret: Uint8Array, counter: number, options: Pick<TotpOptions, "digits" | "algorithm">): string {
  const message = new Uint8Array(8);
  let c = counter;
  for (let i = 7; i >= 0; i--) {
    message[i] = c % 256;
    c = Math.floor(c / 256);
  }
  const digest = hmac(HASHES[options.algorithm], secret, message);
  const offset = digest[digest.length - 1]! & 0x0f;
  const binary = ((digest[offset]! & 0x7f) << 24) | (digest[offset + 1]! << 16) | (digest[offset + 2]! << 8) | digest[offset + 3]!;
  return String(binary % 10 ** options.digits).padStart(options.digits, "0");
}

export function totpCounter(now: number, period: number): number {
  return Math.floor(now / 1000 / period);
}

export function totp(secretBase32: string, options: TotpOptions, now = Date.now()): string {
  return hotp(base32Decode(secretBase32), totpCounter(now, options.period), options);
}

export function verifyTotp(
  code: string,
  secretBase32: string,
  options: TotpOptions & { window: number; lastUsedCounter?: number | null },
  now = Date.now(),
): number | null {
  const cleaned = code.replace(/\s+/g, "");
  if (!new RegExp(`^[0-9]{${options.digits}}$`).test(cleaned)) return null;
  const secret = base32Decode(secretBase32);
  const current = totpCounter(now, options.period);
  let matched: number | null = null;
  for (let step = -options.window; step <= options.window; step++) {
    const counter = current + step;
    if (counter < 0) continue;
    if (timingSafeEqual(hotp(secret, counter, options), cleaned) && matched === null) matched = counter;
  }
  if (matched === null) return null;
  if (options.lastUsedCounter !== undefined && options.lastUsedCounter !== null && matched <= options.lastUsedCounter) return null;
  return matched;
}

export function otpauthUrl(params: { secret: string; account: string; issuer: string } & TotpOptions): string {
  const label = `${encodeURIComponent(params.issuer)}:${encodeURIComponent(params.account)}`;
  const query = new URLSearchParams({
    secret: params.secret,
    issuer: params.issuer,
    algorithm: params.algorithm,
    digits: String(params.digits),
    period: String(params.period),
  });
  return `otpauth://totp/${label}?${query.toString()}`;
}

export function generateBackupCodes(count: number): { plain: string[]; stored: string[] } {
  const plain: string[] = [];
  const stored: string[] = [];
  for (let i = 0; i < count; i++) {
    const raw = base32Encode(randomBytes(7)).slice(0, 10);
    const code = `${raw.slice(0, 5)}-${raw.slice(5)}`;
    plain.push(code);
    stored.push(hashBackupCode(code));
  }
  return { plain, stored };
}

export function normalizeBackupCode(input: string): string {
  return input.replace(/[\s-]/g, "").toUpperCase();
}

export function hashBackupCode(code: string): string {
  const salt = randomToken(9);
  return `${salt}$${sha256Hex(`${salt}${normalizeBackupCode(code)}`)}`;
}

export function consumeBackupCode(input: string, stored: string[]): string[] | null {
  const normalized = normalizeBackupCode(input);
  if (normalized.length !== 10) return null;
  let index = -1;
  for (let i = 0; i < stored.length; i++) {
    const [salt, hash] = stored[i]!.split("$");
    if (salt && hash && timingSafeEqual(utf8(hash), utf8(sha256Hex(`${salt}${normalized}`))) && index < 0) index = i;
  }
  if (index < 0) return null;
  return stored.filter((_, i) => i !== index);
}
