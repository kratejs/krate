import { describe, expect, it } from "vitest";
import type { FakeHandler } from "../src/fakehost.js";
import { installFakeHost } from "./sdkhost.js";
import * as lifecycle from "../src/lifecycle.js";
import * as win from "../src/window.js";
import { flushAll } from "./helpers.js";

// A stream the test pushes into.
function feed() {
  const queue: unknown[] = [];
  let wake: (() => void) | undefined;
  return {
    push(v: unknown) {
      queue.push(v);
      wake?.();
    },
    handler: async function* () {
      for (;;) {
        while (queue.length) yield queue.shift();
        await new Promise<void>((r) => (wake = r));
      }
    } as unknown as FakeHandler,
  };
}

describe("openedFiles and onOpenFile", () => {
  it("openedFiles returns the host list", async () => {
    installFakeHost({ "krate/app.openedFiles": () => ["/a.md", "/b.md"] });
    expect(await lifecycle.openedFiles()).toEqual(["/a.md", "/b.md"]);
  });

  it("replays the files already opened, then delivers new ones once each", async () => {
    const f = feed();
    installFakeHost({ "krate/app.openedFiles": () => ["/launch.md"], "krate/app.openFileEvents": f.handler });
    const got: string[] = [];
    const stop = lifecycle.onOpenFile((p) => got.push(p));
    await flushAll();
    expect(got).toEqual(["/launch.md"]);
    // An event that repeats what the replay delivered is dropped.
    f.push({ paths: ["/launch.md"], index: 0, source: "launch" });
    f.push({ paths: ["/two.md", "/three.md"], index: 1, source: "system" });
    await flushAll();
    expect(got).toEqual(["/launch.md", "/two.md", "/three.md"]);
    stop();
    f.push({ paths: ["/four.md"], index: 3, source: "system" });
    await flushAll();
    expect(got).toHaveLength(3);
  });

  it("an event that arrives while the list is being fetched is not lost or doubled", async () => {
    const f = feed();
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    installFakeHost({
      "krate/app.openedFiles": async () => {
        await gate;
        return ["/a.md", "/b.md"];
      },
      "krate/app.openFileEvents": f.handler,
    });
    const got: string[] = [];
    const stop = lifecycle.onOpenFile((p) => got.push(p));
    await flushAll();
    f.push({ paths: ["/b.md"], index: 1, source: "system" }); // already in the list
    f.push({ paths: ["/c.md"], index: 2, source: "system" });
    await flushAll();
    expect(got).toEqual([]);
    release();
    await flushAll();
    expect(got).toEqual(["/a.md", "/b.md", "/c.md"]);
    stop();
  });

  it("replay: false skips the files opened before", async () => {
    const f = feed();
    installFakeHost({ "krate/app.openedFiles": () => ["/old.md"], "krate/app.openFileEvents": f.handler });
    const got: string[] = [];
    const stop = lifecycle.onOpenFile((p) => got.push(p), { replay: false });
    await flushAll();
    f.push({ paths: ["/old.md"], index: 0, source: "launch" });
    f.push({ paths: ["/new.md"], index: 1, source: "second-instance" });
    await flushAll();
    expect(got).toEqual(["/new.md"]);
    stop();
  });

  it("a throwing listener does not end the subscription", async () => {
    const f = feed();
    installFakeHost({ "krate/app.openedFiles": () => [], "krate/app.openFileEvents": f.handler });
    const got: string[] = [];
    const stop = lifecycle.onOpenFile((p) => {
      got.push(p);
      if (p === "/bad.md") throw new Error("boom");
    });
    await flushAll();
    f.push({ paths: ["/bad.md"], index: 0, source: "system" });
    f.push({ paths: ["/good.md"], index: 1, source: "system" });
    await flushAll();
    expect(got).toEqual(["/bad.md", "/good.md"]);
    stop();
  });

  it("reports a missing permission through onError", async () => {
    installFakeHost({});
    const errs: unknown[] = [];
    lifecycle.onOpenFile(() => {}, { onError: (e) => errs.push(e) });
    await flushAll();
    expect(errs.length).toBeGreaterThan(0);
  });
});

describe("onFileDrop", () => {
  it("delivers only the drops onto this page's view", async () => {
    const f = feed();
    installFakeHost({ "krate/window.thisView": () => "v2", "krate/window.fileDrops": f.handler });
    const got: string[][] = [];
    const stop = win.onFileDrop((e) => got.push(e.paths));
    await flushAll();
    f.push({ view: "main", window: "main", paths: ["/other.png"], x: 1, y: 2 });
    f.push({ view: "v2", window: "main", paths: ["/mine.png", "/dir"], x: 10, y: 20 });
    await flushAll();
    expect(got).toEqual([["/mine.png", "/dir"]]);
    stop();
  });

  it("all: true delivers every view's drops without asking for the view", async () => {
    const f = feed();
    const { seen } = installFakeHost({ "krate/window.fileDrops": f.handler });
    const got: string[] = [];
    const stop = win.onFileDrop((e) => got.push(e.view), { all: true });
    await flushAll();
    f.push({ view: "main", window: "main", paths: ["/a"], x: 0, y: 0 });
    f.push({ view: "v2", window: "main", paths: ["/b"], x: 0, y: 0 });
    await flushAll();
    expect(got).toEqual(["main", "v2"]);
    expect(seen.map((c) => c.id)).toEqual(["krate/window.fileDrops"]);
    stop();
  });

  it("stopping before the view id arrives never subscribes", async () => {
    const f = feed();
    const { seen } = installFakeHost({ "krate/window.thisView": () => "main", "krate/window.fileDrops": f.handler });
    const stop = win.onFileDrop(() => {});
    stop();
    await flushAll();
    expect(seen.map((c) => c.id)).toEqual(["krate/window.thisView"]);
  });
});
