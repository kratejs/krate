// Shared golden vectors: packages/native/wire/testdata is the single source for
// the Go and TS codecs (see its README.md). Every vector is decoded, compared
// with its .json description and re-encoded byte for byte, unless its
// .flags.json says "tsEncode": false.
import { readdirSync, readFileSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { decodeFrame, encodeFrame } from "../src/wire.js";

const dir = fileURLToPath(new URL("../../native/wire/testdata/", import.meta.url));
const names = readdirSync(dir)
  .filter((f) => f.endsWith(".bin"))
  .map((f) => f.slice(0, -4))
  .sort();

const ESCAPED = ["$b$", "$d$", "$m$", "$s$"];

function setOwn(o: Record<string, unknown>, k: string, v: unknown): void {
  Object.defineProperty(o, k, { value: v, enumerable: true, writable: true, configurable: true });
}

// fromDescription turns a .json description into the JS value it stands for.
function fromDescription(v: unknown): unknown {
  if (Array.isArray(v)) return v.map(fromDescription);
  if (v === null || typeof v !== "object") return v;
  const o = v as Record<string, unknown>;
  const keys = Object.keys(o);
  if (keys.length === 1) {
    const k = keys[0];
    const x = o[k];
    if (k === "$b") return new Uint8Array(Buffer.from(x as string, "base64"));
    if (k === "$d") return new Date(x as string);
    if (k === "$m") return new Map((x as unknown[][]).map(([a, b]) => [fromDescription(a), fromDescription(b)]));
    if (k === "$s") return new Set((x as unknown[]).map(fromDescription));
    if (ESCAPED.includes(k)) {
      const inner: Record<string, unknown> = {};
      for (const ik of Object.keys(x as object)) setOwn(inner, ik, fromDescription((x as Record<string, unknown>)[ik]));
      return inner;
    }
  }
  const out: Record<string, unknown> = {};
  for (const k of keys) setOwn(out, k, fromDescription(o[k]));
  return out;
}

const toHex = (b: Uint8Array) => Buffer.from(b).toString("hex");

describe("shared golden vectors", () => {
  it("finds the vector set", () => {
    expect(names.length).toBeGreaterThanOrEqual(30);
  });

  for (const name of names) {
    const bin = new Uint8Array(readFileSync(dir + name + ".bin"));
    const description = JSON.parse(readFileSync(dir + name + ".json", "utf8"));
    const flagsPath = dir + name + ".flags.json";
    const flags: { goEncode?: boolean; tsEncode?: boolean; reason?: string } = existsSync(flagsPath)
      ? JSON.parse(readFileSync(flagsPath, "utf8"))
      : {};

    describe(name, () => {
      it("decodes to its description", () => {
        const { value, size } = decodeFrame(bin);
        expect(size).toBe(bin.length);
        expect(value).toEqual(fromDescription(description));
      });

      if (flags.tsEncode === false) {
        it.skip(`re-encodes byte for byte (skipped: ${flags.reason})`, () => {});
        it("states why the re-encode is skipped", () => {
          expect(flags.reason).toBeTruthy();
        });
      } else {
        it("re-encodes byte for byte", () => {
          const { value } = decodeFrame(bin);
          expect(toHex(encodeFrame(value))).toBe(toHex(bin));
        });
        it("encodes the description's value to the same bytes", () => {
          expect(toHex(encodeFrame(fromDescription(description)))).toBe(toHex(bin));
        });
      }
    });
  }
});
