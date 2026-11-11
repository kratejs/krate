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
    setup({ "krate/dialog.OpenFile": () => ["/a.png", "/b.png"] });
    const r = await dialog.openFile({ title: "Pick", multiple: true, filters: [{ name: "Images", patterns: ["*.png"] }] });
    expect(r).toEqual(["/a.png", "/b.png"]);
    expect(seen[0].args).toEqual([{ title: "Pick", multiple: true, filters: [{ name: "Images", patterns: ["*.png"] }] }]);
  });

  it("saveFile resolves to null on cancel; message maps the kind to the wire number", async () => {
    setup({ "krate/dialog.SaveFile": () => null, "krate/dialog.Message": () => 1 });
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
      "krate/dialog.OpenFile": () => {
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
      "krate/clipboard.WriteText": ([t]) => void (text = t),
      "krate/clipboard.ReadText": () => text,
      "krate/clipboard.WriteImage": ([b]) => void (img = b),
      "krate/clipboard.ReadImage": () => img,
      "krate/clipboard.WriteFiles": () => null,
      "krate/clipboard.ReadFiles": () => ["/a b", "/c"],
      "krate/clipboard.Clear": () => null,
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

describe("window", () => {
  it("open maps options, and handles drive the id-first commands", async () => {
    setup({
      "krate/window.Open": () => "w2",
      "krate/window.SetSize": () => null,
      "krate/window.SetAlwaysOnTop": () => null,
      "krate/window.State": () => ({ width: 500, height: 400, fullscreen: false, title: "S" }),
      "krate/window.List": () => ["main", "w2"],
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
    setup({ "krate/menu.SetApp": () => null, "krate/menu.Popup": () => null });
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

describe("tray and notify", () => {
  it("tray.set sends icon bytes and menu items", async () => {
    setup({ "krate/tray.Set": () => null, "krate/tray.Remove": () => null });
    await tray.set({ tooltip: "T", icon: new Uint8Array([1, 2, 3]), menu: [{ id: "quit", label: "Quit" }] });
    const a = seen[0].args[0] as any;
    expect(a.tooltip).toBe("T");
    expect(Array.from(a.icon)).toEqual([1, 2, 3]);
    expect(a.menu[0].id).toBe("quit");
    await tray.remove();
  });

  it("notify.send maps urgency and actions", async () => {
    setup({ "krate/notify.Send": () => null, "krate/notify.Close": () => null });
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
      "krate/shortcut.Register": ([h, accel]) => {
        handle = h;
        expect(accel).toBe("Ctrl+Shift+K");
        return null;
      },
      "krate/shortcut.Unregister": ([h]) => {
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
      "krate/shortcut.Register": () => {
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
    setup({ "krate/app.Supports": ([c]) => c === "tray", "krate/app.Quit": () => null, "krate/app.Relaunch": () => null }, {
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
