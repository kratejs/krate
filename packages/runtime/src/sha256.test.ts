import { describe, expect, it } from "vitest";
import { createHash } from "node:crypto";
import { sha256Hex } from "./sha256.js";

describe("portable sha256", () => {
  it("matches node:crypto across block boundaries and UTF-8", () => {
    for (const s of ["", "abc", "a".repeat(55), "a".repeat(56), "a".repeat(63), "a".repeat(64), "a".repeat(1000), "héllo 世界 😀"]) {
      expect(sha256Hex(s)).toBe(createHash("sha256").update(s).digest("hex"));
    }
  });
  it("error digest fixed vector", () => {
    expect(sha256Hex("boom\nat x").slice(0, 10)).toBe("8a2474a04d");
  });
});
