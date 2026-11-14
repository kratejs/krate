import { generateKeyPairSync, sign, type KeyObject } from "node:crypto";
import { b64urlDecode, b64urlEncode, concat, utf8 } from "./bytes";
import { encodeCbor } from "./cbor";
import { sha256Bytes } from "./crypto";
import type { AuthenticationResponseJSON, RegistrationResponseJSON } from "./webauthn";

export type Alg = -7 | -8 | -257;

export interface AuthenticatorOptions {
  alg?: Alg;
  rpId?: string;
  origin?: string;
  uv?: boolean;
  backedUp?: boolean;
  multiDevice?: boolean;
}

export interface Tamper {
  origin?: string;
  rpId?: string;
  challenge?: string;
  uv?: boolean;
  type?: string;
  fmt?: string;
  attStmt?: Map<string, unknown>;
  idOverride?: string;
  counter?: number;
  noAttested?: boolean;
  coseOverride?: Uint8Array;
  signWith?: VirtualAuthenticator;
}

export class VirtualAuthenticator {
  readonly alg: Alg;
  readonly credentialId: Uint8Array;
  counter = 0;
  private privateKey: KeyObject;
  readonly cose: Uint8Array;

  constructor(private options: AuthenticatorOptions = {}) {
    this.alg = options.alg ?? -7;
    this.credentialId = crypto.getRandomValues(new Uint8Array(32));
    let publicJwk: JsonWebKey;
    if (this.alg === -7) {
      const pair = generateKeyPairSync("ec", { namedCurve: "prime256v1" });
      this.privateKey = pair.privateKey;
      publicJwk = pair.publicKey.export({ format: "jwk" });
      this.cose = encodeCbor(new Map<any, any>([[1, 2], [3, -7], [-1, 1], [-2, b64urlDecode(publicJwk.x!)], [-3, b64urlDecode(publicJwk.y!)]]));
    } else if (this.alg === -8) {
      const pair = generateKeyPairSync("ed25519");
      this.privateKey = pair.privateKey;
      publicJwk = pair.publicKey.export({ format: "jwk" });
      this.cose = encodeCbor(new Map<any, any>([[1, 1], [3, -8], [-1, 6], [-2, b64urlDecode(publicJwk.x!)]]));
    } else {
      const pair = generateKeyPairSync("rsa", { modulusLength: 2048 });
      this.privateKey = pair.privateKey;
      publicJwk = pair.publicKey.export({ format: "jwk" });
      this.cose = encodeCbor(new Map<any, any>([[1, 3], [3, -257], [-1, b64urlDecode(publicJwk.n!)], [-2, b64urlDecode(publicJwk.e!)]]));
    }
  }

  get id(): string {
    return b64urlEncode(this.credentialId);
  }

  private get rpId(): string {
    return this.options.rpId ?? "localhost";
  }

  private get origin(): string {
    return this.options.origin ?? "http://localhost:3000";
  }

  private flags(attested: boolean, uv: boolean): number {
    let f = 0x01;
    if (uv) f |= 0x04;
    if (this.options.multiDevice) f |= 0x08;
    if (this.options.backedUp) f |= 0x10;
    if (attested) f |= 0x40;
    return f;
  }

  private authData(rpId: string, attested: boolean, uv: boolean, counter: number, cose: Uint8Array): Uint8Array {
    const head = new Uint8Array(37);
    head.set(sha256Bytes(rpId), 0);
    head[32] = this.flags(attested, uv);
    new DataView(head.buffer).setUint32(33, counter);
    if (!attested) return head;
    const len = new Uint8Array(2);
    new DataView(len.buffer).setUint16(0, this.credentialId.length);
    return concat(head, new Uint8Array(16), len, this.credentialId, cose);
  }

  private clientData(type: string, challenge: string, origin: string): Uint8Array {
    return utf8(JSON.stringify({ type, challenge, origin, crossOrigin: false }));
  }

  private signData(data: Uint8Array, key: KeyObject = this.privateKey, alg: Alg = this.alg): Uint8Array {
    return new Uint8Array(sign(alg === -8 ? null : "sha256", Buffer.from(data), key));
  }

  create(options: { challenge: string; rp: { id: string } }, tamper: Tamper = {}): RegistrationResponseJSON {
    const uv = tamper.uv ?? this.options.uv ?? true;
    const authData = this.authData(tamper.rpId ?? options.rp.id ?? this.rpId, !tamper.noAttested, uv, tamper.counter ?? this.counter, tamper.coseOverride ?? this.cose);
    const attestation = encodeCbor(new Map<any, any>([["fmt", tamper.fmt ?? "none"], ["attStmt", tamper.attStmt ?? new Map()], ["authData", authData]]));
    const clientDataJSON = this.clientData(tamper.type ?? "webauthn.create", tamper.challenge ?? options.challenge, tamper.origin ?? this.origin);
    return {
      id: tamper.idOverride ?? this.id,
      rawId: this.id,
      type: "public-key",
      response: { clientDataJSON: b64urlEncode(clientDataJSON), attestationObject: b64urlEncode(attestation), transports: ["internal", "hybrid"] },
      clientExtensionResults: {},
    };
  }

  get(options: { challenge: string; rpId: string }, tamper: Tamper = {}): AuthenticationResponseJSON {
    const uv = tamper.uv ?? this.options.uv ?? true;
    if (tamper.counter === undefined) this.counter += 1;
    const authData = this.authData(tamper.rpId ?? options.rpId ?? this.rpId, false, uv, tamper.counter ?? this.counter, this.cose);
    const clientDataJSON = this.clientData(tamper.type ?? "webauthn.get", tamper.challenge ?? options.challenge, tamper.origin ?? this.origin);
    const signer = tamper.signWith ?? this;
    const signature = this.signData(concat(authData, sha256Bytes(clientDataJSON)), signer.privateKey, signer.alg);
    return {
      id: tamper.idOverride ?? this.id,
      rawId: this.id,
      type: "public-key",
      response: { clientDataJSON: b64urlEncode(clientDataJSON), authenticatorData: b64urlEncode(authData), signature: b64urlEncode(signature), userHandle: null },
      clientExtensionResults: {},
    };
  }
}
