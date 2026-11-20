// Binary frames for the /__krate/regions response. HTML travels as raw UTF-8
// bytes, with no JSON string escaping. The Go server asks for them with
// "Accept: application/vnd.krate.frames"; without it the renderer answers
// NDJSON as it always has. The format is documented in
// packages/compiler/internal/regionwire/frame.go.

export const REGION_FRAMES_TYPE = "application/vnd.krate.frames";

const TYPES: Record<string, number> = { region: 1, skip: 2, error: 3, end: 4 };
const TYPE_NAMES = ["", "region", "skip", "error", "end"];

const F_NOT_FOUND = 0x01;
const F_NO_STORE = 0x02;
const F_ERROR_CAUGHT = 0x04;
const F_FATAL = 0x08;

const STRING_FIELDS = ["id", "kind", "cacheStatus", "redirect", "error", "digest", "stack", "title", "head", "html"] as const;

/** True when the request's Accept header asks for the binary frames. */
export function wantsRegionFrames(accept: string | string[] | undefined): boolean {
  if (!accept) return false;
  return (Array.isArray(accept) ? accept.join(",") : accept).includes(REGION_FRAMES_TYPE);
}

function uvarintLen(n: number): number {
  let l = 1;
  while (n >= 0x80) {
    n = Math.floor(n / 128);
    l++;
  }
  return l;
}

function putUvarint(buf: Buffer, off: number, n: number): number {
  while (n >= 0x80) {
    buf[off++] = (n % 128) | 0x80;
    n = Math.floor(n / 128);
  }
  buf[off++] = n;
  return off;
}

function num(v: unknown): number {
  return typeof v === "number" && Number.isFinite(v) && v > 0 ? Math.floor(v) : 0;
}

function str(v: unknown): string {
  if (v === undefined || v === null) return "";
  return typeof v === "string" ? v : String(v);
}

/** Encodes one frame object (the shape the renderer writes as an NDJSON line). */
export function encodeFrame(frame: Record<string, any>): Buffer {
  const code = TYPES[frame.type as string];
  if (!code) throw new Error(`unknown frame type ${String(frame.type)}`);
  let flags = 0;
  if (frame.notFound) flags |= F_NOT_FOUND;
  if (frame.noStore) flags |= F_NO_STORE;
  if (frame.errorCaught) flags |= F_ERROR_CAUGHT;
  if (frame.fatal) flags |= F_FATAL;
  const status = num(frame.status);
  const denied = num(frame.denied);
  const count = num(frame.count);
  const strs: string[] = [];
  const lens: number[] = [];
  let size = 2 + uvarintLen(status) + uvarintLen(denied) + uvarintLen(count);
  for (const k of STRING_FIELDS) {
    const s = str(frame[k]);
    const l = s === "" ? 0 : Buffer.byteLength(s, "utf8");
    strs.push(s);
    lens.push(l);
    size += uvarintLen(l) + l;
  }
  const buf = Buffer.allocUnsafe(4 + size);
  buf.writeUInt32BE(size, 0);
  buf[4] = code;
  buf[5] = flags;
  let off = putUvarint(buf, 6, status);
  off = putUvarint(buf, off, denied);
  off = putUvarint(buf, off, count);
  for (let i = 0; i < strs.length; i++) {
    off = putUvarint(buf, off, lens[i]);
    if (lens[i] > 0) off += buf.write(strs[i], off, lens[i], "utf8");
  }
  return buf;
}

/**
 * Decodes a whole response body of frames. The sidecar never reads frames; the
 * tests and tooling do. A truncated or corrupt stream throws.
 */
export function decodeFrames(data: Uint8Array): Array<Record<string, any>> {
  const buf = Buffer.from(data.buffer, data.byteOffset, data.byteLength);
  const out: Array<Record<string, any>> = [];
  let pos = 0;
  while (pos < buf.length) {
    if (buf.length - pos < 4) throw new Error("truncated frame length");
    const n = buf.readUInt32BE(pos);
    pos += 4;
    if (n < 2 || n > buf.length - pos) throw new Error("truncated or corrupt frame");
    out.push(decodeBody(buf.subarray(pos, pos + n)));
    pos += n;
  }
  return out;
}

function decodeBody(b: Buffer): Record<string, any> {
  let off = 2;
  const uvarint = (): number => {
    let x = 0;
    let mul = 1;
    for (let i = 0; i < 6; i++) {
      if (off >= b.length) throw new Error("corrupt frame");
      const c = b[off++];
      x += (c & 0x7f) * mul;
      if (c < 0x80) return x;
      mul *= 128;
    }
    throw new Error("corrupt frame");
  };
  const type = TYPE_NAMES[b[0]];
  if (!type) throw new Error("corrupt frame: unknown type");
  const flags = b[1];
  const frame: Record<string, any> = { type };
  const status = uvarint();
  const denied = uvarint();
  const count = uvarint();
  if (status) frame.status = status;
  if (denied) frame.denied = denied;
  if (count) frame.count = count;
  for (const k of STRING_FIELDS) {
    const l = uvarint();
    if (l > b.length - off) throw new Error("corrupt frame");
    if (l > 0) frame[k] = b.toString("utf8", off, off + l);
    off += l;
  }
  if (off !== b.length) throw new Error("corrupt frame: trailing bytes");
  if (flags & F_NOT_FOUND) frame.notFound = true;
  if (flags & F_NO_STORE) frame.noStore = true;
  if (flags & F_ERROR_CAUGHT) frame.errorCaught = true;
  if (flags & F_FATAL) frame.fatal = true;
  return frame;
}
