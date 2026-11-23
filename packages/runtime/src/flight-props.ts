// Encoding of plain-data props crossing from a server component to a client
// component, in the spirit of React Flight. JSON carries strings, finite
// numbers, booleans, null, arrays and plain objects as themselves; every other
// supported value becomes a tagged object { __krateT: <tag>, v: <payload> }
// that the client reviver (clientReviveJS in the compiler) turns back into the
// real value. A plain object that could be mistaken for a tag or a marker is
// wrapped in an "O" tag so user data can never collide with the encoding.

export type FlightHooks = {
  isAction(v: unknown): string | undefined;
  isJSX(v: unknown): boolean;
  fail(path: string, what: string, hint?: string): Error;
};

const RESERVED = ["__krateT", "__krateAction", "__krateBound", "__krateSlotHTML", "__krateSlotTree", "__krateChildren", "__krateTree"];

const TYPED_ARRAYS: Record<string, true> = {
  Int8Array: true, Uint8Array: true, Uint8ClampedArray: true, Int16Array: true, Uint16Array: true,
  Int32Array: true, Uint32Array: true, Float32Array: true, Float64Array: true, BigInt64Array: true, BigUint64Array: true,
};

function tag(t: string, v?: unknown): Record<string, unknown> {
  return v === undefined ? { __krateT: t } : { __krateT: t, v };
}

function toBase64(bytes: Uint8Array): string {
  let s = "";
  for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(s);
}

function bytesOf(buf: ArrayBufferLike, off = 0, len = buf.byteLength - off): string {
  return toBase64(new Uint8Array(buf, off, len));
}

export function describeValue(v: unknown): string {
  if (typeof v === "function") return "a function";
  if (typeof v === "symbol") return "a symbol that is not registered with Symbol.for";
  if (v && typeof v === "object") {
    const ctor = (Object.getPrototypeOf(v) as { constructor?: { name?: string } } | null)?.constructor?.name;
    return ctor ? `an instance of ${ctor}` : "a non-plain object";
  }
  return typeof v;
}

// encodeProp converts one prop value. A top-level undefined is dropped by the
// caller (an absent prop); nested ones are kept as an "U" tag.
export function encodeProp(v: unknown, path: string, hooks: FlightHooks): unknown {
  if (v === null || typeof v === "string" || typeof v === "boolean") return v;
  if (typeof v === "number") {
    if (Object.is(v, -0)) return tag("N", "-0");
    if (Number.isNaN(v)) return tag("N", "NaN");
    if (v === Infinity) return tag("N", "Infinity");
    if (v === -Infinity) return tag("N", "-Infinity");
    return v;
  }
  if (typeof v === "bigint") return tag("B", v.toString());
  if (v === undefined) return tag("U");
  if (typeof v === "symbol") {
    const key = Symbol.keyFor(v);
    if (key === undefined) throw hooks.fail(path, describeValue(v));
    return tag("Y", key);
  }
  const action = hooks.isAction(v);
  if (action !== undefined) {
    // Arguments pre-filled with .bind() travel along; an inline action's sealed
    // closure token is already part of its URL.
    const bound = (v as { $$krateBound?: unknown }).$$krateBound;
    if (Array.isArray(bound) && bound.length > 0 && !action.includes("__krate_args=")) {
      return { __krateAction: action, __krateBound: bound.map((x, i) => encodeProp(x, `${path}.bind[${i}]`, hooks)) };
    }
    return { __krateAction: action };
  }
  if (Array.isArray(v)) return v.map((x, i) => encodeProp(x, `${path}[${i}]`, hooks));
  if (typeof v === "object") {
    if (hooks.isJSX(v)) {
      throw hooks.fail(path, "JSX", "pass JSX as a prop value, an array of JSX, or as children; JSX nested inside an object cannot be sent to a client component");
    }
    const proto = Object.getPrototypeOf(v);
    if (proto === Date.prototype) {
      const t = (v as Date).getTime();
      return tag("D", Number.isNaN(t) ? null : t);
    }
    if (proto === Map.prototype) {
      const entries: unknown[] = [];
      let i = 0;
      for (const [k, x] of v as Map<unknown, unknown>) {
        entries.push([encodeProp(k, `${path}.keys[${i}]`, hooks), encodeProp(x, `${path}.get(${i})`, hooks)]);
        i++;
      }
      return tag("M", entries);
    }
    if (proto === Set.prototype) {
      return tag("S", [...(v as Set<unknown>)].map((x, i) => encodeProp(x, `${path}.values[${i}]`, hooks)));
    }
    if (proto === ArrayBuffer.prototype) return tag("A", bytesOf(v as ArrayBuffer));
    if (ArrayBuffer.isView(v)) {
      if (v instanceof DataView) return tag("V", bytesOf(v.buffer, v.byteOffset, v.byteLength));
      const name = (v as any)[Symbol.toStringTag] as string;
      if (TYPED_ARRAYS[name]) return tag("T", { n: name, b: bytesOf(v.buffer, v.byteOffset, v.byteLength) });
    }
    if (proto !== Object.prototype && proto !== null) throw hooks.fail(path, describeValue(v));
    const out: Record<string, unknown> = {};
    for (const [k, x] of Object.entries(v as Record<string, unknown>)) out[k] = encodeProp(x, `${path}.${k}`, hooks);
    return RESERVED.some((k) => Object.prototype.hasOwnProperty.call(out, k)) ? tag("O", out) : out;
  }
  throw hooks.fail(path, describeValue(v));
}
