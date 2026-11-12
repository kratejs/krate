import { beforeEach, describe, expect, it } from "vitest";
import type { FakeHandler, FakeHost } from "../src/fakehost.js";
import { installFakeHost, type SeenCall } from "./sdkhost.js";
import * as badge from "../src/badge.js";
import * as screen from "../src/screen.js";
import * as power from "../src/power.js";
import * as login from "../src/login.js";
import * as theme from "../src/theme.js";
import * as crash from "../src/crash.js";
import { flushAll } from "./helpers.js";

let host: FakeHost;
let seen: SeenCall[];

function setup(answers: Record<string, (args: any[]) => unknown> = {}, extra: Record<string, FakeHandler> = {}) {
  const handlers: Record<string, FakeHandler> = { ...extra };
  for (const [fn, f] of Object.entries(answers)) handlers[fn] = (args) => f(args);
  ({ host, seen } = installFakeHost(handlers, { windowId: "w1" }));
}

beforeEach(() => setup());

describe("badge", () => {
  it("set turns counts into text and 0 or null into a removal", async () => {
    setup({ "krate/badge.set": () => null });
    await badge.set(3);
    await badge.set("new");
    await badge.set(0);
    await badge.set(null);
    await badge.clear();
    expect(seen.map((s) => s.args)).toEqual([["3"], ["new"], [""], [""], [""]]);
  });

  it("setProgress sends the state and value; clearProgress sends none", async () => {
    setup({ "krate/badge.setProgress": () => null, "krate/badge.setUrgent": () => null });
    await badge.setProgress("normal", 0.5);
    await badge.setProgress("indeterminate");
    await badge.clearProgress();
    await badge.setUrgent(true);
    expect(seen.map((s) => s.args)).toEqual([["normal", 0.5], ["indeterminate", 0], ["none", 0], [true]]);
  });

  it("get and progress return what the host says", async () => {
    setup({ "krate/badge.get": () => "7", "krate/badge.progress": () => ({ state: "paused", value: 0.2 }) });
    expect(await badge.get()).toBe("7");
    expect(await badge.progress()).toEqual({ state: "paused", value: 0.2 });
  });
});

describe("screen", () => {
  const d = { id: "m0", name: "A", x: 0, y: 0, width: 10, height: 10, workX: 0, workY: 0, workWidth: 10, workHeight: 10, scale: 1, primary: true, refreshHz: 60 };

  it("list, primary and ofWindow return displays; centerWindow sends both ids", async () => {
    setup({
      "krate/screen.list": () => [d],
      "krate/screen.primary": () => d,
      "krate/screen.ofWindow": () => d,
      "krate/screen.centerWindow": () => null,
    });
    expect(await screen.list()).toEqual([d]);
    expect((await screen.primary()).primary).toBe(true);
    expect((await screen.ofWindow()).id).toBe("m0");
    await screen.centerWindow("w2", "m1");
    await screen.centerWindow();
    expect(seen.map((s) => s.fn + JSON.stringify(s.args))).toEqual([
      'krate/screen.list[]',
      'krate/screen.primary[]',
      'krate/screen.ofWindow[""]',
      'krate/screen.centerWindow["w2","m1"]',
      'krate/screen.centerWindow["",""]',
    ]);
  });

  it("onChange delivers the new display list", async () => {
    setup({}, {
      "krate/screen.events": async function* () {
        yield { displays: [d] };
        await new Promise(() => {});
      } as unknown as FakeHandler,
    });
    const got: unknown[] = [];
    const stop = screen.onChange((e) => got.push(e));
    await flushAll();
    expect(got).toEqual([{ displays: [d] }]);
    stop();
  });
});

describe("power", () => {
  it("preventSleep sends the reason and display flag; allowSleep the id", async () => {
    setup({ "krate/power.preventSleep": () => "b1", "krate/power.allowSleep": () => null, "krate/power.setIdleThreshold": () => null });
    expect(await power.preventSleep("export", true)).toBe("b1");
    expect(await power.preventSleep("batch")).toBe("b1");
    await power.allowSleep("b1");
    await power.setIdleThreshold(120);
    expect(seen.map((s) => s.args)).toEqual([["export", true], ["batch", false], ["b1"], [120]]);
  });

  it("on filters events by kind", async () => {
    setup({}, {
      "krate/power.events": async function* () {
        yield { kind: "lock" };
        yield { kind: "resume" };
        yield { kind: "resume" };
        await new Promise(() => {});
      } as unknown as FakeHandler,
    });
    let n = 0;
    const stop = power.on("resume", () => n++);
    await flushAll();
    expect(n).toBe(2);
    stop();
  });

  it("state and idleTime pass the host's answer through", async () => {
    setup({
      "krate/power.state": () => ({ onBattery: true, idleSeconds: 4, locked: false, idleState: "active" }),
      "krate/power.idleTime": () => 4,
    });
    expect((await power.state()).onBattery).toBe(true);
    expect(await power.idleTime()).toBe(4);
  });
});

describe("login", () => {
  it("set sends the flag and the arguments", async () => {
    setup({ "krate/login.set": () => null, "krate/login.get": () => ({ enabled: true, method: "autostart" }) });
    await login.set(true, { args: ["--background"] });
    await login.set(false);
    expect(seen.map((s) => s.args)).toEqual([[true, { args: ["--background"] }], [false, { args: [] }]]);
    expect(await login.get()).toEqual({ enabled: true, method: "autostart" });
  });
});

describe("theme", () => {
  it("get, set and onChange", async () => {
    const t = { scheme: "dark", highContrast: false, system: "dark", source: "system" };
    setup({ "krate/theme.get": () => t, "krate/theme.set": () => null }, {
      "krate/theme.events": async function* () {
        yield { ...t, scheme: "light", system: "light" };
        await new Promise(() => {});
      } as unknown as FakeHandler,
    });
    expect(await theme.get()).toEqual(t);
    await theme.set("light");
    expect(seen[1].args).toEqual(["light"]);
    const got: string[] = [];
    const stop = theme.onChange((x) => got.push(x.scheme));
    await flushAll();
    expect(got).toEqual(["light"]);
    stop();
  });
});

describe("crash", () => {
  it("list, read, remove and setAutoReload map to their ids", async () => {
    setup({
      "krate/crash.list": () => [{ id: "crash-1", kind: "webview", time: "t", reason: "crashed" }],
      "krate/crash.read": (a) => ({ id: a[0], kind: "webview", time: "t", reason: "crashed", stack: "" }),
      "krate/crash.delete": () => null,
      "krate/crash.setAutoReload": () => null,
    });
    expect((await crash.list())[0].id).toBe("crash-1");
    expect((await crash.read("crash-1")).reason).toBe("crashed");
    await crash.remove("crash-1");
    await crash.setAutoReload(true);
    expect(seen.map((s) => s.fn)).toEqual(["krate/crash.list", "krate/crash.read", "krate/crash.delete", "krate/crash.setAutoReload"]);
    expect(seen[2].args).toEqual(["crash-1"]);
    expect(seen[3].args).toEqual([true]);
  });

  it("onCrash delivers events", async () => {
    setup({}, {
      "krate/crash.events": async function* () {
        yield { kind: "webview", reason: "killed", window: "main", view: "main", reportId: "r", reloaded: true };
        await new Promise(() => {});
      } as unknown as FakeHandler,
    });
    const got: crash.CrashEvent[] = [];
    const stop = crash.onCrash((e) => got.push(e));
    await flushAll();
    expect(got[0].reloaded).toBe(true);
    stop();
  });
});
