// Golden frames. Each hex string is the exact output of encodeFrame (JSON key
// order = insertion order, no whitespace). The Go codec must decode each one to
// the described value; for re-encoding, Go must emit the same key order.
import { describe, expect, it } from "vitest";
import { decodeFrame, encodeFrame } from "../src/wire.js";

const fromHex = (h: string) => Uint8Array.from(h.match(/../g) ?? [], (x) => parseInt(x, 16));
const toHex = (b: Uint8Array) => Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");

const vectors: { name: string; value: () => unknown; hex: string }[] = [
  {
    name: "call",
    value: () => ({ calls: [{ id: 1, fn: "tasks.Add", args: ["Buy milk", 2] }] }),
    hex: "0000003b7b2263616c6c73223a5b7b226964223a312c22666e223a227461736b732e416464222c2261726773223a5b22427579206d696c6b222c325d7d5d7d0000",
  },
  {
    name: "blob",
    value: () => ({ id: 1, ok: new Uint8Array([1, 2, 3]) }),
    hex: "000000167b226964223a312c226f6b223a7b222462223a307d7d000100000003010203",
  },
  {
    name: "empty blob and one-byte blob",
    value: () => ({ id: 2, ok: [new Uint8Array(0), new Uint8Array([255])] }),
    hex: "000000217b226964223a322c226f6b223a5b7b222462223a307d2c7b222462223a317d5d7d00020000000000000001ff",
  },
  {
    name: "date",
    value: () => ({ id: 3, ok: new Date("2026-11-11T07:23:15.123Z") }),
    hex: "0000002f7b226964223a332c226f6b223a7b222464223a22323032362d31312d31315430373a32333a31352e3132335a227d7d0000",
  },
  {
    name: "map and set",
    value: () => ({ m: new Map<unknown, unknown>([["k", 1]]), s: new Set([1, 2]) }),
    hex: "000000277b226d223a7b22246d223a5b5b226b222c315d5d7d2c2273223a7b222473223a5b312c325d7d7d0000",
  },
  {
    name: "escaped object {$b: 1}",
    value: () => ({ $b: 1 }),
    hex: "000000107b22246224223a7b222462223a317d7d0000",
  },
  {
    name: "escaped object {$b$: 1}",
    value: () => ({ $b$: 1 }),
    hex: "000000117b22246224223a7b22246224223a317d7d0000",
  },
  {
    name: "stream end",
    value: () => ({ end: true }),
    hex: "0000000c7b22656e64223a747275657d0000",
  },
  {
    name: "error result",
    value: () => ({ id: 2, err: { code: "not_found", message: "no such task", data: null } }),
    hex: "000000487b226964223a322c22657272223a7b22636f6465223a226e6f745f666f756e64222c226d657373616765223a226e6f2073756368207461736b222c2264617461223a6e756c6c7d7d0000",
  },
];

describe("golden vectors", () => {
  for (const v of vectors) {
    it(`${v.name}: encodes to the hex`, () => {
      expect(toHex(encodeFrame(v.value()))).toBe(v.hex);
    });
    it(`${v.name}: decodes and re-encodes byte for byte`, () => {
      const bytes = fromHex(v.hex);
      const { value, size } = decodeFrame(bytes);
      expect(size).toBe(bytes.length);
      expect(value).toEqual(v.value());
      expect(toHex(encodeFrame(value))).toBe(v.hex);
    });
  }
});
