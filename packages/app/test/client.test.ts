import { afterEach, describe, expect, it, vi } from "vitest";
import { createClient, type Client } from "../src/client.js";
import { KrateError } from "../src/errors.js";
import { createFakeHost } from "./fakeserver.js";
import { deferred, flushAll, tick } from "./helpers.js";

let client: Client | undefined;
afterEach(() => {
  client?.close();
  client = undefined;
  vi.useRealTimers();
});

function setup(registry: Parameters<typeof createFakeHost>[0], signals: Record<string, unknown> = {}) {
  const host = createFakeHost(registry, signals);
  client = createClient({ fetch: host.fetch, windowId: "w1" });
  return { host, c: client };
}

const callCount = (h: { requests: string[] }) => h.requests.filter((r) => r === "POST /__krate/call").length;

describe("call", () => {
  it("round trips values including blobs", async () => {
    const { c, host } = setup({ "m.Echo": (a) => a });
    const out = await c.call<any[]>("m.Echo", [1, new Uint8Array([1, 2]), new Date(5), new Map([["a", 1]])]);
    expect(out[0]).toBe(1);
    expect(Array.from(out[1])).toEqual([1, 2]);
    expect(out[2]).toEqual(new Date(5));
    expect(out[3]).toEqual(new Map([["a", 1]]));
    expect(host.windows[0]).toBe("w1");
  });

  it("batches calls from the same tick into one request", async () => {
    const { c, host } = setup({ "m.A": () => "a", "m.B": () => "b", "m.C": () => "c" });
    const r = await Promise.all([c.call("m.A"), c.call("m.B"), c.call("m.C")]);
    expect(r).toEqual(["a", "b", "c"]);
    expect(callCount(host)).toBe(1);
    expect(host.batches[0]).toHaveLength(3);
  });
});

describe("errors", () => {
  it("maps err frames to KrateError", async () => {
    const { c } = setup({
      "m.Nope": () => {
        throw new KrateError("not_found", "no such task", { id: 3 });
      },
      "m.Boom": () => {
        throw new Error("kaput");
      },
    });
    const e = await c.call("m.Nope").catch((x) => x);
    expect(e).toBeInstanceOf(KrateError);
    expect(e).toBeInstanceOf(Error);
    expect(e).toMatchObject({ code: "not_found", message: "no such task", data: { id: 3 } });
    expect(await c.call("m.Boom").catch((x) => x)).toMatchObject({ code: "internal", message: "kaput" });
    expect(await c.call("m.Missing").catch((x) => x)).toMatchObject({ code: "not_found" });
  });
});

describe("cancellation", () => {

  it("abort during a call rejects and posts cancel", async () => {
    const gate = deferred();
    let sawAbort = false;
    const { c, host } = setup({
      "m.Slow": (_a, ctx) => {
        ctx.signal.addEventListener("abort", () => (sawAbort = true));
        return gate.promise;
      },
    });
    const ac = new AbortController();
    const p = c.call("m.Slow", [], { signal: ac.signal });
    await flushAll();
    ac.abort();
    await expect(p).rejects.toMatchObject({ name: "AbortError" });
    await flushAll();
    expect(host.requests).toContain("POST /__krate/cancel");
    expect(host.cancelledIds).toHaveLength(1);
    expect(sawAbort).toBe(true);
    gate.resolve();
  });
});

describe("streams", () => {

  it("breaking out sends cancel and closes the fetch", async () => {
    let cleaned = false;
    async function* forever() {
      try {
        for (let i = 0; ; i++) {
          await tick();
          yield i;
        }
      } finally {
        cleaned = true;
      }
    }
    const { c, host } = setup({ "m.Forever": () => forever() });
    for await (const v of c.stream<number>("m.Forever")) {
      if (v === 2) break;
    }
    await flushAll();
    expect(host.cancelledStreams).toEqual(["s1"]);
    expect(host.closedStreams).toEqual(["s1"]);
    expect(cleaned).toBe(true);
  });
});

describe("signals", () => {
  it("delivers initial values and changes on one connection", async () => {
    const { c, host } = setup({}, { "m.Count": 1, "m.Name": "a" });
    const count = c.signal<number>("m.Count");
    const name = c.signal<string>("m.Name");
    expect(count()).toBeUndefined();
    const seen: number[] = [];
    count.subscribe((v) => seen.push(v));
    await flushAll();
    expect(count()).toBe(1);
    expect(name()).toBe("a");
    expect(host.signalConnections).toEqual([["m.Count", "m.Name"]]);
    host.setSignal("m.Count", 2);
    await flushAll();
    expect(count()).toBe(2);
    expect(seen).toEqual([1, 2]);
  });
});
