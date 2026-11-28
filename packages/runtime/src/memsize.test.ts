import { describe, expect, it } from "vitest";
import { approxBytes, cacheByteBudgets, cacheBytesFromEnv, DEFAULT_CACHE_BYTES } from "./memsize.js";
import { SegmentedLRU } from "./segmented-lru.js";
import { DataCache } from "./cache-scope.js";
import { ISRCache } from "./ssr-core.js";

const bytes = { maxBytes: 100, sizeOf: (s: string) => s.length };

describe("approxBytes", () => {
  it("counts string characters and grows with the value", () => {
    expect(approxBytes("x".repeat(1000))).toBeGreaterThanOrEqual(1000);
    expect(approxBytes({ rows: ["a".repeat(500), "b".repeat(500)] })).toBeGreaterThan(1000);
    expect(approxBytes(new Uint8Array(4096))).toBeGreaterThanOrEqual(4096);
  });

  it("terminates on cycles", () => {
    const a: Record<string, unknown> = { s: "abc" };
    a.self = a;
    expect(approxBytes(a)).toBeGreaterThan(0);
  });
});

describe("cacheByteBudgets", () => {
  it("splits the total between pages, regions and data", () => {
    expect(cacheByteBudgets()).toEqual({ pages: DEFAULT_CACHE_BYTES / 2, regions: DEFAULT_CACHE_BYTES / 4, data: DEFAULT_CACHE_BYTES / 4 });
    expect(cacheByteBudgets(400)).toEqual({ pages: 200, regions: 100, data: 100 });
    expect(cacheBytesFromEnv({ KRATE_SSR_CACHE_BYTES: "1048576" })).toBe(1048576);
    expect(cacheBytesFromEnv({ KRATE_SSR_CACHE_BYTES: "nope" })).toBe(0);
  });
});

describe("SegmentedLRU byte limit", () => {
  it("evicts the coldest entries until the new one fits", () => {
    const c = new SegmentedLRU<string>(100, bytes);
    c.set("a", "x".repeat(40));
    c.set("b", "x".repeat(40));
    const evicted = c.set("c", "x".repeat(40));
    expect(evicted.map(([k]) => k)).toEqual(["a"]);
    expect(c.keys().sort()).toEqual(["b", "c"]);
    expect(c.bytes).toBe(80);
  });

  it("does not store an entry larger than the whole limit, and drops its stale copy", () => {
    const c = new SegmentedLRU<string>(100, bytes);
    c.set("a", "small");
    expect(c.set("big", "x".repeat(101))).toEqual([]);
    expect(c.has("big")).toBe(false);
    expect(c.set("a", "x".repeat(200)).map(([k]) => k)).toEqual(["a"]);
    expect(c.has("a")).toBe(false);
    expect(c.bytes).toBe(0);
  });

  it("keeps the protected entries when one-off entries churn", () => {
    const c = new SegmentedLRU<string>(1000, bytes);
    c.set("hot", "x".repeat(30));
    c.get("hot");
    for (let i = 0; i < 20; i++) c.set("once" + i, "x".repeat(30));
    expect(c.has("hot")).toBe(true);
    expect(c.bytes).toBeLessThanOrEqual(100);
  });

  it("evicts others when an entry grows in place", () => {
    const c = new SegmentedLRU<string>(100, bytes);
    c.set("a", "x".repeat(40));
    c.set("b", "x".repeat(40));
    expect(c.set("b", "x".repeat(90)).map(([k]) => k)).toEqual(["a"]);
    expect(c.bytes).toBe(90);
  });

  it("tracks bytes through delete and clear", () => {
    const c = new SegmentedLRU<string>(100, bytes);
    c.set("a", "x".repeat(10));
    c.set("b", "x".repeat(20));
    c.delete("a");
    expect(c.bytes).toBe(20);
    c.clear();
    expect(c.bytes).toBe(0);
  });
});

describe("ISRCache byte limit", () => {
  it("keeps the rendered pages under the budget and their tag index in step", () => {
    const c = new ISRCache(512, 10_000);
    for (let i = 0; i < 10; i++) c.set("/p/" + i, { html: "x".repeat(3000), timestamp: 0, tags: ["t"] });
    expect(c.bytes).toBeLessThanOrEqual(10_000);
    expect(c.get("/p/9")).toBeDefined();
    expect(c.get("/p/0")).toBeUndefined();
  });
});

describe("DataCache byte limit", () => {
  it("evicts the oldest values to stay under the budget", () => {
    const d = new DataCache(1000, 5000);
    for (let i = 0; i < 10; i++) d.set("k" + i, { rows: "x".repeat(1000) }, 60, ["t"]);
    expect(d.bytes).toBeLessThanOrEqual(5000);
    expect(d.get("k9")).toBeDefined();
    expect(d.get("k0")).toBeUndefined();
    d.clear();
    expect(d.bytes).toBe(0);
  });

  it("skips a value larger than the budget", () => {
    const d = new DataCache(1000, 100);
    d.set("big", "x".repeat(500), 60, []);
    expect(d.get("big")).toBeUndefined();
    expect(d.size).toBe(0);
  });
});
