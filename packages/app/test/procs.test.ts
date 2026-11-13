import { describe, expect, it } from "vitest";
import { installFakeHost } from "./sdkhost.js";
import { createChannel, transferPort, portFromId } from "../src/port.js";
import { startWorker, listWorkers } from "../src/worker.js";
import { captureFrame, openOffscreen } from "../src/offscreen.js";
import { automationEndpoint, cdp } from "../src/automation.js";
import { declaredExtensions, loadExtension } from "../src/extension.js";

async function* once<T>(...vs: T[]): AsyncGenerator<T> {
  for (const v of vs) yield v;
}

describe("port", () => {
  it("createChannel, transferPort and post map to their ids", async () => {
    const { seen } = installFakeHost(
      {
        "krate/port.channel": () => ({ a: "ch1a", b: "ch1b" }),
        "krate/port.transfer": () => null,
        "krate/port.post": () => null,
        "krate/port.close": () => null,
      },
      { windowId: "w1" },
    );
    const { port1, port2 } = await createChannel();
    expect([port1.id, port2.id]).toEqual(["ch1a", "ch1b"]);
    await transferPort(port2, "peer");
    expect(port2.closed).toBe(true);
    await port1.postMessage({ a: 1 });
    await port1.close();
    expect(seen.map((s) => [s.fn, s.args])).toEqual([
      ["krate/port.channel", []],
      ["krate/port.transfer", ["ch1b", "peer"]],
      ["krate/port.post", ["ch1a", { a: 1 }]],
      ["krate/port.close", ["ch1a"]],
    ]);
  });

  it("onmessage starts delivery and a closed event ends the port", async () => {
    installFakeHost({ "krate/port.listen": () => once({ data: "x" }, { data: { n: 2 } }, { closed: true }) }, { windowId: "w1" });
    const p = portFromId("ch2b");
    const got: unknown[] = [];
    const closed = new Promise<void>((r) => (p.onclose = () => r()));
    p.onmessage = (e) => got.push(e.data);
    await closed;
    expect(got).toEqual(["x", { n: 2 }]);
    expect(p.closed).toBe(true);
  });
});

describe("worker", () => {
  it("startWorker returns a handle that posts and delivers events", async () => {
    const { seen } = installFakeHost(
      {
        "krate/worker.start": () => "w7",
        "krate/worker.post": () => null,
        "krate/worker.stop": () => null,
        "krate/worker.list": () => [{ id: "w7", name: "echo", state: "running", pid: 1, restarts: 0 }],
        "krate/worker.events": () => once({ kind: "message", id: "w7", name: "echo", data: { hi: 1 } }),
      },
      { windowId: "w1" },
    );
    const w = await startWorker("echo", { args: ["--x"] });
    const msg = new Promise((r) => (w.onmessage = r));
    expect(await msg).toEqual({ hi: 1 });
    expect(w.portOwner).toBe("worker:w7");
    await w.postMessage("go");
    await w.terminate();
    expect((await listWorkers())[0].state).toBe("running");
    expect(seen[0]).toMatchObject({ fn: "krate/worker.start", args: ["echo", { args: ["--x"] }] });
    expect(seen.map((s) => s.fn)).toContain("krate/worker.stop");
  });
});

describe("offscreen, automation and extension", () => {
  it("map to their ids", async () => {
    const { seen } = installFakeHost(
      {
        "krate/offscreen.open": () => "w9",
        "krate/offscreen.capture": () => ({ seq: 1, width: 2, height: 2, format: "png", dropped: 0, timeMs: 0, data: new Uint8Array([1]) }),
        "krate/automation.endpoint": () => ({ enabled: true, kind: "cdp", url: "http://127.0.0.1:9222", port: 9222, cdp: true }),
        "krate/automation.cdp": () => ({ product: "Edg" }),
        "krate/extension.declared": () => ["dark"],
        "krate/extension.load": () => ({ id: "e1", name: "dark", enabled: true }),
      },
      { windowId: "w1" },
    );
    expect(await openOffscreen({ path: "/x" })).toBe("w9");
    expect((await captureFrame("w9", { format: "png" })).width).toBe(2);
    expect((await automationEndpoint()).kind).toBe("cdp");
    expect(await cdp("Browser.getVersion")).toEqual({ product: "Edg" });
    expect(await declaredExtensions()).toEqual(["dark"]);
    expect((await loadExtension("dark")).id).toBe("e1");
    expect(seen.find((s) => s.fn === "krate/automation.cdp")?.args).toEqual(["Browser.getVersion", {}, ""]);
  });
});
