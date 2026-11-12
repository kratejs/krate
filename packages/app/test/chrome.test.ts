import { describe, expect, it } from "vitest";
import { KrateError } from "../src/errors.js";
import { installFakeHost, type SeenCall } from "./sdkhost.js";
import * as chrome from "../src/chrome.js";

function setup(answers: Record<string, (args: any[]) => unknown>): SeenCall[] {
  const handlers: Record<string, (args: any[]) => unknown> = {};
  for (const [fn, f] of Object.entries(answers)) handlers[fn] = (args) => f(args);
  return installFakeHost(handlers as any, { windowId: "w1" }).seen;
}

describe("chrome", () => {
  it("window operations send the window id first", async () => {
    const seen = setup({
      "krate/chrome.setTitleBar": () => null,
      "krate/chrome.setBackdrop": () => null,
      "krate/chrome.setVibrancy": () => null,
      "krate/chrome.setTrafficLightPosition": () => null,
      "krate/chrome.setDocumentEdited": () => null,
      "krate/chrome.setRepresentedFile": () => null,
      "krate/chrome.setTabbing": () => null,
      "krate/chrome.attachSheet": () => null,
    });
    const w = chrome.getChrome("w2");
    await w.setTitleBar("hidden");
    await w.setBackdrop("mica");
    await w.setVibrancy("sidebar");
    await w.setTrafficLightPosition(14, 18);
    await w.setDocumentEdited(true);
    await w.setRepresentedFile("/tmp/a.txt");
    await w.setTabbing("docs");
    await w.attachSheet(chrome.getChrome("w3"));
    expect(seen.map((s) => [s.fn, s.args])).toEqual([
      ["krate/chrome.setTitleBar", ["w2", "hidden"]],
      ["krate/chrome.setBackdrop", ["w2", "mica"]],
      ["krate/chrome.setVibrancy", ["w2", "sidebar"]],
      ["krate/chrome.setTrafficLightPosition", ["w2", 14, 18]],
      ["krate/chrome.setDocumentEdited", ["w2", true]],
      ["krate/chrome.setRepresentedFile", ["w2", "/tmp/a.txt"]],
      ["krate/chrome.setTabbing", ["w2", "docs", "preferred"]],
      ["krate/chrome.attachSheet", ["w2", "w3"]],
    ]);
  });

  it("the overlay is sent as an object or null", async () => {
    const seen = setup({ "krate/chrome.setTitleBarOverlay": () => null });
    await chrome.mainChrome().setTitleBarOverlay({ color: "#111", height: 36 });
    await chrome.mainChrome().setTitleBarOverlay(null);
    expect(seen[0].args).toEqual(["main", { color: "#111", height: 36 }]);
    expect(seen[1].args).toEqual(["main", null]);
  });

  it("thumb buttons get an empty icon when none is given", async () => {
    const seen = setup({ "krate/chrome.setThumbButtons": () => null });
    await chrome.mainChrome().setThumbButtons([{ id: "play", tooltip: "Play", icon: new Uint8Array([1, 2]) }, { id: "next" }]);
    const list = seen[0].args[1] as any[];
    expect(list[0].icon).toEqual(new Uint8Array([1, 2]));
    expect(list[1].icon).toEqual(new Uint8Array(0));
    expect(list[1].id).toBe("next");
  });

  it("share, jump list and dock fill in defaults", async () => {
    const seen = setup({
      "krate/chrome.share": () => null,
      "krate/chrome.setJumpList": () => null,
      "krate/chrome.clearJumpList": () => null,
      "krate/chrome.setActivationPolicy": () => null,
      "krate/chrome.setDockVisible": () => null,
    });
    await chrome.share("main", { text: "hi" });
    await chrome.setJumpList([{ title: "New window", args: ["--new"] }]);
    await chrome.clearJumpList();
    await chrome.setActivationPolicy("accessory");
    await chrome.setDockVisible(false);
    expect(seen[0].args).toEqual(["main", { text: "hi", urls: [], files: [] }]);
    expect(seen[1].args).toEqual([[{ title: "New window", args: ["--new"], description: "", iconPath: "", iconIndex: 0 }]]);
    expect(seen[3].args).toEqual(["accessory"]);
    expect(seen[4].args).toEqual([false]);
  });

  it("an unsupported platform rejects with the host's code", async () => {
    setup({
      "krate/chrome.setBackdrop": () => {
        throw new KrateError("unsupported", "window backdrops are a Windows 11 feature");
      },
    });
    await expect(chrome.mainChrome().setBackdrop("mica")).rejects.toMatchObject({ code: "unsupported" });
  });

  it("state is returned as the host sent it", async () => {
    setup({ "krate/chrome.state": () => ({ titleBar: "none", transparent: true, features: ["drag"], tabs: 1 }) });
    const st = await chrome.mainChrome().state();
    expect(st.titleBar).toBe("none");
    expect(st.features).toEqual(["drag"]);
  });
});
