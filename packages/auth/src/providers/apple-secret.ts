import { p256 } from "@noble/curves/p256";
import { b64urlDecode, b64urlEncode, concat, utf8 } from "../bytes";
import { sha256Bytes } from "../crypto";

export interface AppleSecretOptions {
  teamId: string;
  keyId: string;
  clientId: string;
  privateKey: string;
  expiresIn?: number;
  now?: number;
}

function pemToDer(pem: string): Uint8Array {
  const body = pem
    .replace(/-----BEGIN [A-Z ]+-----/g, "")
    .replace(/-----END [A-Z ]+-----/g, "")
    .replace(/\\n/g, "")
    .replace(/\s+/g, "")
    .replace(/\+/g, "-")
    .replace(/\//g, "_");
  return b64urlDecode(body);
}

function scalarFromPkcs8(der: Uint8Array): Uint8Array {
  for (let i = 0; i + 5 + 32 <= der.length; i++) {
    if (der[i] === 0x02 && der[i + 1] === 0x01 && der[i + 2] === 0x01 && der[i + 3] === 0x04 && der[i + 4] === 0x20) {
      return der.slice(i + 5, i + 5 + 32);
    }
  }
  throw new Error("privateKey is not a PKCS8 P-256 key (use the .p8 file from Apple)");
}

export function appleClientSecret(options: AppleSecretOptions): string {
  const now = Math.floor((options.now ?? Date.now()) / 1000);
  const lifetime = Math.min(options.expiresIn ?? 15_777_000, 15_777_000);
  const header = b64urlEncode(JSON.stringify({ alg: "ES256", kid: options.keyId, typ: "JWT" }));
  const payload = b64urlEncode(
    JSON.stringify({ iss: options.teamId, iat: now, exp: now + lifetime, aud: "https://appleid.apple.com", sub: options.clientId }),
  );
  const signingInput = `${header}.${payload}`;
  const key = scalarFromPkcs8(pemToDer(options.privateKey));
  const signature = p256.sign(sha256Bytes(utf8(signingInput)), key, { lowS: false }).toCompactRawBytes();
  return `${signingInput}.${b64urlEncode(concat(signature))}`;
}
