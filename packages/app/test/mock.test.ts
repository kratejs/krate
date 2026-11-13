import { afterEach, describe, expect, it } from "vitest";
import { createClient } from "../src/client.js";
import { KrateError } from "../src/errors.js";
import { hostSignal } from "../src/host.js";
import { mockHost, resetHostMocks } from "../src/testing.js";

afterEach(resetHostMocks);

const noNet = () => {
  throw new Error("network used");
};

describe("mockHost", () => {
  it("routes calls without network", async () => {
    mockHost("src/lib/tasks", { List: async () => ["a"], Add: (t: string) => t.toUpperCase() });
    const c = createClient({ fetch: noNet as never });
    expect(await c.call("src/lib/tasks.List")).toEqual(["a"]);
    expect(await c.call("src/lib/tasks.Add", ["x"])).toBe("X");
  });

  it("missing export is not_found; unmocked modules still use the network", async () => {
    mockHost("m", {});
    let hit = false;
    const c = createClient({ fetch: (async () => ((hit = true), new Response(null, { status: 500 }))) as never });
    await expect(c.call("m.Nope")).rejects.toMatchObject({ code: "not_found" });
    await c.call("other.X").catch(() => {});
    expect(hit).toBe(true);
  });

  it("thrown KrateError passes through; bad args are rejected like the real codec", async () => {
    mockHost("m", {
      Fail: () => {
        throw new KrateError("nope", "no");
      },
      Id: (x: unknown) => x,
    });
    const c = createClient({ fetch: noNet as never });
    await expect(c.call("m.Fail")).rejects.toMatchObject({ code: "nope" });
    await expect(c.call("m.Id", [() => 1])).rejects.toThrow("argument 0");
  });

  it("streams from async and sync iterables", async () => {
    mockHost("m", {
      A: async function* () {
        yield 1;
        yield 2;
      },
      B: () => [3, 4],
    });
    const c = createClient({ fetch: noNet as never });
    const a: number[] = [];
    for await (const v of c.stream<number>("m.A")) a.push(v);
    const b: number[] = [];
    for await (const v of c.stream<number>("m.B")) b.push(v);
    expect([a, b]).toEqual([[1, 2], [3, 4]]);
  });

  it("signals from host signals and plain values", async () => {
    const count = hostSignal(1);
    mockHost("m", { Count: count, Name: "bob" });
    const c = createClient({ fetch: noNet as never });
    const s = c.signal<number>("m.Count");
    const seen: number[] = [];
    s.subscribe((v) => seen.push(v));
    expect(s()).toBe(1);
    count.set(2);
    expect(s()).toBe(2);
    expect(seen).toEqual([2]);
    expect(c.signal("m.Name")()).toBe("bob");
  });

  it("resetHostMocks restores real routing", async () => {
    mockHost("m", { A: () => 1 });
    resetHostMocks();
    const c = createClient({ fetch: (async () => new Response(null, { status: 500 })) as never });
    await expect(c.call("m.A")).rejects.toMatchObject({ code: "transport" });
  });
});

describe("hostSignal (host side)", () => {
  it("get/set/update/subscribe", () => {
    const s = hostSignal(1);
    const seen: number[] = [];
    const off = s.subscribe((v) => seen.push(v));
    s.set(2);
    s.update((n) => n + 5);
    off();
    s.set(100);
    expect(s.get()).toBe(100);
    expect(seen).toEqual([2, 7]);
  });

  it("is branded so the host entry can register it", () => {
    const s = hostSignal("x");
    expect((s as any).__krateHostSignal).toBe(true);
    expect(Object.keys(s)).not.toContain("__krateHostSignal");
  });
});
