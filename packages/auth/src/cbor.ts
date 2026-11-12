export type CborValue = number | bigint | string | boolean | null | undefined | Uint8Array | CborValue[] | Map<CborValue, CborValue>;

export interface Decoded {
  value: CborValue;
  length: number;
}

const MAX_DEPTH = 16;
const MAX_ITEMS = 4096;

export function decodeCbor(bytes: Uint8Array, offset = 0): Decoded {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  let pos = offset;

  function need(n: number): void {
    if (pos + n > bytes.length) throw new Error("cbor: unexpected end of input");
  }

  function readArg(info: number): number | bigint {
    if (info < 24) return info;
    if (info === 24) {
      need(1);
      return bytes[pos++]!;
    }
    if (info === 25) {
      need(2);
      const v = view.getUint16(pos);
      pos += 2;
      return v;
    }
    if (info === 26) {
      need(4);
      const v = view.getUint32(pos);
      pos += 4;
      return v;
    }
    if (info === 27) {
      need(8);
      const v = view.getBigUint64(pos);
      pos += 8;
      return v <= BigInt(Number.MAX_SAFE_INTEGER) ? Number(v) : v;
    }
    throw new Error("cbor: indefinite or reserved lengths are not supported");
  }

  function item(depth: number): CborValue {
    if (depth > MAX_DEPTH) throw new Error("cbor: nesting too deep");
    need(1);
    const initial = bytes[pos++]!;
    const major = initial >> 5;
    const info = initial & 0x1f;
    if (major === 7) {
      if (info === 20) return false;
      if (info === 21) return true;
      if (info === 22) return null;
      if (info === 23) return undefined;
      throw new Error("cbor: unsupported simple or float value");
    }
    const arg = readArg(info);
    if (major === 0) return arg;
    if (major === 1) return typeof arg === "bigint" ? -1n - arg : -1 - arg;
    const len = Number(arg);
    if (major === 2 || major === 3) {
      need(len);
      const slice = bytes.slice(pos, pos + len);
      pos += len;
      return major === 2 ? slice : new TextDecoder("utf-8", { fatal: true }).decode(slice);
    }
    if (len > MAX_ITEMS) throw new Error("cbor: collection too large");
    if (major === 4) {
      const out: CborValue[] = [];
      for (let i = 0; i < len; i++) out.push(item(depth + 1));
      return out;
    }
    if (major === 5) {
      const out = new Map<CborValue, CborValue>();
      for (let i = 0; i < len; i++) {
        const key = item(depth + 1);
        out.set(key, item(depth + 1));
      }
      return out;
    }
    throw new Error("cbor: tags are not supported");
  }

  const value = item(0);
  return { value, length: pos - offset };
}

function head(major: number, value: number | bigint): number[] {
  const m = major << 5;
  const n = typeof value === "bigint" ? value : BigInt(value);
  if (n < 24n) return [m | Number(n)];
  if (n < 256n) return [m | 24, Number(n)];
  if (n < 65536n) return [m | 25, Number(n >> 8n), Number(n & 255n)];
  if (n < 4294967296n) return [m | 26, Number((n >> 24n) & 255n), Number((n >> 16n) & 255n), Number((n >> 8n) & 255n), Number(n & 255n)];
  const out = [m | 27];
  for (let shift = 56n; shift >= 0n; shift -= 8n) out.push(Number((n >> shift) & 255n));
  return out;
}

export function encodeCbor(value: CborValue): Uint8Array {
  const out: number[] = [];
  const push = (v: CborValue): void => {
    if (v === null) out.push(0xf6);
    else if (v === undefined) out.push(0xf7);
    else if (typeof v === "boolean") out.push(v ? 0xf5 : 0xf4);
    else if (typeof v === "number" || typeof v === "bigint") {
      const n = typeof v === "bigint" ? v : BigInt(v);
      if (n >= 0n) out.push(...head(0, n));
      else out.push(...head(1, -1n - n));
    } else if (typeof v === "string") {
      const bytes = new TextEncoder().encode(v);
      out.push(...head(3, bytes.length), ...bytes);
    } else if (v instanceof Uint8Array) {
      out.push(...head(2, v.length));
      for (const b of v) out.push(b);
    } else if (Array.isArray(v)) {
      out.push(...head(4, v.length));
      for (const item of v) push(item);
    } else if (v instanceof Map) {
      out.push(...head(5, v.size));
      for (const [k, val] of v) {
        push(k);
        push(val);
      }
    } else {
      throw new Error("cbor: cannot encode value");
    }
  };
  push(value);
  return Uint8Array.from(out);
}
