import { beforeEach, describe, expect, it } from "vitest";
import { KrateError } from "../src/errors.js";
import type { FakeHandler, FakeHost } from "../src/fakehost.js";
import { installFakeHost, type SeenCall } from "./sdkhost.js";
import * as dialog from "../src/dialog.js";
import * as clipboard from "../src/clipboard.js";
import * as menu from "../src/menu.js";
import * as tray from "../src/tray.js";
import * as notify from "../src/notify.js";
import * as shortcut from "../src/shortcut.js";
import * as win from "../src/window.js";
import * as lifecycle from "../src/lifecycle.js";
import { flushAll } from "./helpers.js";

let host: FakeHost;
let seen: SeenCall[];

// A host whose functions record their arguments and answer from `answers`.
function setup(answers: Record<string, (args: any[]) => unknown> = {}, extra: Record<string, FakeHandler> = {}) {
  const handlers: Record<string, FakeHandler> = { ...extra };
  for (const [fn, f] of Object.entries(answers)) handlers[fn] = (args) => f(args);
  ({ host, seen } = installFakeHost(handlers, { windowId: "w1" }));
}

beforeEach(() => setup());

describe("dialog", () => {
  it("openFile sends only the given options and returns the paths", async () => {
    setup({ "krate/dialog.openFile": () => ["/a.png", "/b.png"] });
    const r = await dialog.openFile({ title: "Pick", multiple: true, filters: [{ name: "Images", patterns: ["*.png"] }] });
    expect(r).toEqual(["/a.png", "/b.png"]);
    expect(seen[0].args).toEqual([{ title: "Pick", multiple: true, filters: [{ name: "Images", patterns: ["*.png"] }] }]);
  });

  it("saveFile resolves to null on cancel; message maps the kind to the wire number", async () => {
    setup({ "krate/dialog.saveFile": () => null, "krate/dialog.message": () => 1 });
    expect(await dialog.saveFile({ name: "x.txt", overwrite: true })).toBeNull();
    expect(seen[0].args).toEqual([{ name: "x.txt", overwrite: true }]);
    expect(await dialog.message({ kind: "error", message: "boom", buttons: ["A", "B"] })).toBe(1);
    expect(seen[1].args).toEqual([{ message: "boom", buttons: ["A", "B"], kind: 2 }]);
    expect(await dialog.confirm("Sure?", "Delete?")).toBe(true);
    expect((seen[2].args[0] as any).kind).toBe(3);
  });

  it("a missing permission is a not_found KrateError", async () => {
    setup();
    await expect(dialog.openFile()).rejects.toMatchObject({ code: "not_found" });
  });

  it("unsupported platforms reject with the code from the host", async () => {
    setup({
      "krate/dialog.openFile": () => {
        throw new KrateError("unsupported", "dialogs are not available on this platform");
      },
    });
    await expect(dialog.openFile()).rejects.toMatchObject({ code: "unsupported" });
  });
});

describe("clipboard", () => {
  it("round trips text, binary images and file lists", async () => {
    let text = "";
    let img: Uint8Array | null = null;
    setup({
      "krate/clipboard.writeText": ([t]) => void (text = t),
      "krate/clipboard.readText": () => text,
      "krate/clipboard.writeImage": ([b]) => void (img = b),
      "krate/clipboard.readImage": () => img,
      "krate/clipboard.writeFiles": () => null,
      "krate/clipboard.readFiles": () => ["/a b", "/c"],
      "krate/clipboard.clear": () => null,
    });
    await clipboard.writeText("héllo");
    expect(await clipboard.readText()).toBe("héllo");
    expect(await clipboard.readImage()).toBeNull();
    await clipboard.writeImage(new Uint8Array([137, 80, 78, 71, 0, 255]));
    expect(Array.from((await clipboard.readImage())!)).toEqual([137, 80, 78, 71, 0, 255]);
    await clipboard.writeFiles(["/x"]);
    expect(await clipboard.readFiles()).toEqual(["/a b", "/c"]);
    await clipboard.clear();
  });
});

describe("clipboard rich formats", () => {
  it("writes several representations and reads them back by name", async () => {
    setup({
      "krate/clipboard.write": () => null,
      "krate/clipboard.readHtml": () => "<b>x</b>",
      "krate/clipboard.readRtf": () => "{\\rtf1}",
      "krate/clipboard.formats": () => ["text/plain", "text/html"],
      "krate/clipboard.readFormat": ([n]) => (n === "text/html" ? new Uint8Array([60, 98, 62]) : null),
      "krate/clipboard.writeFormat": () => null,
    });
    await clipboard.write({ text: "x", html: "<b>x</b>" });
    expect(seen[0].args).toEqual([{ text: "x", html: "<b>x</b>" }]);
    expect(await clipboard.readHtml()).toBe("<b>x</b>");
    expect(await clipboard.readRtf()).toBe("{\\rtf1}");
    expect(await clipboard.formats()).toEqual(["text/plain", "text/html"]);
    expect(Array.from((await clipboard.readFormat("text/html"))!)).toEqual([60, 98, 62]);
    expect(await clipboard.readFormat("image/png")).toBeNull();
    await clipboard.writeFormat("application/x-acme", new Uint8Array([1, 2]));
    expect(seen[seen.length - 1].args[0]).toBe("application/x-acme");
  });
});

describe("window", () => {
  it("hide sends the release flag only when asked, and show restores", async () => {
    setup({ "krate/window.hide": () => null, "krate/window.show": () => null });
    const w = win.get("w2");
    await w.hide();
    await w.hide({ release: false });
    await w.hide({ release: true });
    await w.show();
    expect(seen.map((c) => c.args)).toEqual([["w2"], ["w2"], ["w2", true], ["w2"]]);
  });

  it("onMemoryPressure listens for the shell's DOM event and unsubscribes", () => {
    const listeners = new Map<string, Set<() => void>>();
    const g = globalThis as unknown as Record<string, unknown>;
    const saved = { add: g.addEventListener, rem: g.removeEventListener };
    g.addEventListener = (t: string, f: () => void) => {
      (listeners.get(t) ?? listeners.set(t, new Set()).get(t)!).add(f);
    };
    g.removeEventListener = (t: string, f: () => void) => listeners.get(t)?.delete(f);
    try {
      let n = 0;
      const stop = win.onMemoryPressure(() => n++);
      for (const f of listeners.get("krate:memory-pressure") ?? []) f();
      expect(n).toBe(1);
      stop();
      expect(listeners.get("krate:memory-pressure")?.size).toBe(0);
    } finally {
      g.addEventListener = saved.add;
      g.removeEventListener = saved.rem;
    }
  });

  it("onMemoryPressure is a no-op outside a page", () => {
    const g = globalThis as unknown as Record<string, unknown>;
    const saved = g.addEventListener;
    g.addEventListener = undefined;
    try {
      expect(() => win.onMemoryPressure(() => {})()).not.toThrow();
    } finally {
      g.addEventListener = saved;
    }
  });

  it("open maps options, and handles drive the id-first commands", async () => {
    setup({
      "krate/window.open": () => "w2",
      "krate/window.setSize": () => null,
      "krate/window.setAlwaysOnTop": () => null,
      "krate/window.state": () => ({ width: 500, height: 400, fullscreen: false, title: "S" }),
      "krate/window.list": () => ["main", "w2"],
    });
    const w = await win.open({ title: "S", path: "/settings", frameless: true, position: { x: 5, y: 6 } });
    expect(w.id).toBe("w2");
    expect(seen[0].args).toEqual([{ title: "S", path: "/settings", frameless: true, x: 5, y: 6, hasPosition: true }]);
    await w.setSize(500, 400);
    await w.setAlwaysOnTop(true);
    expect(seen[1].args).toEqual(["w2", 500, 400]);
    expect(seen[2].args).toEqual(["w2", true]);
    expect((await w.state()).width).toBe(500);
    expect(await win.list()).toEqual(["main", "w2"]);
    expect(win.main().id).toBe("main");
  });

  it("views: addView maps options, handles drive the id-first commands, layouts are normalized", async () => {
    setup({
      "krate/window.addView": () => "v1",
      "krate/window.removeView": () => null,
      "krate/window.focusView": () => null,
      "krate/window.navigateView": () => null,
      "krate/window.postMessage": () => null,
      "krate/window.setLayout": () => null,
      "krate/window.layout": () => ({ view: "main" }),
      "krate/window.views": () => [{ id: "main", window: "main", path: "/", primary: true }],
    });
    const w = win.main();
    const side = await w.addView({ path: "/side", side: "left", size: 0.25, minWidth: 180, beside: win.getView("main"), permissions: ["kv"] });
    expect(side.id).toBe("v1");
    expect(seen[0].args).toEqual(["main", { path: "/side", side: "left", size: 0.25, minWidth: 180, beside: "main", permissions: ["kv"] }]);
    await w.addView();
    expect(seen[1].args).toEqual(["main", {}]);

    await side.focus();
    await side.navigate("/other");
    await side.post({ n: 1, nested: { ok: true } });
    await side.remove();
    expect(seen.slice(2).map((c) => [c.id, c.args])).toEqual([
      ["krate/window.focusView", ["v1"]],
      ["krate/window.navigateView", ["v1", "/other"]],
      ["krate/window.postMessage", ["v1", { n: 1, nested: { ok: true } }]],
      ["krate/window.removeView", ["v1"]],
    ]);
    await expect(side.post(undefined)).rejects.toThrow(/JSON/);

    await w.setLayout(win.split("horizontal", win.pane(side, { minWidth: 100 }), win.split("vertical", win.pane("main"), win.pane("v2"), 0.7)));
    expect(seen[seen.length - 1].args).toEqual([
      "main",
      {
        direction: "horizontal",
        first: { view: "v1", minWidth: 100 },
        second: { direction: "vertical", ratio: 0.7, first: { view: "main" }, second: { view: "v2" } },
      },
    ]);
    expect(await w.layout()).toEqual({ view: "main" });
    expect(await w.views()).toHaveLength(1);
    expect(await win.views()).toHaveLength(1);
    expect(seen[seen.length - 1].args).toEqual([""]);
  });

  it("onViewMessage listens for the DOM event and stops when asked", () => {
    const listeners = new Map<string, (e: unknown) => void>();
    const g = globalThis as unknown as { addEventListener?: unknown; removeEventListener?: unknown };
    const saved = { add: g.addEventListener, remove: g.removeEventListener };
    g.addEventListener = (type: string, fn: (e: unknown) => void) => listeners.set(type, fn);
    g.removeEventListener = (type: string) => listeners.delete(type);
    try {
      const got: unknown[] = [];
      const stop = win.onViewMessage((m) => got.push(m));
      listeners.get("krate:view-message")!({ detail: { from: "v2", data: { a: 1 } } });
      expect(got).toEqual([{ from: "v2", data: { a: 1 } }]);
      stop();
      expect(listeners.has("krate:view-message")).toBe(false);
    } finally {
      g.addEventListener = saved.add;
      g.removeEventListener = saved.remove;
    }
  });

  it("onViewEvent delivers view events", async () => {
    setup({}, {
      "krate/window.viewEvents": async function* () {
        yield { view: "v1", window: "main", kind: "added", path: "/side" };
        yield { view: "v1", window: "main", kind: "removed", path: "" };
      } as unknown as FakeHandler,
    });
    const got: string[] = [];
    const stop = win.onViewEvent((e) => got.push(`${e.view}:${e.kind}`));
    await flushAll();
    expect(got).toEqual(["v1:added", "v1:removed"]);
    stop();
  });

  it("onCloseRequested filters by window and the unsubscribe stops delivery", async () => {
    let push!: (v: unknown) => void;
    const queue: unknown[] = [];
    let wake: (() => void) | undefined;
    push = (v) => {
      queue.push(v);
      wake?.();
    };
    setup({}, {
      "krate/window.events": async function* () {
        for (;;) {
          while (queue.length) yield queue.shift();
          await new Promise<void>((r) => (wake = r));
        }
      } as unknown as FakeHandler,
    });
    const got: string[] = [];
    const stop = win.get("w2").onCloseRequested(() => got.push("w2"));
    await flushAll();
    push({ window: "w1", kind: "close-requested" });
    push({ window: "w2", kind: "focus" });
    push({ window: "w2", kind: "close-requested" });
    await flushAll();
    expect(got).toEqual(["w2"]);
    stop();
    await flushAll();
    expect(host.cancelledStreams.length + host.closedStreams.length).toBeGreaterThan(0);
  });
});

describe("menu", () => {
  it("setApp encodes kinds, submenus and defaults", async () => {
    setup({ "krate/menu.setApp": () => null, "krate/menu.popup": () => null });
    await menu.setApp([
      { label: "&File", submenu: [{ id: "new", label: "New", accelerator: "CmdOrCtrl+N" }, { kind: "separator" }, { id: "auto", label: "Autosave", kind: "check", checked: true }] },
    ]);
    expect(seen[0].args).toEqual([
      [
        {
          id: "", label: "&File", kind: 4, accelerator: "", disabled: false, checked: false,
          submenu: [
            { id: "new", label: "New", kind: 0, accelerator: "CmdOrCtrl+N", disabled: false, checked: false, submenu: [] },
            { id: "", label: "", kind: 3, accelerator: "", disabled: false, checked: false, submenu: [] },
            { id: "auto", label: "Autosave", kind: 1, accelerator: "", disabled: false, checked: true, submenu: [] },
          ],
        },
      ],
    ]);
    await menu.popup([{ id: "c", label: "Copy" }], { window: "w2" });
    expect(seen[1].args.slice(0, 3)).toEqual(["w2", -1, -1]);
  });

  it("onMenu delivers matching clicks from the event stream", async () => {
    const clicks = [
      { id: "other", window: "main", checked: false },
      { id: "new", window: "main", checked: false },
      { id: "auto", window: "w2", checked: true },
    ];
    setup({}, {
      "krate/menu.events": async function* () {
        for (const c of clicks) yield c;
        await new Promise(() => {});
      } as unknown as FakeHandler,
    });
    const got: unknown[] = [];
    const stop = menu.onMenu("auto", (e) => got.push(e));
    await flushAll();
    expect(got).toEqual([{ id: "auto", window: "w2", checked: true }]);
    stop();
  });

  it("a stream that is not permitted stops the subscription and reports once", async () => {
    setup();
    const errors: unknown[] = [];
    const stop = menu.onClick(() => {}, { onError: (e) => errors.push(e) });
    await flushAll();
    expect(errors).toHaveLength(1);
    expect((errors[0] as KrateError).code).toBe("not_found");
    stop();
  });
});

describe("context menu", () => {
  const ev = (over: Partial<menu.ContextMenuEvent> = {}): menu.ContextMenuEvent => ({
    window: "w1", view: "w1", x: 1, y: 2, windowX: 3, windowY: 4, kinds: ["page"], linkUrl: "", imageUrl: "", mediaUrl: "",
    selectionText: "", misspelled: "", suggestions: [], inspectable: false, ...over,
  });

  it("setContextMenu sends the target, mode and items", async () => {
    setup({ "krate/menu.setContextMenu": () => null });
    await menu.setContextMenu({ mode: "none" });
    await menu.setContextMenu({ mode: "extend", items: [{ id: "a", label: "A" }] }, { window: "*" });
    await menu.setContextMenu({ mode: "custom" }, { view: "v2" });
    expect(seen[0].args).toEqual(["", "", { mode: "none", items: [] }]);
    expect((seen[1].args as any)[0]).toBe("*");
    expect((seen[1].args as any)[2].items[0].id).toBe("a");
    expect(seen[2].args.slice(0, 2)).toEqual(["", "v2"]);
  });

  it("onContextMenu pops up the answer at the window coordinates", async () => {
    setup(
      { "krate/menu.popup": () => null },
      {
        "krate/menu.contextEvents": async function* () {
          yield ev({ windowX: 30, windowY: 40, kinds: ["page", "link"], linkUrl: "https://e.x/" });
          yield ev({ windowX: 1, windowY: 1 });
          await new Promise(() => {});
        } as unknown as FakeHandler,
      },
    );
    const stop = menu.onContextMenu((e) => (e.kinds.includes("link") ? [{ id: "open", label: "Open" }] : null));
    await flushAll();
    stop();
    const pops = seen.filter((c) => c.id === "krate/menu.popup");
    expect(pops).toHaveLength(1);
    expect(pops[0].args.slice(0, 3)).toEqual(["w1", 30, 40]);
  });

  it("defaultContextItems follows what was clicked", () => {
    const ids = (e: menu.ContextMenuEvent) => menu.defaultContextItems(e).map((i) => i.id ?? i.kind ?? i.label);
    expect(ids(ev())).toEqual([]);
    expect(ids(ev({ kinds: ["page", "selection"] }))).toEqual(["krate:copy", "krate:selectAll"]);
    expect(ids(ev({ kinds: ["page", "link"], linkUrl: "https://e.x/", inspectable: true }))).toEqual([
      "krate:copyLink:https://e.x/",
      "separator",
      "krate:inspectElement",
    ]);
    const spell = ids(ev({ kinds: ["page", "editable"], misspelled: "teh", suggestions: ["the"] }));
    expect(spell.slice(0, 3)).toEqual(["krate:replaceMisspelling:the", "krate:addToDictionary", "separator"]);
    expect(spell).toContain("krate:cut");
    expect(spell).toContain("krate:paste");
    expect(ids(ev({ kinds: ["page", "editable"] }))[0]).toBe("krate:undo");
    expect(ids(ev({ kinds: ["page", "editable"], misspelled: "xx" }))[0]).toBe("No Guesses Found");
  });
});

describe("tray and notify", () => {
  it("tray.set sends icon bytes and menu items", async () => {
    setup({ "krate/tray.set": () => null, "krate/tray.remove": () => null });
    await tray.set({ tooltip: "T", icon: new Uint8Array([1, 2, 3]), menu: [{ id: "quit", label: "Quit" }] });
    const a = seen[0].args[0] as any;
    expect(a.tooltip).toBe("T");
    expect(Array.from(a.icon)).toEqual([1, 2, 3]);
    expect(a.menu[0].id).toBe("quit");
    await tray.remove();
  });

  it("notify.send maps urgency and actions", async () => {
    setup({ "krate/notify.send": () => null, "krate/notify.close": () => null });
    await notify.send({ id: "n", title: "Done", actions: [{ id: "open", label: "Open" }], urgency: "critical", timeoutMs: 3000 });
    expect(seen[0].args).toEqual([
      { id: "n", title: "Done", body: "", icon: "", actions: [{ id: "open", label: "Open" }], urgency: 2, timeoutMs: 3000, silent: false },
    ]);
    await notify.close("n");
  });

  it("notify.onAction fires for the matching id and action only", async () => {
    setup({}, {
      "krate/notify.events": async function* () {
        yield { id: "n", action: "other", closed: false };
        yield { id: "m", action: "open", closed: false };
        yield { id: "n", action: "open", closed: true };
        yield { id: "n", action: "open", closed: false };
        await new Promise(() => {});
      } as unknown as FakeHandler,
    });
    let n = 0;
    const stop = notify.onAction("n", "open", () => n++);
    await flushAll();
    expect(n).toBe(1);
    stop();
  });
});

describe("shortcut and lifecycle", () => {
  it("register routes events by handle and unregister releases it", async () => {
    let handle = "";
    let emit!: (v: unknown) => void;
    const pending: unknown[] = [];
    let wake: (() => void) | undefined;
    emit = (v) => {
      pending.push(v);
      wake?.();
    };
    setup({
      "krate/shortcut.register": ([h, accel]) => {
        handle = h;
        expect(accel).toBe("Ctrl+Shift+K");
        return null;
      },
      "krate/shortcut.unregister": ([h]) => {
        expect(h).toBe(handle);
        return null;
      },
    }, {
      "krate/shortcut.events": async function* () {
        for (;;) {
          while (pending.length) yield pending.shift();
          await new Promise<void>((r) => (wake = r));
        }
      } as unknown as FakeHandler,
    });
    let presses = 0;
    const unregister = await shortcut.register("Ctrl+Shift+K", () => presses++);
    await flushAll();
    emit({ id: "someone-else" });
    emit({ id: handle });
    await flushAll();
    expect(presses).toBe(1);
    await unregister();
    emit({ id: handle });
    await flushAll();
    expect(presses).toBe(1);
  });

  it("a failed register leaves nothing behind", async () => {
    setup({
      "krate/shortcut.register": () => {
        throw new KrateError("unsupported", "no portal");
      },
    }, {
      "krate/shortcut.events": async function* () {
        await new Promise(() => {});
      } as unknown as FakeHandler,
    });
    await expect(shortcut.register("Ctrl+Alt+J", () => {})).rejects.toMatchObject({ code: "unsupported" });
  });

  it("supports, quit and second-instance URLs", async () => {
    setup({ "krate/app.supports": ([c]) => c === "tray", "krate/app.quit": () => null, "krate/app.relaunch": () => null }, {
      "krate/app.events": async function* () {
        yield { args: ["--x", "acme://open/1"], dir: "/w", urls: ["acme://open/1"] };
        await new Promise(() => {});
      } as unknown as FakeHandler,
    });
    expect(await lifecycle.supports("tray")).toBe(true);
    expect(await lifecycle.supports("dialog")).toBe(false);
    await lifecycle.quit();
    await lifecycle.relaunch();
    const urls: string[] = [];
    const stop = lifecycle.onOpenURL((u) => urls.push(u));
    await flushAll();
    expect(urls).toEqual(["acme://open/1"]);
    stop();
  });
});
