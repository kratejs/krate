import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The in-process path: TS host code runs in QuickJS where hostjs installs
// __krateHost.call/stream/signal. @krate/app picks it up by itself.
const calls: Array<[string, unknown[]]> = [];
const bridge = {
  call: vi.fn(async (id: string, args: unknown[] = []) => {
    calls.push([id, args]);
    if (id === "krate/fs.readText") return "contents of " + args[0];
    if (id === "boom") throw Object.assign(new Error("denied"), { code: "permission_denied", data: { permission: "fs" } });
    return null;
  }),
  stream: (id: string) =>
    (async function* () {
      yield id + ":1";
      yield id + ":2";
    })(),
  signal: (_id: string) => {
    let v = "init";
    const subs = new Set<(x: string) => void>();
    return { get: () => v, subscribe: (f: (x: string) => void) => (subs.add(f), f(v), () => subs.delete(f)), set: (x: string) => ((v = x), subs.forEach((f) => f(x))) };
  },
};

beforeEach(() => {
  calls.length = 0;
  vi.resetModules();
  (globalThis as any).__krateHost = bridge;
});
afterEach(() => {
  delete (globalThis as any).__krateHost;
});

describe("default client in host code", () => {
  it("SDK wrappers dispatch through __krateHost.call with no HTTP", async () => {
    const fetchSpy = vi.fn(() => {
      throw new Error("HTTP used");
    });
    vi.stubGlobal("fetch", fetchSpy);
    const { readText } = await import("../src/fs.js");
    expect(await readText("/a.txt")).toBe("contents of /a.txt");
    expect(calls).toEqual([["krate/fs.readText", ["/a.txt"]]]);
    expect(fetchSpy).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });

  it("user host functions, streams and errors", async () => {
    const { call, stream } = await import("../src/index.js");
    const got: unknown[] = [];
    for await (const v of stream("src/lib/x.Feed")) got.push(v);
    expect(got).toEqual(["src/lib/x.Feed:1", "src/lib/x.Feed:2"]);
    await expect(call("boom")).rejects.toMatchObject({ name: "KrateError", code: "permission_denied", data: { permission: "fs" } });
  });

  it("signals read through the bridge", async () => {
    const { signal } = await import("../src/index.js");
    const s = signal<string>("src/lib/x.status");
    expect(s()).toBe("init");
    const seen: string[] = [];
    s.subscribe((v) => seen.push(v));
    expect(seen).toEqual(["init"]);
  });

  it("mocks still win in tests", async () => {
    const { call } = await import("../src/index.js");
    const { mockHost, resetHostMocks } = await import("../src/testing.js");
    mockHost("src/lib/tasks", { List: () => ["m"] });
    expect(await call("src/lib/tasks.List")).toEqual(["m"]);
    expect(calls).toEqual([]);
    resetHostMocks();
  });

  it("without __krateHost the HTTP client is used", async () => {
    delete (globalThis as any).__krateHost;
    const { hostBridge } = await import("../src/client.js");
    expect(hostBridge()).toBeUndefined();
  });
});
