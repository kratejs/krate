import { ed25519 } from "@noble/curves/ed25519";
import { p256 } from "@noble/curves/p256";
import { b64urlDecode, b64urlDecodeString, b64urlEncode, concat, fromUtf8, timingSafeEqual } from "./bytes.js";
import { decodeCbor, type CborValue } from "./cbor.js";
import { sha256Bytes } from "./crypto.js";

export class WebAuthnError extends Error {
  readonly code: string;
  constructor(code: string, message: string) {
    super(message);
    this.name = "WebAuthnError";
    this.code = code;
  }
}

export const SUPPORTED_ALGORITHMS = [-7, -8, -257] as const;
export type CoseAlgorithm = (typeof SUPPORTED_ALGORITHMS)[number];

export interface RegistrationResponseJSON {
  id: string;
  rawId?: string;
  type: "public-key";
  response: {
    clientDataJSON: string;
    attestationObject: string;
    transports?: string[];
  };
  authenticatorAttachment?: string;
  clientExtensionResults?: Record<string, unknown>;
}

export interface AuthenticationResponseJSON {
  id: string;
  rawId?: string;
  type: "public-key";
  response: {
    clientDataJSON: string;
    authenticatorData: string;
    signature: string;
    userHandle?: string | null;
  };
  authenticatorAttachment?: string;
  clientExtensionResults?: Record<string, unknown>;
}

export interface AuthData {
  rpIdHash: Uint8Array;
  flags: { up: boolean; uv: boolean; be: boolean; bs: boolean; at: boolean; ed: boolean };
  signCount: number;
  attested?: { aaguid: Uint8Array; credentialId: Uint8Array; publicKey: Uint8Array };
}

export function parseAuthData(bytes: Uint8Array): AuthData {
  if (bytes.length < 37) throw new WebAuthnError("InvalidAuthData", "authenticator data is too short");
  const flagByte = bytes[32]!;
  const flags = {
    up: (flagByte & 0x01) !== 0,
    uv: (flagByte & 0x04) !== 0,
    be: (flagByte & 0x08) !== 0,
    bs: (flagByte & 0x10) !== 0,
    at: (flagByte & 0x40) !== 0,
    ed: (flagByte & 0x80) !== 0,
  };
  const signCount = new DataView(bytes.buffer, bytes.byteOffset + 33, 4).getUint32(0);
  const out: AuthData = { rpIdHash: bytes.slice(0, 32), flags, signCount };
  if (flags.at) {
    if (bytes.length < 55) throw new WebAuthnError("InvalidAuthData", "attested credential data is truncated");
    const aaguid = bytes.slice(37, 53);
    const idLength = new DataView(bytes.buffer, bytes.byteOffset + 53, 2).getUint16(0);
    if (idLength === 0 || idLength > 1023 || 55 + idLength > bytes.length) throw new WebAuthnError("InvalidAuthData", "credential id length is invalid");
    const credentialId = bytes.slice(55, 55 + idLength);
    const keyStart = 55 + idLength;
    let decoded;
    try {
      decoded = decodeCbor(bytes, keyStart);
    } catch (err) {
      throw new WebAuthnError("InvalidAuthData", `credential public key is not valid CBOR: ${(err as Error).message}`);
    }
    out.attested = { aaguid, credentialId, publicKey: bytes.slice(keyStart, keyStart + decoded.length) };
  }
  return out;
}

export interface ClientData {
  type: string;
  challenge: string;
  origin: string;
  crossOrigin?: boolean;
}

export function verifyClientData(clientDataJSON: Uint8Array, expected: { type: "webauthn.create" | "webauthn.get"; challenge: string; origins: string[] }): ClientData {
  let data: ClientData;
  try {
    data = JSON.parse(fromUtf8(clientDataJSON)) as ClientData;
  } catch {
    throw new WebAuthnError("InvalidClientData", "clientDataJSON is not valid JSON");
  }
  if (data.type !== expected.type) throw new WebAuthnError("InvalidClientData", `unexpected ceremony type ${String(data.type)}`);
  if (typeof data.challenge !== "string" || !timingSafeEqual(data.challenge, expected.challenge)) {
    throw new WebAuthnError("InvalidChallenge", "the challenge does not match");
  }
  if (!expected.origins.includes(data.origin)) throw new WebAuthnError("InvalidOrigin", `origin ${String(data.origin)} is not allowed`);
  if (data.crossOrigin === true) throw new WebAuthnError("InvalidOrigin", "cross-origin ceremonies are not allowed");
  return data;
}

interface CoseKey {
  kty: number;
  alg: number;
  map: Map<CborValue, CborValue>;
}

function readCose(bytes: Uint8Array): CoseKey {
  let value: CborValue;
  try {
    value = decodeCbor(bytes).value;
  } catch {
    throw new WebAuthnError("InvalidPublicKey", "the public key is not valid CBOR");
  }
  if (!(value instanceof Map)) throw new WebAuthnError("InvalidPublicKey", "the public key is not a COSE map");
  const kty = value.get(1);
  const alg = value.get(3);
  if (typeof kty !== "number" || typeof alg !== "number") throw new WebAuthnError("InvalidPublicKey", "the public key lacks kty or alg");
  return { kty, alg, map: value };
}

export function coseAlgorithm(bytes: Uint8Array): number {
  return readCose(bytes).alg;
}

export function assertSupportedKey(bytes: Uint8Array): void {
  const key = readCose(bytes);
  if (!(SUPPORTED_ALGORITHMS as readonly number[]).includes(key.alg)) throw new WebAuthnError("UnsupportedAlgorithm", `COSE algorithm ${key.alg} is not supported`);
  const bin = (k: number): Uint8Array => {
    const v = key.map.get(k);
    if (!(v instanceof Uint8Array)) throw new WebAuthnError("InvalidPublicKey", `COSE parameter ${k} is missing`);
    return v;
  };
  if (key.alg === -7) {
    if (key.kty !== 2 || key.map.get(-1) !== 1 || bin(-2).length !== 32 || bin(-3).length !== 32) throw new WebAuthnError("InvalidPublicKey", "expected a P-256 key");
  } else if (key.alg === -8) {
    if (key.kty !== 1 || key.map.get(-1) !== 6 || bin(-2).length !== 32) throw new WebAuthnError("InvalidPublicKey", "expected an Ed25519 key");
  } else if (key.kty !== 3 || bin(-1).length < 256 || bin(-2).length > 8) {
    throw new WebAuthnError("InvalidPublicKey", "expected an RSA key of at least 2048 bits");
  }
}

function bytesToBigInt(bytes: Uint8Array): bigint {
  let out = 0n;
  for (const b of bytes) out = (out << 8n) | BigInt(b);
  return out;
}

function modPow(base: bigint, exponent: bigint, modulus: bigint): bigint {
  let result = 1n;
  let b = base % modulus;
  let e = exponent;
  while (e > 0n) {
    if (e & 1n) result = (result * b) % modulus;
    e >>= 1n;
    b = (b * b) % modulus;
  }
  return result;
}

const SHA256_DIGEST_INFO = Uint8Array.from([0x30, 0x31, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x01, 0x05, 0x00, 0x04, 0x20]);

function verifyRsa(n: Uint8Array, e: Uint8Array, data: Uint8Array, signature: Uint8Array): boolean {
  const modulus = bytesToBigInt(n);
  const k = n.length;
  if (signature.length !== k) return false;
  const s = bytesToBigInt(signature);
  if (s >= modulus) return false;
  const m = modPow(s, bytesToBigInt(e), modulus);
  const em = new Uint8Array(k);
  let rest = m;
  for (let i = k - 1; i >= 0; i--) {
    em[i] = Number(rest & 0xffn);
    rest >>= 8n;
  }
  const digest = sha256Bytes(data);
  const tLength = SHA256_DIGEST_INFO.length + digest.length;
  if (k < tLength + 11) return false;
  const expected = new Uint8Array(k);
  expected[1] = 0x01;
  expected.fill(0xff, 2, k - tLength - 1);
  expected.set(concat(SHA256_DIGEST_INFO, digest), k - tLength);
  return timingSafeEqual(em, expected);
}

export function verifySignature(coseBytes: Uint8Array, data: Uint8Array, signature: Uint8Array): boolean {
  const key = readCose(coseBytes);
  const bin = (k: number): Uint8Array => {
    const v = key.map.get(k);
    if (!(v instanceof Uint8Array)) throw new WebAuthnError("InvalidPublicKey", `COSE parameter ${k} is missing`);
    return v;
  };
  try {
    if (key.alg === -7) {
      const publicKey = concat(Uint8Array.of(0x04), bin(-2), bin(-3));
      return p256.verify(signature, sha256Bytes(data), publicKey, { lowS: false, format: "der", prehash: false });
    }
    if (key.alg === -8) return ed25519.verify(signature, data, bin(-2));
    if (key.alg === -257) return verifyRsa(bin(-1), bin(-2), data, signature);
  } catch {
    return false;
  }
  throw new WebAuthnError("UnsupportedAlgorithm", `COSE algorithm ${key.alg} is not supported`);
}

export interface VerifiedRegistration {
  credentialID: string;
  publicKey: string;
  counter: number;
  deviceType: "singleDevice" | "multiDevice";
  backedUp: boolean;
  userVerified: boolean;
  fmt: string;
  transports: string[];
}

export function verifyRegistration(params: {
  credential: RegistrationResponseJSON;
  challenge: string;
  origins: string[];
  rpId: string;
  requireUserVerification: boolean;
}): VerifiedRegistration {
  const { credential } = params;
  if (!credential || credential.type !== "public-key" || typeof credential.id !== "string" || !credential.response) {
    throw new WebAuthnError("InvalidCredential", "the credential is malformed");
  }
  let clientData: Uint8Array;
  let attestationObject: Uint8Array;
  try {
    clientData = b64urlDecode(credential.response.clientDataJSON);
    attestationObject = b64urlDecode(credential.response.attestationObject);
  } catch {
    throw new WebAuthnError("InvalidCredential", "the credential fields are not valid base64url");
  }
  verifyClientData(clientData, { type: "webauthn.create", challenge: params.challenge, origins: params.origins });

  let attestation: CborValue;
  try {
    attestation = decodeCbor(attestationObject).value;
  } catch {
    throw new WebAuthnError("InvalidAttestation", "the attestation object is not valid CBOR");
  }
  if (!(attestation instanceof Map)) throw new WebAuthnError("InvalidAttestation", "the attestation object is not a map");
  const fmt = attestation.get("fmt");
  const authDataBytes = attestation.get("authData");
  if (typeof fmt !== "string" || !(authDataBytes instanceof Uint8Array)) throw new WebAuthnError("InvalidAttestation", "the attestation object lacks fmt or authData");
  const authData = parseAuthData(authDataBytes);
  if (!timingSafeEqual(authData.rpIdHash, sha256Bytes(params.rpId))) throw new WebAuthnError("InvalidRpId", "the relying party id hash does not match");
  if (!authData.flags.up) throw new WebAuthnError("UserNotPresent", "user presence was not asserted");
  if (params.requireUserVerification && !authData.flags.uv) throw new WebAuthnError("UserNotVerified", "user verification is required");
  if (!authData.attested) throw new WebAuthnError("InvalidAuthData", "no attested credential data");
  const credentialID = b64urlEncode(authData.attested.credentialId);
  if (credentialID !== credential.id) throw new WebAuthnError("InvalidCredential", "the credential id does not match the attested id");
  assertSupportedKey(authData.attested.publicKey);
  if (fmt === "none") {
    const stmt = attestation.get("attStmt");
    if (!(stmt instanceof Map) || stmt.size !== 0) throw new WebAuthnError("InvalidAttestation", "a none attestation must have an empty statement");
  }
  return {
    credentialID,
    publicKey: b64urlEncode(authData.attested.publicKey),
    counter: authData.signCount,
    deviceType: authData.flags.be ? "multiDevice" : "singleDevice",
    backedUp: authData.flags.bs,
    userVerified: authData.flags.uv,
    fmt,
    transports: Array.isArray(credential.response.transports) ? credential.response.transports.filter((t) => typeof t === "string").slice(0, 8) : [],
  };
}

export interface VerifiedAuthentication {
  counter: number;
  userVerified: boolean;
  backedUp: boolean;
}

export function verifyAuthentication(params: {
  credential: AuthenticationResponseJSON;
  challenge: string;
  origins: string[];
  rpId: string;
  requireUserVerification: boolean;
  publicKey: string;
  storedCounter: number;
}): VerifiedAuthentication {
  const { credential } = params;
  if (!credential || credential.type !== "public-key" || !credential.response) throw new WebAuthnError("InvalidCredential", "the credential is malformed");
  let clientData: Uint8Array;
  let authDataBytes: Uint8Array;
  let signature: Uint8Array;
  try {
    clientData = b64urlDecode(credential.response.clientDataJSON);
    authDataBytes = b64urlDecode(credential.response.authenticatorData);
    signature = b64urlDecode(credential.response.signature);
  } catch {
    throw new WebAuthnError("InvalidCredential", "the credential fields are not valid base64url");
  }
  verifyClientData(clientData, { type: "webauthn.get", challenge: params.challenge, origins: params.origins });
  const authData = parseAuthData(authDataBytes);
  if (!timingSafeEqual(authData.rpIdHash, sha256Bytes(params.rpId))) throw new WebAuthnError("InvalidRpId", "the relying party id hash does not match");
  if (!authData.flags.up) throw new WebAuthnError("UserNotPresent", "user presence was not asserted");
  if (params.requireUserVerification && !authData.flags.uv) throw new WebAuthnError("UserNotVerified", "user verification is required");
  const signed = concat(authDataBytes, sha256Bytes(clientData));
  if (!verifySignature(b64urlDecode(params.publicKey), signed, signature)) throw new WebAuthnError("InvalidSignature", "the signature is not valid");
  if ((authData.signCount !== 0 || params.storedCounter !== 0) && authData.signCount <= params.storedCounter) {
    throw new WebAuthnError("CounterRegression", "the signature counter did not increase; the authenticator may be cloned");
  }
  return { counter: authData.signCount, userVerified: authData.flags.uv, backedUp: authData.flags.bs };
}

export function decodeBase64urlJson(value: string): unknown {
  return JSON.parse(b64urlDecodeString(value));
}
