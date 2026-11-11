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

  it("separate ticks make separate requests", async () => {
    const { c, host } = setup({ "m.A": () => "a" });
    await c.call("m.A");
    await c.call("m.A");
    expect(callCount(host)).toBe(2);
  });

  it("a fast call resolves before a slow one in the same batch", async () => {
    const slow = deferred<string>();
    const { c, host } = setup({ "m.Slow": () => slow.promise, "m.Fast": () => "fast" });
    const order: string[] = [];
    const ps = Promise.all([
      c.call("m.Slow").then((v) => order.push("slow:" + v)),
      c.call("m.Fast").then((v) => order.push("fast:" + v)),
    ]);
    await flushAll();
    expect(order).toEqual(["fast:fast"]);
    slow.resolve("done");
    await ps;
    expect(order).toEqual(["fast:fast", "slow:done"]);
    expect(callCount(host)).toBe(1);
  });

  it("out-of-order results reach the right promises", async () => {
    const gates = [deferred<number>(), deferred<number>(), deferred<number>()];
    const { c } = setup({ "m.G": ([i]) => gates[i].promise });
    const ps = [0, 1, 2].map((i) => c.call<number>("m.G", [i]));
    await tick();
    gates[2].resolve(20);
    gates[0].resolve(0);
    gates[1].resolve(10);
    expect(await Promise.all(ps)).toEqual([0, 10, 20]);
  });

  it("dedupes identical in-flight calls when asked", async () => {
    const gate = deferred<number>();
    let runs = 0;
    const { c, host } = setup({ "m.Read": () => (runs++, gate.promise) });
    const a = c.call("m.Read", [1], { dedupe: true });
    const b = c.call("m.Read", [1], { dedupe: true });
    const other = c.call("m.Read", [2], { dedupe: true });
    await tick();
    const late = c.call("m.Read", [1], { dedupe: true });
    gate.resolve(7);
    expect(await Promise.all([a, b, late, other])).toEqual([7, 7, 7, 7]);
    expect(runs).toBe(2);
    expect(host.batches[0]).toHaveLength(2);
  });

  it("does not dedupe without the flag", async () => {
    let runs = 0;
    const { c } = setup({ "m.Read": () => ++runs });
    await Promise.all([c.call("m.Read", [1]), c.call("m.Read", [1])]);
    expect(runs).toBe(2);
  });

  it("one aborted deduped caller does not cancel the shared call", async () => {
    const gate = deferred<number>();
    const { c, host } = setup({ "m.Read": () => gate.promise });
    const ac = new AbortController();
    const a = c.call("m.Read", [], { dedupe: true, signal: ac.signal });
    const b = c.call("m.Read", [], { dedupe: true });
    await tick();
    ac.abort();
    await expect(a).rejects.toMatchObject({ name: "AbortError" });
    await tick();
    expect(host.cancelledIds).toEqual([]);
    gate.resolve(1);
    expect(await b).toBe(1);
  });

  it("an encode error rejects only that call", async () => {
    const { c, host } = setup({ "m.A": () => "a" });
    const bad = c.call("m.A", [1, () => 1]);
    const good = c.call("m.A", [new Uint8Array([1])]);
    await expect(bad).rejects.toThrow("argument 1: cannot encode a function");
    expect(await good).toBe("a");
    expect(host.batches[0]).toHaveLength(1);
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

  it("transport failure", async () => {
    c1();
    function c1() {
      client = createClient({
        fetch: async () => {
          throw new TypeError("network down");
        },
      });
    }
    const e = await client!.call("m.A").catch((x) => x);
    expect(e).toBeInstanceOf(KrateError);
    expect(e).toMatchObject({ code: "transport", message: "network down" });
  });

  it("non-200 with an error frame", async () => {
    const { encodeFrame } = await import("../src/wire.js");
    client = createClient({
      fetch: async () => new Response(encodeFrame({ error: { code: "bad_request", message: "bad frame" } }) as BodyInit, { status: 400 }),
    });
    expect(await client.call("m.A").catch((x) => x)).toMatchObject({ code: "bad_request", message: "bad frame" });
  });

  it("non-200 without a frame", async () => {
    client = createClient({ fetch: async () => new Response("oops", { status: 502 }) });
    expect(await client.call("m.A").catch((x) => x)).toMatchObject({ code: "transport", message: "host returned HTTP 502" });
  });

  it("a call without a result frame gets 'no result'", async () => {
    const { encodeFrame } = await import("../src/wire.js");
    client = createClient({
      fetch: async (_u, init) => {
        const body = init!.body as Uint8Array;
        void body;
        return new Response(encodeFrame({ id: 99999, ok: 1 }) as BodyInit);
      },
    });
    const [a, b] = await Promise.all([client.call("m.A").catch((x) => x), client.call("m.B").catch((x) => x)]);
    expect(a).toMatchObject({ code: "transport", message: "no result" });
    expect(b).toMatchObject({ code: "transport", message: "no result" });
  });

  it("response cut mid-frame is a transport error", async () => {
    const { encodeFrame } = await import("../src/wire.js");
    client = createClient({
      fetch: async () => new Response(encodeFrame({ id: 1, ok: 1 }).subarray(0, 5) as BodyInit),
    });
    expect(await client.call("m.A").catch((x) => x)).toMatchObject({ code: "transport" });
  });
});

describe("cancellation", () => {
  it("abort before the call sends nothing", async () => {
    const { c, host } = setup({ "m.A": () => 1 });
    const ac = new AbortController();
    ac.abort();
    await expect(c.call("m.A", [], { signal: ac.signal })).rejects.toMatchObject({ name: "AbortError" });
    await flushAll();
    expect(host.requests).toEqual([]);
  });

  it("abort with a custom reason rejects with it", async () => {
    const { c } = setup({ "m.A": () => 1 });
    const ac = new AbortController();
    ac.abort(new Error("why"));
    await expect(c.call("m.A", [], { signal: ac.signal })).rejects.toThrow("why");
  });

  it("abort in the same tick drops the call from the batch", async () => {
    const { c, host } = setup({ "m.A": () => 1, "m.B": () => 2 });
    const ac = new AbortController();
    const a = c.call("m.A", [], { signal: ac.signal });
    const b = c.call("m.B");
    ac.abort();
    await expect(a).rejects.toMatchObject({ name: "AbortError" });
    expect(await b).toBe(2);
    expect(host.batches[0]).toHaveLength(1);
    expect(host.cancelledIds).toEqual([]);
  });

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
  async function* gen(n: number, then?: () => void) {
    for (let i = 0; i < n; i++) yield i;
    then?.();
  }

  it("yields values then ends", async () => {
    const { c, host } = setup({ "m.Count": ([n]) => gen(n) });
    const got: number[] = [];
    for await (const v of c.stream<number>("m.Count", [3])) got.push(v);
    expect(got).toEqual([0, 1, 2]);
    expect(host.requests).toContain("GET /__krate/stream/s1");
  });

  it("is lazy until iterated", async () => {
    const { c, host } = setup({ "m.Count": ([n]) => gen(n) });
    c.stream("m.Count", [1]);
    await flushAll();
    expect(host.requests).toEqual([]);
  });

  it("an error frame throws KrateError", async () => {
    async function* bad() {
      yield 1;
      throw new KrateError("boom", "mid-stream", { x: 1 });
    }
    const { c } = setup({ "m.Bad": () => bad() });
    const it = c.stream<number>("m.Bad");
    expect(await it.next()).toEqual({ done: false, value: 1 });
    await expect(it.next()).rejects.toMatchObject({ name: "KrateError", code: "boom", data: { x: 1 } });
    expect(await it.next()).toEqual({ done: true, value: undefined });
  });

  it("call error before the stream opens", async () => {
    const { c } = setup({
      "m.No": () => {
        throw new KrateError("denied", "no");
      },
    });
    await expect(c.stream("m.No")[Symbol.asyncIterator]().next()).rejects.toMatchObject({ code: "denied" });
  });

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

  it("natural end does not send cancel", async () => {
    const { c, host } = setup({ "m.Count": () => gen(2) });
    for await (const _ of c.stream("m.Count"));
    await flushAll();
    expect(host.cancelledStreams).toEqual([]);
  });

  it("abort signal ends a pending next() with the abort reason", async () => {
    const gate = deferred();
    async function* stuck() {
      yield 1;
      await gate.promise;
      yield 2;
    }
    const { c, host } = setup({ "m.Stuck": () => stuck() });
    const ac = new AbortController();
    const it = c.stream<number>("m.Stuck", [], { signal: ac.signal });
    expect(await it.next()).toEqual({ done: false, value: 1 });
    const pending = it.next();
    await tick();
    ac.abort();
    await expect(pending).rejects.toMatchObject({ name: "AbortError" });
    await flushAll();
    expect(host.cancelledStreams).toEqual(["s1"]);
    gate.resolve();
  });

  it("abort before the first next rejects without a request", async () => {
    const { c, host } = setup({ "m.Count": () => gen(1) });
    const ac = new AbortController();
    ac.abort();
    await expect(c.stream("m.Count", [], { signal: ac.signal }).next()).rejects.toMatchObject({ name: "AbortError" });
    expect(host.requests).toEqual([]);
  });

  it("a plain result from stream() is a protocol error", async () => {
    const { c } = setup({ "m.Plain": () => [1] });
    await expect(c.stream("m.Plain").next()).rejects.toMatchObject({ code: "protocol" });
  });

  it("a stream result from call() is a protocol error and is cancelled", async () => {
    const { c, host } = setup({ "m.Count": () => gen(1) });
    await expect(c.call("m.Count")).rejects.toMatchObject({ code: "protocol" });
    await flushAll();
    expect(host.cancelledStreams).toEqual(["s1"]);
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

  it("same name returns the same signal; unsubscribe works", async () => {
    const { c, host } = setup({}, { "m.Count": 1 });
    const s = c.signal<number>("m.Count");
    expect(c.signal("m.Count")).toBe(s);
    const seen: number[] = [];
    const off = s.subscribe((v) => seen.push(v));
    await flushAll();
    off();
    host.setSignal("m.Count", 5);
    await flushAll();
    expect(seen).toEqual([1]);
    expect(s()).toBe(5);
  });

  it("adding a name reconnects with the full set", async () => {
    const { c, host } = setup({}, { "m.A": 1, "m.B": 2 });
    const a = c.signal<number>("m.A");
    await flushAll();
    const b = c.signal<number>("m.B");
    await flushAll();
    expect(host.signalConnections).toEqual([["m.A"], ["m.A", "m.B"]]);
    expect(a()).toBe(1);
    expect(b()).toBe(2);
    host.setSignal("m.B", 3);
    await flushAll();
    expect(b()).toBe(3);
  });

  it("reconnects with backoff after a drop and picks up current values", async () => {
    vi.useFakeTimers();
    const { c, host } = setup({}, { "m.A": 1 });
    const a = c.signal<number>("m.A");
    await vi.advanceTimersByTimeAsync(0);
    expect(a()).toBe(1);
    host.dropSignals();
    host.setSignal("m.A", 2);
    await vi.advanceTimersByTimeAsync(50);
    expect(host.signalConnections).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(60);
    expect(host.signalConnections).toHaveLength(2);
    expect(a()).toBe(2);
  });

  it("backoff doubles up to 5s while the host is down", async () => {
    vi.useFakeTimers();
    let attempts = 0;
    client = createClient({
      fetch: async () => {
        attempts++;
        throw new TypeError("down");
      },
    });
    client.signal("m.A");
    await vi.advanceTimersByTimeAsync(0);
    expect(attempts).toBe(1);
    // delays: 100, 200, 400, 800, 1600, 3200, 5000, 5000
    const expected = [100, 300, 700, 1500, 3100, 6300, 11300, 16300];
    for (let i = 0; i < expected.length; i++) {
      await vi.advanceTimersByTimeAsync(expected[i] - (i ? expected[i - 1] : 0) - 1);
      expect(attempts).toBe(i + 1);
      await vi.advanceTimersByTimeAsync(1);
      expect(attempts).toBe(i + 2);
    }
  });

  it("uses an injected createSignal", async () => {
    const host = createFakeHost({}, { "m.A": 4 });
    const writes: unknown[] = [];
    client = createClient({
      fetch: host.fetch,
      createSignal: <T>(init: T) => {
        let v = init;
        return [() => v, (n: T) => (writes.push(n), (v = n))] as [() => T, (v: T) => void];
      },
    });
    const s = client.signal<number>("m.A");
    await flushAll();
    expect(s()).toBe(4);
    expect(writes).toEqual([4]);
  });
});
