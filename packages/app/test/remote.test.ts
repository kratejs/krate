import { beforeEach, describe, expect, it } from "vitest";
import type { FakeHandler, FakeHost } from "../src/fakehost.js";
import { installFakeHost, type SeenCall } from "./sdkhost.js";
import * as remote from "../src/remote.js";
import { flushAll } from "./helpers.js";

let host: FakeHost;
let seen: SeenCall[];

function setup(answers: Record<string, (args: any[]) => unknown> = {}, extra: Record<string, FakeHandler> = {}) {
  const handlers: Record<string, FakeHandler> = { ...extra };
  for (const [fn, f] of Object.entries(answers)) handlers[fn] = (args) => f(args);
  ({ host, seen } = installFakeHost(handlers, { windowId: "main" }));
}

beforeEach(() => setup());

describe("remote", () => {
  it("openWindow and addView send every option and return handles", async () => {
    setup({ "krate/remote.open": () => "w2", "krate/remote.addView": () => "v1" });
    const win = await remote.openWindow({ url: "https://a.test/", partition: "persist:a", allow: ["https://b.test"], blocked: "block", title: "A" });
    expect(win.id).toBe("w2");
    const view = await remote.addView({ url: "https://a.test/", messages: true, side: "left", size: 0.3 }, "w2");
    expect(view.id).toBe("v1");
    expect(seen.map((s) => [s.fn, s.args])).toEqual([
      [
        "krate/remote.open",
        [{ url: "https://a.test/", partition: "persist:a", allow: ["https://b.test"], allowAnyHttps: false, blocked: "block", messages: false, userAgent: "", id: "", title: "A", width: 0, height: 0, fixed: false }],
      ],
      [
        "krate/remote.addView",
        ["w2", { url: "https://a.test/", partition: "", allow: [], allowAnyHttps: false, blocked: "", messages: true, userAgent: "", id: "", beside: "", side: "left", size: 0.3, minWidth: 0, minHeight: 0 }],
      ],
    ]);
  });

  it("post always names the target origin", async () => {
    setup({ "krate/remote.post": () => null, "krate/remote.navigate": () => null });
    await remote.post("v1", { n: 1 }, "https://a.test");
    await remote.navigate("v1", "https://a.test/x");
    expect(seen.map((s) => [s.fn, s.args])).toEqual([
      ["krate/remote.post", ["v1", "https://a.test", { n: 1 }]],
      ["krate/remote.navigate", ["v1", "https://a.test/x"]],
    ]);
  });

  it("rules, network settings and clearing fill in defaults", async () => {
    setup({
      "krate/remote.setRules": () => null,
      "krate/remote.setUserAgent": () => null,
      "krate/remote.setProxy": () => null,
      "krate/remote.clearData": () => null,
    });
    await remote.setRules("p", [{ urls: ["<all_urls>"], action: "block" }]);
    await remote.setUserAgent("p", "UA/1");
    await remote.setProxy("p", { mode: "direct" });
    await remote.clearData("p");
    expect(seen.map((s) => s.args)).toEqual([
      ["p", [{ urls: ["<all_urls>"], action: "block", redirect: "", headers: {} }]],
      ["p", "UA/1"],
      ["p", { mode: "direct", url: "", bypass: [] }],
      ["p", { cookies: false, cache: false, storage: false }],
    ]);
  });

  it("messages and events come from streams", async () => {
    setup(
      {},
      {
        "krate/remote.messages": async function* () {
          yield { view: "v1", origin: "https://a.test", data: { hi: 1 } };
          await new Promise(() => {});
        } as unknown as FakeHandler,
        "krate/remote.events": async function* () {
          yield { view: "v1", kind: "navBlocked", url: "https://x.test/", verdict: "external" };
          await new Promise(() => {});
        } as unknown as FakeHandler,
      },
    );
    const msgs: unknown[] = [];
    const evs: string[] = [];
    const a = remote.onMessage((m) => msgs.push(m));
    const b = remote.onRemoteEvent((e) => evs.push(e.kind + ":" + e.verdict));
    await flushAll();
    expect(msgs).toEqual([{ view: "v1", origin: "https://a.test", data: { hi: 1 } }]);
    expect(evs).toEqual(["navBlocked:external"]);
    a();
    b();
  });

  it("certificate and login handlers answer by id", async () => {
    setup(
      { "krate/remote.answerCertificate": () => null, "krate/remote.answerAuth": () => null },
      {
        "krate/remote.certificates": async function* () {
          yield { id: 4, view: "v1", url: "https://x.test/", host: "x.test", errors: ["untrusted"] };
          await new Promise(() => {});
        } as unknown as FakeHandler,
        "krate/remote.auth": async function* () {
          yield { id: 5, view: "v1", host: "x.test:443", realm: "r", scheme: "basic", proxy: false, retry: false };
          await new Promise(() => {});
        } as unknown as FakeHandler,
      },
    );
    const a = remote.onCertificateError((e) => e.host === "x.test");
    const b = remote.onAuth(() => ({ user: "u", password: "p" }));
    await flushAll();
    await flushAll();
    expect(seen.filter((s) => s.fn.includes("answer")).map((s) => [s.fn, s.args])).toEqual([
      ["krate/remote.answerCertificate", [4, true]],
      ["krate/remote.answerAuth", [5, "u", "p", true]],
    ]);
    a();
    b();
  });
});
