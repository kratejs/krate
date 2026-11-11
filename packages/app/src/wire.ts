// Frame codec for the host call protocol (see packages/native/PROTOCOL.md).
//
// Frame: u32 BE json length | json | u16 BE blob count | blobs (u32 BE len + bytes).
//
// Escape rule (extends PROTOCOL.md, which only defines `$b$`): the reserved tag
// keys are $b, $d, $m, $s. A user object with exactly one own key that is a tag
// ($b) is written as {"$b$": <object>}. A user object whose only key is already
// an escaped form ($b$) is wrapped under that same key: {"$b$": {"$b$": x}}.
// On decode, an object whose only key is `$b$`, `$d$`, `$m$` or `$s$` is a
// wrapper: its value is a plain object whose own keys are taken literally
// (values still decode normally). So {"$b": 1} -> {"$b$": {"$b": 1}} and
// {"$b$": 1} -> {"$b$": {"$b$": 1}}; same for $d, $m, $s.

const TAGS = ["$b", "$d", "$m", "$s"];
const ESCAPED = TAGS.map((t) => t + "$");

export interface EncodeCtx {
  blobs: Uint8Array[];
}

const te = new TextEncoder();
const td = new TextDecoder("utf-8", { fatal: true });

function fail(root: string, path: string, msg: string): never {
  throw new TypeError(`${root}${path ? " at " + path : ""}: ${msg}`);
}

function addBlob(ctx: EncodeCtx, bytes: Uint8Array): unknown {
  ctx.blobs.push(bytes);
  return { $b: ctx.blobs.length - 1 };
}

// encodeValue converts a JS value to its JSON-ready tree, appending blobs to ctx.
// root names the value in error messages (for example "argument 2").
export function encodeValue(v: unknown, ctx: EncodeCtx, root = "value"): unknown {
  return enc(v, ctx, root, "", []);
}

function enc(v: unknown, ctx: EncodeCtx, root: string, path: string, stack: object[]): unknown {
  switch (typeof v) {
    case "string":
    case "boolean":
      return v;
    case "number":
      if (!Number.isFinite(v)) fail(root, path, `cannot encode ${String(v)} (numbers must be finite)`);
      return v;
    case "undefined":
      return null;
    case "bigint":
      return fail(root, path, "cannot encode bigint");
    case "function":
      return fail(root, path, "cannot encode a function");
    case "symbol":
      return fail(root, path, "cannot encode a symbol");
  }
  if (v === null) return null;
  const o = v as object;
  if (o instanceof Uint8Array) return addBlob(ctx, o);
  if (o instanceof ArrayBuffer) return addBlob(ctx, new Uint8Array(o.slice(0)));
  if (ArrayBuffer.isView(o)) {
    const view = o as ArrayBufferView;
    return addBlob(ctx, new Uint8Array(view.buffer.slice(view.byteOffset, view.byteOffset + view.byteLength)));
  }
  if (o instanceof Date) {
    if (Number.isNaN(o.getTime())) fail(root, path, "cannot encode an invalid Date");
    return { $d: o.toISOString() };
  }
  if (stack.includes(o)) fail(root, path, "cannot encode a circular structure");
  stack.push(o);
  try {
    if (Array.isArray(o)) {
      return o.map((x, i) => enc(x, ctx, root, `${path}[${i}]`, stack));
    }
    if (o instanceof Map) {
      const pairs: unknown[] = [];
      let i = 0;
      for (const [k, val] of o) {
        pairs.push([enc(k, ctx, root, `${path}.<key ${i}>`, stack), enc(val, ctx, root, `${path}.<value ${i}>`, stack)]);
        i++;
      }
      return { $m: pairs };
    }
    if (o instanceof Set) {
      const items: unknown[] = [];
      let i = 0;
      for (const x of o) items.push(enc(x, ctx, root, `${path}.<item ${i++}>`, stack));
      return { $s: items };
    }
    const proto = Object.getPrototypeOf(o);
    if (proto !== Object.prototype && proto !== null) {
      const name = (proto.constructor && proto.constructor.name) || "unknown";
      fail(root, path, `cannot encode an instance of ${name} (only plain objects, arrays, Date, Map, Set and Uint8Array)`);
    }
    const keys = Object.keys(o);
    const out: Record<string, unknown> = {};
    for (const k of keys) {
      const val = (o as Record<string, unknown>)[k];
      if (val === undefined) continue;
      out[k] = enc(val, ctx, root, `${path}.${k}`, stack);
    }
    if (keys.length === 1 && out[keys[0]] !== undefined) {
      const k = keys[0];
      if (TAGS.includes(k)) return { [k + "$"]: out };
      if (ESCAPED.includes(k)) return { [k]: out };
    }
    return out;
  } finally {
    stack.pop();
  }
}

export function buildFrame(tree: unknown, blobs: Uint8Array[]): Uint8Array {
  if (blobs.length > 0xffff) throw new TypeError(`too many binary values in one frame (${blobs.length} > 65535)`);
  const json = te.encode(JSON.stringify(tree));
  let size = 4 + json.length + 2;
  for (const b of blobs) size += 4 + b.length;
  const out = new Uint8Array(size);
  const dv = new DataView(out.buffer);
  dv.setUint32(0, json.length);
  out.set(json, 4);
  let p = 4 + json.length;
  dv.setUint16(p, blobs.length);
  p += 2;
  for (const b of blobs) {
    dv.setUint32(p, b.length);
    out.set(b, p + 4);
    p += 4 + b.length;
  }
  return out;
}

export function encodeFrame(value: unknown, root = "value"): Uint8Array {
  const ctx: EncodeCtx = { blobs: [] };
  const tree = encodeValue(value, ctx, root);
  return buildFrame(tree, ctx.blobs);
}

function dec(t: unknown, blobs: Uint8Array[]): unknown {
  if (t === null || typeof t !== "object") return t;
  if (Array.isArray(t)) return t.map((x) => dec(x, blobs));
  const o = t as Record<string, unknown>;
  const keys = Object.keys(o);
  if (keys.length === 1) {
    const k = keys[0];
    const v = o[k];
    if (k === "$b") {
      if (typeof v !== "number" || !Number.isInteger(v) || v < 0 || v >= blobs.length) {
        throw new Error(`bad blob index ${JSON.stringify(v)}`);
      }
      return blobs[v];
    }
    if (k === "$d") {
      if (typeof v !== "string") throw new Error("bad $d value");
      const d = new Date(v);
      if (Number.isNaN(d.getTime())) throw new Error(`bad date ${JSON.stringify(v)}`);
      return d;
    }
    if (k === "$m") {
      if (!Array.isArray(v)) throw new Error("bad $m value");
      return new Map(v.map((p) => {
        if (!Array.isArray(p) || p.length !== 2) throw new Error("bad $m entry");
        return [dec(p[0], blobs), dec(p[1], blobs)] as [unknown, unknown];
      }));
    }
    if (k === "$s") {
      if (!Array.isArray(v)) throw new Error("bad $s value");
      return new Set(v.map((x) => dec(x, blobs)));
    }
    if (ESCAPED.includes(k)) {
      if (v === null || typeof v !== "object" || Array.isArray(v)) throw new Error(`bad ${k} wrapper`);
      const inner: Record<string, unknown> = {};
      for (const ik of Object.keys(v)) inner[ik] = dec((v as Record<string, unknown>)[ik], blobs);
      return inner;
    }
  }
  const out: Record<string, unknown> = {};
  for (const k of keys) out[k] = dec(o[k], blobs);
  return out;
}

export function decodeParts(json: Uint8Array, blobs: Uint8Array[]): unknown {
  return dec(JSON.parse(td.decode(json)), blobs);
}

// decodeFrame decodes one frame at the start of buf. It returns the value and
// the number of bytes consumed.
export function decodeFrame(buf: Uint8Array): { value: unknown; size: number } {
  const dv = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);
  const trunc = () => new Error("truncated frame");
  if (buf.length < 4) throw trunc();
  const n = dv.getUint32(0);
  if (buf.length < 4 + n + 2) throw trunc();
  const json = buf.subarray(4, 4 + n);
  let p = 4 + n;
  const k = dv.getUint16(p);
  p += 2;
  const blobs: Uint8Array[] = [];
  for (let i = 0; i < k; i++) {
    if (buf.length < p + 4) throw trunc();
    const m = dv.getUint32(p);
    p += 4;
    if (buf.length < p + m) throw trunc();
    blobs.push(buf.subarray(p, p + m));
    p += m;
  }
  return { value: decodeParts(json, blobs), size: p };
}

export function decodeFrames(buf: Uint8Array): unknown[] {
  const out: unknown[] = [];
  let p = 0;
  while (p < buf.length) {
    const { value, size } = decodeFrame(buf.subarray(p));
    out.push(value);
    p += size;
  }
  return out;
}

// FrameReader pulls frames off a byte stream, tolerating arbitrary chunking.
export class FrameReader {
  private reader: ReadableStreamDefaultReader<Uint8Array>;
  private chunks: Uint8Array[] = [];
  private len = 0;
  private eof = false;

  constructor(stream: ReadableStream<Uint8Array>) {
    this.reader = stream.getReader();
  }

  // take returns exactly n bytes, or null on a clean EOF at a frame boundary
  // (only when allowEof is set and nothing is buffered).
  private async take(n: number, allowEof = false): Promise<Uint8Array | null> {
    while (this.len < n) {
      if (this.eof) {
        if (allowEof && this.len === 0) return null;
        throw new Error("truncated frame");
      }
      const r = await this.reader.read();
      if (r.done) {
        this.eof = true;
        continue;
      }
      if (r.value.length) {
        this.chunks.push(r.value);
        this.len += r.value.length;
      }
    }
    const out = new Uint8Array(n);
    let o = 0;
    while (o < n) {
      const c = this.chunks[0];
      const need = n - o;
      if (c.length <= need) {
        out.set(c, o);
        o += c.length;
        this.chunks.shift();
      } else {
        out.set(c.subarray(0, need), o);
        this.chunks[0] = c.subarray(need);
        o += need;
      }
    }
    this.len -= n;
    return out;
  }

  // next resolves to the next decoded frame, or undefined at end of stream.
  async next(): Promise<{ value: unknown } | undefined> {
    const head = await this.take(4, true);
    if (head === null) return undefined;
    const n = new DataView(head.buffer).getUint32(0);
    const body = (await this.take(n + 2))!;
    const json = body.subarray(0, n);
    const k = new DataView(body.buffer, body.byteOffset + n, 2).getUint16(0);
    const blobs: Uint8Array[] = [];
    for (let i = 0; i < k; i++) {
      const lb = (await this.take(4))!;
      const m = new DataView(lb.buffer).getUint32(0);
      blobs.push((await this.take(m))!);
    }
    return { value: decodeParts(json, blobs) };
  }

  async cancel(): Promise<void> {
    try {
      await this.reader.cancel();
    } catch {
      // already closed or errored
    }
  }
}

export async function* readFrames(stream: ReadableStream<Uint8Array>): AsyncGenerator<unknown, void, undefined> {
  const r = new FrameReader(stream);
  try {
    for (;;) {
      const f = await r.next();
      if (!f) return;
      yield f.value;
    }
  } finally {
    await r.cancel();
  }
}
