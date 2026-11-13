import { describe, expect, it } from "vitest";
import { FrameReader, decodeFrame, decodeFrames, encodeFrame, readFrames } from "../src/wire.js";

const rt = (v: unknown) => decodeFrame(encodeFrame(v)).value;

function streamOf(chunks: Uint8Array[]): ReadableStream<Uint8Array> {
  let i = 0;
  return new ReadableStream({
    pull(c) {
      if (i < chunks.length) c.enqueue(chunks[i++]);
      else c.close();
    },
  });
}

function cat(...bs: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(bs.reduce((n, b) => n + b.length, 0));
  let p = 0;
  for (const b of bs) {
    out.set(b, p);
    p += b.length;
  }
  return out;
}

describe("codec round trips", () => {
  it("plain JSON values", () => {
    const v = { a: 1, b: [true, null, "x", 1.5], c: { d: "é\u{1F600}" } };
    expect(rt(v)).toEqual(v);
  });

  it("blobs, including empty ones", () => {
    const v = { a: new Uint8Array([1, 2, 3]), b: [new Uint8Array(0), new Uint8Array([255])] };
    const out = rt(v) as typeof v;
    expect(Array.from(out.a)).toEqual([1, 2, 3]);
    expect(out.b[0]).toBeInstanceOf(Uint8Array);
    expect(out.b[0].length).toBe(0);
    expect(Array.from(out.b[1])).toEqual([255]);
  });

  it("ArrayBuffer and typed views become Uint8Array copies", () => {
    const f = new Float32Array([1, 2]);
    const out = rt([new Uint8Array([9]).buffer, new DataView(new Uint8Array([1, 2, 3]).buffer, 1, 2), f]) as Uint8Array[];
    expect(Array.from(out[0])).toEqual([9]);
    expect(Array.from(out[1])).toEqual([2, 3]);
    expect(out[2].length).toBe(8);
  });

  it("Date, Map, Set", () => {
    const v = {
      d: new Date("2026-11-11T07:23:15.123Z"),
      m: new Map<unknown, unknown>([["k", 1], [2, new Set([1, 2])]]),
      s: new Set(["a", new Date(0)]),
    };
    expect(rt(v)).toEqual(v);
  });

  it("escapes user objects that look like tags", () => {
    for (const k of ["$b", "$d", "$m", "$s", "$b$", "$d$", "$m$", "$s$"]) {
      const v = { [k]: 1 };
      expect(rt(v)).toEqual(v);
      expect(rt([v, { x: v }])).toEqual([v, { x: v }]);
    }
    const nested = { $b: new Uint8Array([7]) };
    const out = rt(nested) as typeof nested;
    expect(Array.from(out.$b)).toEqual([7]);
  });

  it("escape wire form", () => {
    const json = (v: unknown) => {
      const b = encodeFrame(v);
      const n = new DataView(b.buffer).getUint32(0);
      return new TextDecoder().decode(b.subarray(4, 4 + n));
    };
    expect(json({ $b: 1 })).toBe('{"$b$":{"$b":1}}');
    expect(json({ $b$: 1 })).toBe('{"$b$":{"$b$":1}}');
    expect(json({ $d: "x", y: 1 })).toBe('{"$d":"x","y":1}');
  });

  it("undefined: dropped in objects, null in arrays", () => {
    expect(rt({ a: undefined, b: 1 })).toEqual({ b: 1 });
    expect(rt([undefined])).toEqual([null]);
  });

  it("decodes several frames back to back", () => {
    const buf = cat(encodeFrame({ a: 1 }), encodeFrame([new Uint8Array([1])]), encodeFrame("z"));
    expect(decodeFrames(buf)).toEqual([{ a: 1 }, [new Uint8Array([1])], "z"]);
  });
});

describe("encode rejections", () => {
  const msg = (v: unknown, root = "argument 1") => {
    try {
      encodeFrame(v, root);
    } catch (e) {
      expect(e).toBeInstanceOf(TypeError);
      return (e as Error).message;
    }
    throw new Error("did not throw");
  };

  it("functions with the argument index and path", () => {
    expect(msg({ a: [1, { f: () => 1 }] })).toBe("argument 1 at .a[1].f: cannot encode a function");
    expect(msg(() => 1, "argument 0")).toBe("argument 0: cannot encode a function");
  });
  it("symbols, bigint", () => {
    expect(msg({ s: Symbol("x") })).toContain("symbol");
    expect(msg(10n)).toContain("bigint");
  });
  it("NaN and Infinity", () => {
    expect(msg({ n: NaN })).toBe("argument 1 at .n: cannot encode NaN (numbers must be finite)");
    expect(msg([Infinity])).toContain("Infinity");
    expect(msg([-Infinity])).toContain("-Infinity");
  });
  it("class instances", () => {
    class Foo {}
    expect(msg({ x: new Foo() })).toBe(
      "argument 1 at .x: cannot encode an instance of Foo (only plain objects, arrays, Date, Map, Set and Uint8Array)",
    );
    expect(msg(new Error("e"))).toContain("Error");
  });
  it("invalid Date and cycles", () => {
    expect(msg(new Date("nope"))).toContain("invalid Date");
    const a: any = {};
    a.self = a;
    expect(msg(a)).toContain("circular");
  });
  it("null-prototype objects are fine", () => {
    const o = Object.create(null);
    o.a = 1;
    expect(rt(o)).toEqual({ a: 1 });
  });
});

describe("decode errors", () => {
  it("truncated frames", () => {
    const b = encodeFrame({ a: new Uint8Array([1, 2, 3]) });
    expect(() => decodeFrame(b.subarray(0, b.length - 1))).toThrow("truncated");
    expect(() => decodeFrame(b.subarray(0, 2))).toThrow("truncated");
  });
  it("bad blob index", () => {
    const j = new TextEncoder().encode('{"$b":3}');
    const b = cat(new Uint8Array([0, 0, 0, j.length]), j, new Uint8Array([0, 0]));
    expect(() => decodeFrame(b)).toThrow("blob index");
  });
});

describe("FrameReader", () => {
  const all = cat(
    encodeFrame({ n: 1 }),
    encodeFrame({ b: new Uint8Array([1, 2, 3, 4, 5]), e: new Uint8Array(0) }),
    encodeFrame({ n: 3 }),
  );

  async function collect(chunks: Uint8Array[]) {
    const out: unknown[] = [];
    for await (const f of readFrames(streamOf(chunks))) out.push(f);
    return out;
  }

  it("one chunk", async () => {
    expect(await collect([all])).toHaveLength(3);
  });

  it("every possible single split point", async () => {
    for (let i = 1; i < all.length; i++) {
      const out = await collect([all.subarray(0, i), all.subarray(i)]);
      expect(out).toHaveLength(3);
      expect((out[0] as any).n).toBe(1);
      expect(Array.from((out[1] as any).b)).toEqual([1, 2, 3, 4, 5]);
    }
  });

  it("one byte at a time", async () => {
    const chunks = Array.from(all, (b) => new Uint8Array([b]));
    expect(await collect(chunks)).toHaveLength(3);
  });

  it("clean EOF yields nothing; mid-frame EOF throws", async () => {
    expect(await collect([])).toEqual([]);
    await expect(collect([all.subarray(0, all.length - 1)])).rejects.toThrow("truncated");
  });

  it("cancel stops reading", async () => {
    const r = new FrameReader(streamOf([all]));
    expect(await r.next()).toBeDefined();
    await r.cancel();
  });
});
