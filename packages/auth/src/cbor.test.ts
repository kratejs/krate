import { describe, expect, it } from "vitest";
import { decodeCbor, encodeCbor } from "./cbor";
import { WebAuthnError, parseAuthData } from "./webauthn";

describe("cbor", () => {
  it("round-trips the types WebAuthn uses", () => {
    const value = new Map<any, any>([
      ["fmt", "none"],
      ["n", -257],
      [1, 2],
      [-3, new Uint8Array([1, 2, 3])],
      ["list", [1, "two", false, true, null]],
      ["big", 4294967296],
      ["neg", -1000000],
    ]);
    const decoded = decodeCbor(encodeCbor(value)).value as Map<any, any>;
    expect(decoded.get("fmt")).toBe("none");
    expect(decoded.get("n")).toBe(-257);
    expect(decoded.get(1)).toBe(2);
    expect(Array.from(decoded.get(-3))).toEqual([1, 2, 3]);
    expect(decoded.get("list")).toEqual([1, "two", false, true, null]);
    expect(decoded.get("big")).toBe(4294967296);
    expect(decoded.get("neg")).toBe(-1000000);
  });

  it("matches known encodings", () => {
    expect(Array.from(encodeCbor(0))).toEqual([0x00]);
    expect(Array.from(encodeCbor(23))).toEqual([0x17]);
    expect(Array.from(encodeCbor(24))).toEqual([0x18, 0x18]);
    expect(Array.from(encodeCbor(-1))).toEqual([0x20]);
    expect(Array.from(encodeCbor(-257))).toEqual([0x39, 0x01, 0x00]);
    expect(Array.from(encodeCbor("a"))).toEqual([0x61, 0x61]);
    expect(Array.from(encodeCbor(new Map()))).toEqual([0xa0]);
  });

  it("reports how many bytes it consumed", () => {
    const first = encodeCbor(new Map<any, any>([[1, 2]]));
    const joined = new Uint8Array([...first, 0xff, 0xff]);
    expect(decodeCbor(joined).length).toBe(first.length);
  });

  it("rejects truncated, indefinite, tagged and over-deep input", () => {
    expect(() => decodeCbor(new Uint8Array([0x62, 0x61]))).toThrow();
    expect(() => decodeCbor(new Uint8Array([0x5f, 0xff]))).toThrow();
    expect(() => decodeCbor(new Uint8Array([0xc1, 0x00]))).toThrow();
    expect(() => decodeCbor(new Uint8Array([0xfb, 0, 0, 0, 0, 0, 0, 0, 0]))).toThrow();
    expect(() => decodeCbor(new Uint8Array([]))).toThrow();
    expect(() => decodeCbor(new Uint8Array(40).fill(0x81))).toThrow("too deep");
    expect(() => decodeCbor(new Uint8Array([0x99, 0xff, 0xff]))).toThrow("too large");
  });
});

describe("authenticator data", () => {
  it("rejects short and truncated data", () => {
    expect(() => parseAuthData(new Uint8Array(10))).toThrow(WebAuthnError);
    const truncated = new Uint8Array(40);
    truncated[32] = 0x41;
    expect(() => parseAuthData(truncated)).toThrow("truncated");
    const badLength = new Uint8Array(60);
    badLength[32] = 0x41;
    new DataView(badLength.buffer).setUint16(53, 5000);
    expect(() => parseAuthData(badLength)).toThrow("credential id length");
  });

  it("reads flags and the counter", () => {
    const data = new Uint8Array(37);
    data[32] = 0x01 | 0x04 | 0x08 | 0x10;
    new DataView(data.buffer).setUint32(33, 42);
    const parsed = parseAuthData(data);
    expect(parsed.flags).toMatchObject({ up: true, uv: true, be: true, bs: true, at: false, ed: false });
    expect(parsed.signCount).toBe(42);
    expect(parsed.attested).toBeUndefined();
  });
});
