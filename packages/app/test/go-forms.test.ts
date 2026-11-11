// Forms the Go codec emits that JSON.stringify does not, and forms JSON.stringify
// emits that Go does not. The shared vectors only hold forms both sides emit.
import { describe, expect, it } from "vitest";
import { decodeFrame, decodeParts, encodeFrame } from "../src/wire.js";

const te = new TextEncoder();
const parts = (json: string) => decodeParts(te.encode(json), []);

describe("decoding what Go emits", () => {
  it("accepts Go string escapes", () => {
    expect(parts('"a\\u2028b\\u2029c\\ufffdd"')).toBe("a\u2028b\u2029c\ufffdd");
    expect(parts('"\\u00e9\\u00E9\\/"')).toBe("\u00e9\u00e9/");
    expect(parts('"\\ud83d\\ude00"')).toBe("\u{1F600}");
    expect(parts('"\\u0000\\u001f\\u007f"')).toBe("\u0000\u001f\u007f");
  });

  it("accepts what JSON.stringify emits for lone surrogates and raw U+2028", () => {
    const lone = JSON.stringify("\ud800x\udc00");
    expect(lone).toBe('"\\ud800x\\udc00"');
    expect(parts(lone)).toBe("\ud800x\udc00");
    expect(parts(JSON.stringify("\u2028\u2029"))).toBe("\u2028\u2029");
  });

  it("decodes -0 and Go number spellings", () => {
    expect(Object.is(parts("-0"), -0)).toBe(true);
    expect(parts("[1E5,1e+21,5e-324,-1.5E-3,9007199254740991]")).toEqual([1e5, 1e21, 5e-324, -1.5e-3, 9007199254740991]);
  });

  it("encodes -0 as 0 (JSON.stringify)", () => {
    const { value } = decodeFrame(encodeFrame([-0]));
    expect(Object.is((value as number[])[0], 0)).toBe(true);
  });

  it("accepts any RFC 3339 date", () => {
    const want = Date.UTC(2026, 10, 11, 7, 23, 15, 123);
    for (const s of [
      "2026-11-11T07:23:15.123Z",
      "2026-11-11T07:23:15.123000Z",
      "2026-11-11T08:23:15.123+01:00",
      "2026-11-11T02:23:15.123-05:00",
      "2026-11-11T07:23:15.123000000Z",
    ]) {
      expect((parts(`{"$d":"${s}"}`) as Date).getTime()).toBe(want);
    }
    expect((parts('{"$d":"2026-11-11T07:23:15Z"}') as Date).getTime()).toBe(want - 123);
  });

  it("rejects dates that are not RFC 3339", () => {
    for (const s of ["2026-11-11", "2026-11-11 07:23:15Z", "2026-11-11T07:23:15", "x", "2026-13-11T07:23:15Z", "Nov 11 2026"]) {
      expect(() => parts(`{"$d":"${s}"}`)).toThrow();
    }
  });

  it("emits exactly three fractional digits and refuses years outside 0000-9999", () => {
    const f = encodeFrame(new Date(Date.UTC(2026, 10, 11, 7, 23, 15, 5)));
    expect(new TextDecoder().decode(f.subarray(4, f.length - 2))).toBe('{"$d":"2026-11-11T07:23:15.005Z"}');
    expect(() => encodeFrame(new Date(Date.UTC(10000, 0, 1)))).toThrow(/year/);
    expect(() => encodeFrame(new Date(-1e14))).toThrow(/year/);
  });
});

describe("escapes", () => {
  const json = (v: unknown) => {
    const f = encodeFrame(v);
    return new TextDecoder().decode(f.subarray(4, f.length - 2));
  };

  it("wraps a sole tag key and a sole escaped key for all four tags", () => {
    for (const tag of ["$b", "$d", "$m", "$s"]) {
      expect(json({ [tag]: 1 })).toBe(`{"${tag}$":{"${tag}":1}}`);
      expect(json({ [tag + "$"]: 1 })).toBe(`{"${tag}$":{"${tag}$":1}}`);
      expect(json({ [tag]: 1, y: 2 })).toBe(`{"${tag}":1,"y":2}`);
      expect(json({ y: 2, [tag]: 1 })).toBe(`{"y":2,"${tag}":1}`);
      expect(decodeFrame(encodeFrame({ [tag]: 1 })).value).toEqual({ [tag]: 1 });
      expect(decodeFrame(encodeFrame({ [tag + "$"]: 1 })).value).toEqual({ [tag + "$"]: 1 });
    }
  });

  it("does not reinterpret the keys of a wrapped object", () => {
    expect(parts('{"$d$":{"$d":{"$d":"2026-11-11T07:23:15.123Z"}}}')).toEqual({ $d: new Date("2026-11-11T07:23:15.123Z") });
  });

  it("rejects a wrapper that does not hold an object", () => {
    for (const j of ['{"$b$":1}', '{"$s$":[]}', '{"$m$":null}', '{"$d$":"x"}']) expect(() => parts(j)).toThrow();
  });

  it("keeps a __proto__ key as data", () => {
    const v = parts('{"__proto__":{"a":1},"b":2}') as Record<string, unknown>;
    expect(Object.getPrototypeOf(v)).toBe(Object.prototype);
    expect(Object.keys(v)).toEqual(["__proto__", "b"]);
    const o = JSON.parse('{"__proto__":{"a":1}}');
    const f = encodeFrame(o);
    expect(new TextDecoder().decode(f.subarray(4, f.length - 2))).toBe('{"__proto__":{"a":1}}');
  });

  it("emits integer-like keys first, ascending (JS key order)", () => {
    const f = encodeFrame({ b: 1, 2: 1, 1: 1 });
    expect(new TextDecoder().decode(f.subarray(4, f.length - 2))).toBe('{"1":1,"2":1,"b":1}');
  });
});

describe("maps", () => {
  it("decodes plain objects (what Go sends for a map)", () => {
    expect(parts('{"a":1,"b":{"c":2}}')).toEqual({ a: 1, b: { c: 2 } });
  });
});
