import { beforeEach, describe, expect, it } from "vitest";
import { KrateError } from "../src/errors.js";
import type { FakeHandler, FakeHost } from "../src/fakehost.js";
import { installFakeHost, type SeenCall } from "./sdkhost.js";
import * as media from "../src/media.js";
import { flushAll } from "./helpers.js";

let host: FakeHost;
let seen: SeenCall[];

function setup(answers: Record<string, (args: any[]) => unknown> = {}, extra: Record<string, FakeHandler> = {}) {
  const handlers: Record<string, FakeHandler> = { ...extra };
  for (const [fn, f] of Object.entries(answers)) handlers[fn] = (args) => f(args);
  ({ host, seen } = installFakeHost(handlers, { windowId: "w1" }));
}

beforeEach(() => setup());

describe("media", () => {
  it("policy and decisions use the wire shapes of the host ids", async () => {
    setup({
      "krate/media.policy": () => ({ camera: "allow" }),
      "krate/media.setPolicy": () => null,
      "krate/media.decisions": () => [{ kind: "camera", scope: "app", allow: true, at: "2026-01-01T00:00:00Z" }],
      "krate/media.decide": () => null,
      "krate/media.reset": () => null,
    });
    expect(await media.getPolicy()).toEqual({ camera: "allow" });
    await media.setPolicy("microphone", "deny");
    expect(seen[1]).toMatchObject({ fn: "krate/media.setPolicy", args: ["microphone", "deny"] });
    expect((await media.decisions())[0].kind).toBe("camera");
    await media.decide("location", false);
    expect(seen[3].args).toEqual(["location", "app", false]);
    await media.decide("camera", true, "v2");
    expect(seen[4].args).toEqual(["camera", "v2", true]);
    await media.reset();
    await media.reset("screen");
    expect(seen[5].args).toEqual([]);
    expect(seen[6].args).toEqual(["screen"]);
  });

  it("onRequest answers each request with the handler's verdict", async () => {
    setup(
      { "krate/media.respond": () => null },
      {
        "krate/media.requests": async function* () {
          yield { id: 7, view: "main", origin: "krate://app", kinds: ["camera"], pending: true };
          yield { id: 8, view: "v2", origin: "krate://app", kinds: ["microphone"], pending: true };
          yield { id: 9, view: "main", origin: "krate://app", kinds: ["location"], pending: true };
          await new Promise(() => {});
        } as unknown as FakeHandler,
      },
    );
    const stop = media.onRequest(async (r) => {
      if (r.id === 7) return true;
      if (r.id === 8) return { allow: false, remember: "view" };
      throw new Error("handler bug");
    });
    await flushAll();
    const answers = seen.filter((c) => c.fn === "krate/media.respond").map((c) => c.args);
    expect(answers).toContainEqual([7, true, ""]);
    expect(answers).toContainEqual([8, false, "view"]);
    // A throwing handler denies.
    expect(answers).toContainEqual([9, false, ""]);
    stop();
  });

  it("os consent, sources and chooseSource", async () => {
    setup({
      "krate/media.osStatus": () => "notDetermined",
      "krate/media.osRequest": () => "granted",
      "krate/media.sources": () => [{ id: "screen:1", kind: "screen", name: "Display 1", app: "", width: 1920, height: 1080, thumbnail: null }],
      "krate/media.chooseSource": () => {
        throw new KrateError("unsupported", "the system picker decides");
      },
    });
    expect(await media.osStatus("camera")).toBe("notDetermined");
    expect(await media.osRequest("camera")).toBe("granted");
    expect((await media.sources({ thumbnailWidth: 160 }))[0].name).toBe("Display 1");
    expect(seen[2].args).toEqual([{ thumbnailWidth: 160 }]);
    await media.sources();
    expect(seen[3].args).toEqual([{ thumbnailWidth: 0 }]);
    await expect(media.chooseSource("screen:1")).rejects.toMatchObject({ code: "unsupported" });
  });

  it("a missing permission is a not_found KrateError", async () => {
    setup();
    await expect(media.getPolicy()).rejects.toMatchObject({ code: "not_found" });
  });
});
