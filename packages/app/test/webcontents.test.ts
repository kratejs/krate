import { beforeEach, describe, expect, it } from "vitest";
import { KrateError } from "../src/errors.js";
import type { FakeHandler, FakeHost } from "../src/fakehost.js";
import { installFakeHost, type SeenCall } from "./sdkhost.js";
import * as page from "../src/page.js";
import * as downloads from "../src/downloads.js";
import * as cookies from "../src/cookies.js";
import { flushAll } from "./helpers.js";

let host: FakeHost;
let seen: SeenCall[];

function setup(answers: Record<string, (args: any[]) => unknown> = {}, extra: Record<string, FakeHandler> = {}) {
  const handlers: Record<string, FakeHandler> = { ...extra };
  for (const [fn, f] of Object.entries(answers)) handlers[fn] = (args) => f(args);
  ({ host, seen } = installFakeHost(handlers, { windowId: "w1" }));
}

beforeEach(() => setup());

describe("page", () => {
  it("print and zoom name the view, the caller's by default", async () => {
    setup({ "krate/page.print": () => null, "krate/page.zoom": () => 1.25, "krate/page.setZoom": () => null });
    await page.print();
    await page.print("side");
    expect(await page.zoom()).toBe(1.25);
    await page.setZoom(2, "side");
    expect(seen.map((s) => s.args)).toEqual([[""], ["side"], [""], ["side", 2]]);
  });

  it("printToPdf maps a named size, a custom size and margins", async () => {
    const pdf = new Uint8Array([0x25, 0x50, 0x44, 0x46]);
    setup({ "krate/page.printToPdf": () => pdf });
    expect(await page.printToPdf()).toEqual(pdf);
    await page.printToPdf({ view: "v1", landscape: true, pageSize: "Letter", margins: { top: 1, left: 0.5 } });
    await page.printToPdf({ pageSize: { width: 5, height: 7 }, printBackground: true });
    expect(seen[0].args).toEqual(["", { landscape: false, printBackground: false }]);
    expect(seen[1].args).toEqual([
      "v1",
      { landscape: true, printBackground: false, pageSize: "Letter", margins: { top: 1, right: 0, bottom: 0, left: 0.5 } },
    ]);
    expect(seen[2].args).toEqual(["", { landscape: false, printBackground: true, pageWidth: 5, pageHeight: 7 }]);
  });

  it("find sends the options and onFind delivers the results", async () => {
    setup(
      { "krate/page.find": () => null, "krate/page.stopFind": () => null },
      {
        "krate/page.findEvents": async function* () {
          yield { view: "w1", text: "foo", matches: 3, active: 1, final: true };
          await new Promise(() => {});
        } as unknown as FakeHandler,
      },
    );
    const got: page.FindEvent[] = [];
    const stop = page.onFind((e) => got.push(e));
    await page.find("foo", { matchCase: true, continue: true });
    await page.stopFind("side");
    await flushAll();
    expect(seen.find((s) => s.fn === "krate/page.find")?.args).toEqual([
      "",
      "foo",
      { backward: false, matchCase: true, noWrap: false, continue: true },
    ]);
    expect(seen.find((s) => s.fn === "krate/page.stopFind")?.args).toEqual(["side"]);
    expect(got).toEqual([{ view: "w1", text: "foo", matches: 3, active: 1, final: true }]);
    stop();
  });

  it("capture resolves to bytes; a missing permission is not_found", async () => {
    setup({ "krate/page.capture": () => new Uint8Array([0x89, 0x50, 0x4e, 0x47]) });
    expect(Array.from(await page.capture("v2"))).toEqual([0x89, 0x50, 0x4e, 0x47]);
    expect(seen[0].args).toEqual(["v2"]);
    setup();
    await expect(page.capture()).rejects.toMatchObject({ code: "not_found" });
  });

  it("unsupported engines reject with the host's code", async () => {
    setup({
      "krate/page.printToPdf": () => {
        throw new KrateError("unsupported", "printing needs macOS 11");
      },
    });
    await expect(page.printToPdf()).rejects.toMatchObject({ code: "unsupported" });
  });
});

describe("downloads", () => {
  it("configure defaults to asking; onDone fires for completed downloads only", async () => {
    setup(
      { "krate/downloads.configure": () => null, "krate/downloads.cancel": () => null, "krate/downloads.list": () => [] },
      {
        "krate/downloads.events": async function* () {
          yield { id: 1, view: "w1", url: "u", path: "/d/a", state: "progress", received: 5, total: 10, error: "" };
          yield { id: 1, view: "w1", url: "u", path: "/d/a", state: "completed", received: 10, total: 10, error: "" };
          yield { id: 2, view: "w1", url: "v", path: "/d/b", state: "cancelled", received: 1, total: 10, error: "" };
          await new Promise(() => {});
        } as unknown as FakeHandler,
      },
    );
    await downloads.configure({ dir: "/d" });
    await downloads.configure({});
    await downloads.cancel(3);
    expect(await downloads.list()).toEqual([]);
    expect(seen.map((s) => [s.fn, s.args])).toEqual([
      ["krate/downloads.configure", [{ dir: "/d", ask: false }]],
      ["krate/downloads.configure", [{ dir: "", ask: false }]],
      ["krate/downloads.cancel", [3]],
      ["krate/downloads.list", []],
    ]);
    const done: string[] = [];
    const states: string[] = [];
    const stopDone = downloads.onDone((e) => done.push(e.path));
    const stopAll = downloads.onEvent((e) => states.push(e.state));
    await flushAll();
    expect(done).toEqual(["/d/a"]);
    expect(states).toEqual(["progress", "completed", "cancelled"]);
    stopDone();
    stopAll();
  });
});

describe("cookies", () => {
  it("list, get and delete pass the url; set normalises the input", async () => {
    setup({
      "krate/cookies.list": () => [],
      "krate/cookies.get": () => null,
      "krate/cookies.set": () => null,
      "krate/cookies.delete": () => null,
      "krate/cookies.clearSiteData": () => null,
    });
    await cookies.list("https://api.example.com");
    expect(await cookies.get("sid")).toBeNull();
    await cookies.set({ name: "a", value: "b", expires: new Date(1700000000000), sameSite: "lax" }, "https://x.test");
    await cookies.set({ name: "s", value: "t" });
    await cookies.delete("a", "https://x.test");
    await cookies.clearSiteData({ cache: true });
    expect(seen.map((s) => [s.fn, s.args])).toEqual([
      ["krate/cookies.list", ["https://api.example.com"]],
      ["krate/cookies.get", ["sid", ""]],
      [
        "krate/cookies.set",
        [{ name: "a", value: "b", domain: "", path: "", expires: 1700000000000, secure: false, httpOnly: false, sameSite: "lax" }, "https://x.test"],
      ],
      ["krate/cookies.set", [{ name: "s", value: "t", domain: "", path: "", expires: null, secure: false, httpOnly: false, sameSite: "" }, ""]],
      ["krate/cookies.delete", ["a", "https://x.test"]],
      ["krate/cookies.clearSiteData", [{ cookies: false, cache: true, storage: false }]],
    ]);
  });

  it("the app origin is unsupported where WebKit keeps no cookies for it", async () => {
    setup({
      "krate/cookies.list": () => {
        throw new KrateError("unsupported", "web engines keep no cookies for the krate: scheme");
      },
    });
    await expect(cookies.list()).rejects.toMatchObject({ code: "unsupported" });
  });
});
