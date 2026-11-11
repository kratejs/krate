import { afterEach, describe, expect, it } from "vitest";
import { createClient, setClient, KrateError, type Client } from "../src/index.js";
import { createFakeHost, type FakeHandler } from "../src/fakehost.js";
import * as os from "../src/os.js";
import * as fs from "../src/fs.js";
import * as db from "../src/db.js";
import * as kv from "../src/kv.js";
import * as permission from "../src/permission.js";

let client: Client | undefined;
afterEach(() => {
  client?.close();
  client = undefined;
});

// install registers handlers on a fake host and makes it the shared client.
function install(handlers: Record<string, FakeHandler>) {
  const seen: { id: string; args: unknown[] }[] = [];
  const registry: Record<string, FakeHandler> = {};
  for (const [id, h] of Object.entries(handlers)) {
    registry[id] = (args, ctx) => {
      seen.push({ id, args });
      return h(args, ctx);
    };
  }
  const host = createFakeHost(registry);
  client = createClient({ fetch: host.fetch });
  setClient(client);
  return { host, seen };
}

describe("os", () => {
  it("directories and info", async () => {
    const info = { os: "linux", arch: "amd64", osVersion: "x", hostname: "h", locale: "en-US", appId: "a.b", appVersion: "1" };
    const { seen } = install({
      "krate/os.appDataDir": () => "/data",
      "krate/os.cacheDir": () => "/cache",
      "krate/os.configDir": () => "/config",
      "krate/os.logDir": () => "/log",
      "krate/os.tempDir": () => "/tmp/x",
      "krate/os.info": () => info,
    });
    expect(await os.appDataDir()).toBe("/data");
    expect(await os.cacheDir()).toBe("/cache");
    expect(await os.configDir()).toBe("/config");
    expect(await os.logDir()).toBe("/log");
    expect(await os.tempDir()).toBe("/tmp/x");
    expect(await os.info()).toEqual(info);
    expect(seen.every((s) => s.args.length === 0)).toBe(true);
  });

  it("openExternal and secrets pass their arguments", async () => {
    const store = new Map<string, string>();
    const { seen } = install({
      "krate/os.openExternal": () => null,
      "krate/os.secretSet": ([k, v]) => void store.set(k as string, v as string),
      "krate/os.secretGet": ([k]) => store.get(k as string) ?? null,
      "krate/os.secretDelete": ([k]) => void store.delete(k as string),
    });
    await os.openExternal("https://example.com");
    expect(seen[0]).toEqual({ id: "krate/os.openExternal", args: ["https://example.com"] });
    expect(await os.secretGet("tok")).toBeNull();
    await os.secretSet("tok", "s3cret");
    expect(await os.secretGet("tok")).toBe("s3cret");
    await os.secretDelete("tok");
    expect(await os.secretGet("tok")).toBeNull();
  });

  it("surfaces an unsupported platform", async () => {
    install({
      "krate/os.secretSet": () => {
        throw new KrateError("unsupported", "no Secret Service provider");
      },
    });
    await expect(os.secretSet("k", "v")).rejects.toMatchObject({ code: "unsupported" });
  });
});

describe("fs", () => {
  it("sends paths and data and decodes dates and bytes", async () => {
    const when = new Date("2026-11-11T07:23:15.123Z");
    const { seen } = install({
      "krate/fs.readFile": () => new Uint8Array([1, 2, 3]),
      "krate/fs.readText": () => "text",
      "krate/fs.readDir": () => [{ name: "a", isDir: false, size: 3, modTime: when }],
      "krate/fs.stat": () => ({ name: "a", size: 3, isDir: false, modTime: when, mode: 0o644 }),
      "krate/fs.exists": () => true,
      "krate/fs.writeFile": () => null,
      "krate/fs.writeText": () => null,
      "krate/fs.mkdir": () => null,
      "krate/fs.remove": () => null,
      "krate/fs.rename": () => null,
    });
    expect(Array.from(await fs.readFile("a.bin"))).toEqual([1, 2, 3]);
    expect(await fs.readText("a.txt")).toBe("text");
    const list = await fs.readDir(".");
    expect(list[0].modTime).toEqual(when);
    expect((await fs.stat("a")).mode).toBe(0o644);
    expect(await fs.exists("a")).toBe(true);
    await fs.writeFile("b.bin", new Uint8Array([9]));
    await fs.writeText("b.txt", "hi");
    await fs.mkdir("d/e");
    await fs.remove("d");
    await fs.remove("d", { recursive: true });
    await fs.rename("b.txt", "c.txt");

    const byId = (id: string) => seen.filter((s) => s.id === id).map((s) => s.args);
    expect(Array.from((byId("krate/fs.writeFile")[0][1] as Uint8Array))).toEqual([9]);
    expect(byId("krate/fs.writeFile")[0][0]).toBe("b.bin");
    expect(byId("krate/fs.writeText")[0]).toEqual(["b.txt", "hi"]);
    expect(byId("krate/fs.mkdir")[0]).toEqual(["d/e"]);
    expect(byId("krate/fs.remove")).toEqual([["d"], ["d", true]]);
    expect(byId("krate/fs.rename")[0]).toEqual(["b.txt", "c.txt"]);
  });

  it("scope errors carry their data", async () => {
    install({
      "krate/fs.readText": () => {
        throw new KrateError("permission_denied", "outside", { permission: "fs", reason: "outside_scope", path: "/etc/passwd" });
      },
    });
    const err = await fs.readText("/etc/passwd").catch((e) => e);
    expect(err).toBeInstanceOf(KrateError);
    expect(err.data).toEqual({ permission: "fs", reason: "outside_scope", path: "/etc/passwd" });
    expect(permission.isPermissionDenied(err)).toBe(true);
    expect(permission.deniedPermission(err)).toBe("fs");
  });
});

describe("db", () => {
  it("exec and query send [sql, params]", async () => {
    const { seen } = install({
      "krate/db.exec": () => ({ rowsAffected: 1, lastInsertId: 7 }),
      "krate/db.query": () => [{ id: 1, name: "x", blob: new Uint8Array([1]), at: new Date(0) }],
    });
    expect(await db.exec("INSERT INTO t(a, b, c, d) VALUES(?, ?, ?, ?)", 1, "s", new Uint8Array([2]), null)).toEqual({
      rowsAffected: 1,
      lastInsertId: 7,
    });
    const rows = await db.query<{ id: number; name: string }>("SELECT * FROM t WHERE id = ?", 1);
    expect(rows[0].name).toBe("x");
    expect(seen[0].args[0]).toBe("INSERT INTO t(a, b, c, d) VALUES(?, ?, ?, ?)");
    const params = seen[0].args[1] as unknown[];
    expect(params.slice(0, 2)).toEqual([1, "s"]);
    expect(Array.from(params[2] as Uint8Array)).toEqual([2]);
    expect(params[3]).toBeNull();
    expect(seen[1].args).toEqual(["SELECT * FROM t WHERE id = ?", [1]]);
  });

  it("omits the params array when there are none", async () => {
    const { seen } = install({ "krate/db.query": () => [], "krate/db.exec": () => ({ rowsAffected: 0, lastInsertId: 0 }) });
    await db.query("SELECT 1");
    await db.exec("DELETE FROM t");
    expect(seen.map((s) => s.args)).toEqual([["SELECT 1"], ["DELETE FROM t"]]);
  });

  it("transaction sends statement objects", async () => {
    const { seen } = install({ "krate/db.transaction": ([s]) => (s as unknown[]).map(() => ({ rowsAffected: 1, lastInsertId: 0 })) });
    const res = await db.transaction([{ sql: "INSERT INTO t VALUES(?)", params: [1] }, { sql: "DELETE FROM t" }]);
    expect(res).toHaveLength(2);
    expect(seen[0].args).toEqual([[{ sql: "INSERT INTO t VALUES(?)", params: [1] }, { sql: "DELETE FROM t" }]]);
  });

  it("sql errors keep their code", async () => {
    install({
      "krate/db.exec": () => {
        throw new KrateError("constraint", "UNIQUE constraint failed");
      },
    });
    await expect(db.exec("INSERT INTO t VALUES(1)")).rejects.toMatchObject({ code: "constraint" });
  });
});

describe("kv", () => {
  it("round trips wire values through the fake host", async () => {
    const store = new Map<string, unknown>();
    const { seen } = install({
      "krate/kv.get": ([k]) => store.get(k as string) ?? null,
      "krate/kv.set": ([k, v]) => void store.set(k as string, v),
      "krate/kv.delete": ([k]) => void store.delete(k as string),
      "krate/kv.keys": ([p]) => [...store.keys()].filter((k) => k.startsWith((p as string) ?? "")).sort(),
    });
    const value = { when: new Date(5), bytes: new Uint8Array([1, 2]), tags: new Set(["a"]), nested: { n: [1, 2] } };
    await kv.set("k", value);
    const back = await kv.get<typeof value>("k");
    expect(back?.when).toEqual(new Date(5));
    expect(Array.from(back!.bytes)).toEqual([1, 2]);
    expect(back?.tags).toEqual(new Set(["a"]));
    expect(back?.nested).toEqual({ n: [1, 2] });
    expect(await kv.get("missing")).toBeNull();
    await kv.set("a/1", 1);
    await kv.set("a/2", 2);
    expect(await kv.keys("a/")).toEqual(["a/1", "a/2"]);
    expect(await kv.keys()).toEqual(["a/1", "a/2", "k"]);
    await kv.delete("k");
    expect(await kv.get("k")).toBeNull();
    expect(seen.find((s) => s.id === "krate/kv.keys")?.args).toEqual(["a/"]);
    expect(seen.filter((s) => s.id === "krate/kv.keys")[1].args).toEqual([]);
  });
});

describe("permission", () => {
  it("query asks the host", async () => {
    const granted = new Set(["kv", "db"]);
    install({ "krate/permission.query": ([n]) => granted.has(n as string) });
    expect(await permission.query("kv")).toBe(true);
    expect(await permission.query("fs")).toBe(false);
  });

  it("recognizes a denial and names the permission", async () => {
    install({
      "krate/os.secretGet": () => {
        throw new KrateError("permission_denied", 'permission "os:secrets.read" is not granted in app.json', { permission: "os:secrets.read" });
      },
    });
    const err = await os.secretGet("k").catch((e) => e);
    expect(permission.isPermissionDenied(err)).toBe(true);
    expect(permission.deniedPermission(err)).toBe("os:secrets.read");
    expect(permission.deniedPermission(new Error("x"))).toBeUndefined();
    expect(permission.deniedPermission(new KrateError("not_found", "x"))).toBeUndefined();
  });
});
