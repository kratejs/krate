import { describe, expect, it } from "vitest";
import { KrateError } from "../src/index.js";
import type { FakeHandler } from "../src/fakehost.js";
import { installFakeHost } from "./sdkhost.js";
import * as updater from "../src/updater.js";

const info = { version: "1.2.0", notes: "fixes", pubDate: "2026-01-01T00:00:00Z", size: 300 };

describe("updater", () => {
  it("check resolves the update or null", async () => {
    let answer: unknown = info;
    const { seen } = installFakeHost({ "krate/updater.check": () => answer });
    expect(await updater.check()).toEqual(info);
    answer = null;
    expect(await updater.check()).toBeNull();
    expect(seen.every((s) => s.args.length === 0)).toBe(true);
  });

  it("download reports progress until the stream ends", async () => {
    installFakeHost({
      "krate/updater.download": async function* () {
        yield { done: 100, total: 300 };
        yield { done: 300, total: 300 };
      } as unknown as FakeHandler,
    });
    const got: unknown[] = [];
    await updater.download((p) => got.push(p));
    expect(got).toEqual([
      { done: 100, total: 300 },
      { done: 300, total: 300 },
    ]);
  });

  it("download rejects with the host error code", async () => {
    installFakeHost({
      "krate/updater.download": async function* () {
        throw new KrateError("bad_signature", "signature does not match");
      } as unknown as FakeHandler,
    });
    await expect(updater.download()).rejects.toMatchObject({ code: "bad_signature" });
  });

  it("apply and relaunch call their ids", async () => {
    const { seen } = installFakeHost({ "krate/updater.apply": () => null, "krate/updater.relaunch": () => null });
    await updater.apply();
    await updater.relaunch();
    expect(seen.map((s) => s.id)).toEqual(["krate/updater.apply", "krate/updater.relaunch"]);
  });

  it("update runs check, download and apply, and skips both when up to date", async () => {
    let answer: unknown = info;
    const { seen } = installFakeHost({
      "krate/updater.check": () => answer,
      "krate/updater.download": async function* () {
        yield { done: 300, total: 300 };
      } as unknown as FakeHandler,
      "krate/updater.apply": () => null,
    });
    expect(await updater.update()).toEqual(info);
    expect(seen.map((s) => s.id)).toEqual(["krate/updater.check", "krate/updater.download", "krate/updater.apply"]);
    seen.length = 0;
    answer = null;
    expect(await updater.update()).toBeNull();
    expect(seen.map((s) => s.id)).toEqual(["krate/updater.check"]);
  });

  it("a missing permission rejects with permission_denied", async () => {
    installFakeHost({
      "krate/updater.check": () => {
        throw new KrateError("permission_denied", "no", { permission: "updater" });
      },
    });
    await expect(updater.check()).rejects.toMatchObject({ code: "permission_denied" });
  });
});
