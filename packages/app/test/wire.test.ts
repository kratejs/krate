import { describe, expect, it } from "vitest";
import { decodeFrame, encodeFrame, readFrames } from "../src/wire.js";

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
});

describe("decode errors", () => {
  it("truncated frames", () => {
    const b = encodeFrame({ a: new Uint8Array([1, 2, 3]) });
    expect(() => decodeFrame(b.subarray(0, b.length - 1))).toThrow("truncated");
    expect(() => decodeFrame(b.subarray(0, 2))).toThrow("truncated");
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

  it("every possible single split point", async () => {
    for (let i = 1; i < all.length; i++) {
      const out = await collect([all.subarray(0, i), all.subarray(i)]);
      expect(out).toHaveLength(3);
      expect((out[0] as any).n).toBe(1);
      expect(Array.from((out[1] as any).b)).toEqual([1, 2, 3, 4, 5]);
    }
  });
});
