// @vitest-environment node
//
// End-to-end test of the Node SSR sidecar. server-renderer.ts starts a server
// when it is loaded, so the test runs the built dist/server-renderer.js as the
// Go host does: with KRATE_* environment variables, a server manifest and page
// bundles on disk, and talks to it over HTTP. Skipped when the runtime has not
// been built (KRATE_REQUIRE_E2E=1 turns that into a failure, as in CI).
import { spawn, type ChildProcess } from "node:child_process";
import { createHash } from "node:crypto";
import fs from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { afterAll, beforeAll, describe, expect, it } from "vitest";

const here = path.dirname(new URL(import.meta.url).pathname);
const renderer = path.resolve(here, "..", "dist", "server-renderer.js");
const serverJs = path.resolve(here, "..", "dist", "server.js");
const built = fs.existsSync(renderer) && fs.existsSync(serverJs);
if (!built && process.env.KRATE_REQUIRE_E2E) {
  throw new Error("dist/server-renderer.js is missing (KRATE_REQUIRE_E2E is set): build @krate/runtime first");
}

const SECRET = "test-secret";
let tmp = "";
let out = "";
let isrFile = "";
let child: ChildProcess | null = null;
let port = 0;
let logs = "";

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const s = net.createServer();
    s.once("error", reject);
    s.listen(0, "127.0.0.1", () => {
      const p = (s.address() as net.AddressInfo).port;
      s.close(() => resolve(p));
    });
  });
}

function write(rel: string, data: string) {
  const f = path.join(out, rel);
  fs.mkdirSync(path.dirname(f), { recursive: true });
  fs.writeFileSync(f, data);
}

const jsxImport = `import { jsx } from ${JSON.stringify(pathToFileURL(serverJs).href)};\n`;

function bundles() {
  write(
    "pages/ssr.mjs",
    jsxImport +
      `export default (props) => jsx("main", { children: [jsx("title", { children: "T " + ((props.params || {}).id || "") }), jsx("p", { children: "q=" + ((props.query || {}).q || "") })] });\n`,
  );
  write("pages/isr.mjs", jsxImport + `let n = 0;\nexport default () => jsx("p", { children: "isr-render-" + (++n) });\n`);
  write(
    "pages/boom.mjs",
    jsxImport + `export default () => { const e = new Error("secret detail"); e.stack = "at x"; throw e; };\n`,
  );
  write("pages/redirect.mjs", jsxImport + `export default () => { throw Object.assign(new Error("r"), { __krateRedirect: "/login" }); };\n`);
  write("pages/missing.mjs", jsxImport + `export default () => { throw Object.assign(new Error("NEXT_NOT_FOUND"), { __krateNotFound: true, digest: "NEXT_NOT_FOUND" }); };\n`);
  write(
    "pages/regions.mjs",
    jsxImport +
      `export const Slow = async () => { await new Promise((ok) => setTimeout(ok, 250)); return jsx("i", { children: "slow" }); };\n` +
      `export const Fast = async () => jsx("i", { children: "fast" });\n` +
      `export const Bad = async () => { throw new Error("region failed"); };\n` +
      `export default () => jsx("p", { children: "whole page" });\n`,
  );
  write(
    "rt/price.js",
    `globalThis.__krate_render = (json) => { const p = JSON.parse(json); return "<b>" + p.price + "/" + (p.params ? p.params.id : "-") + "</b>"; };\n`,
  );
  write("404.html", "<h1>custom not found</h1>");
  write("500.html", "<h1>custom error</h1>");
  const page = (route: string, mode: string, bundle: string, extra: object = {}) => ({ route, source: route + ".tsx", mode, bundlePath: "pages/" + bundle + ".mjs", ...extra });
  write(
    ".krate/server-manifest.json",
    JSON.stringify({
      pages: [
        page("/ssr/[id]", "ssr", "ssr"),
        page("/isr", "isr", "isr", { revalidate: 300 }),
        page("/boom", "ssr", "boom"),
        page("/redirect", "ssr", "redirect"),
        page("/missing", "ssr", "missing"),
        page("/regions", "ssr", "regions", { serverComponents: true }),
      ],
      regions: {
        "/regions": [
          { id: "slow", serverExport: "Slow" },
          { id: "fast", serverExport: "Fast" },
          { id: "bad", serverExport: "Bad" },
          { id: "price", bundlePath: "rt/price.js", props: { price: 12 } },
        ],
      },
    }),
  );
}

async function start(): Promise<void> {
  port = await freePort();
  logs = "";
  child = spawn(process.execPath, [renderer], {
    env: {
      PATH: process.env.PATH,
      KRATE_SSR_PORT: String(port),
      KRATE_OUT_DIR: out,
      KRATE_ROOT: tmp,
      KRATE_INTERNAL_SECRET: SECRET,
      KRATE_ISR_CACHE: isrFile,
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  child.stdout!.on("data", (d) => (logs += d));
  child.stderr!.on("data", (d) => (logs += d));
  const deadline = Date.now() + 15000;
  for (;;) {
    try {
      const r = await fetch(`http://127.0.0.1:${port}/__krate/ssr/health`);
      if (r.ok) return;
    } catch {
      // not listening yet
    }
    if (child.exitCode !== null) throw new Error("renderer exited early:\n" + logs);
    if (Date.now() > deadline) throw new Error("renderer did not start:\n" + logs);
    await new Promise((ok) => setTimeout(ok, 50));
  }
}

async function stop(): Promise<void> {
  const c = child;
  child = null;
  if (!c || c.exitCode !== null) return;
  await new Promise<void>((ok) => {
    c.once("exit", () => ok());
    c.kill("SIGKILL");
  });
}

const secret = { "x-krate-internal": SECRET };
const url = (p: string) => `http://127.0.0.1:${port}${p}`;

async function regions(body: object, headers: Record<string, string> = secret) {
  const res = await fetch(url("/__krate/regions"), { method: "POST", headers, body: JSON.stringify(body) });
  const text = await res.text();
  return { res, frames: text.split("\n").filter(Boolean).map((l) => JSON.parse(l) as Record<string, any>) };
}

describe.skipIf(!built)("server-renderer sidecar", () => {
  beforeAll(async () => {
    tmp = fs.mkdtempSync(path.join(os.tmpdir(), "krate-ssr-"));
    out = path.join(tmp, "dist");
    isrFile = path.join(tmp, "isr-cache.json");
    bundles();
    await start();
  }, 30000);

  afterAll(async () => {
    await stop();
    fs.rmSync(tmp, { recursive: true, force: true });
  });

  it("reports health with the number of SSR pages", async () => {
    const r = await fetch(url("/__krate/ssr/health"));
    expect(await r.json()).toEqual({ ok: true, pages: 6 });
  });

  describe("internal endpoints need the host's secret", () => {
    it("refuses regions, revalidation and direct renders without it", async () => {
      expect((await regions({ route: "/regions", url: "/regions" }, {})).res.status).toBe(403);
      expect((await fetch(url("/__krate/ssr/cache-invalidate"), { method: "POST", body: "{}" })).status).toBe(403);
      expect((await fetch(url("/__krate/ssr/cache-invalidate"), { method: "POST", body: "{}", headers: { "x-krate-internal": "wrong" } })).status).toBe(403);
      expect((await fetch(url("/ssr/[id]"))).status).toBe(403);
    });

    it("answers 405 to a GET on a mutating endpoint, before the secret check", async () => {
      const r = await fetch(url("/__krate/ssr/revalidate"));
      expect(r.status).toBe(405);
      expect(r.headers.get("allow")).toBe("POST");
    });

    it("applies an authorised invalidation and reports the counts", async () => {
      const r = await fetch(url("/__krate/ssr/cache-invalidate"), { method: "POST", headers: secret, body: JSON.stringify({ tags: ["nothing"] }) });
      expect(r.status).toBe(200);
      expect(await r.json()).toMatchObject({ ok: true, workers: 1, isrEntries: 0, regionEntries: 0 });
      const bad = await fetch(url("/__krate/ssr/cache-invalidate"), { method: "POST", headers: secret, body: "{not json" });
      expect(bad.status).toBe(500);
    });
  });

  describe("direct page requests", () => {
    it("renders a request-time page; a direct request carries no params or query", async () => {
      // The Go host asks for regions instead and forwards both (see below).
      const r = await fetch(url("/ssr/[id]?q=1"), { headers: secret });
      expect(r.status).toBe(200);
      expect(r.headers.get("content-type")).toBe("text/html; charset=utf-8");
      expect(await r.text()).toContain("<p>q=</p>");
    });

    it("serves the build's 404 page for an unknown route and for notFound()", async () => {
      for (const p of ["/nowhere", "/missing"]) {
        const r = await fetch(url(p), { headers: secret });
        expect(r.status, p).toBe(404);
        expect(await r.text()).toBe("<h1>custom not found</h1>");
      }
    });

    it("serves the build's 500 page for a failing render, without the error text", async () => {
      const r = await fetch(url("/boom"), { headers: secret });
      expect(r.status).toBe(500);
      const body = await r.text();
      expect(body).toBe("<h1>custom error</h1>");
      expect(body).not.toContain("secret detail");
      const digest = createHash("sha256").update("secret detail\nat x").digest("hex").slice(0, 10);
      expect(logs).toContain(`digest ${digest}`);
    });

    it("redirects with 307 and a Location header", async () => {
      const r = await fetch(url("/redirect"), { headers: secret, redirect: "manual" });
      expect(r.status).toBe(307);
      expect(r.headers.get("location")).toBe("/login");
    });

    it("marks ISR responses from the cache", async () => {
      const a = await fetch(url("/isr"), { headers: secret });
      const first = await a.text();
      expect(a.headers.get("x-krate-cache")).toBeNull();
      const b = await fetch(url("/isr"), { headers: secret });
      expect(b.headers.get("x-krate-cache")).toBe("HIT");
      expect(await b.text()).toBe(first);
    });
  });

  describe("/__krate/regions", () => {
    const body = (extra: object = {}) => ({ route: "/regions", url: "/regions", method: "GET", headers: {}, ...extra });

    it("streams frames as regions finish, not in document order, and ends with a count", async () => {
      const { res, frames } = await regions(
        body({ regions: [{ id: "slow" }, { id: "fast" }, { id: "price" }], params: { id: "9" } }),
      );
      expect(res.headers.get("content-type")).toContain("application/x-ndjson");
      expect(res.headers.get("cache-control")).toBe("no-store");
      expect(frames[frames.length - 1]).toEqual({ type: "end", count: 3 });
      const ids = frames.filter((f) => f.type === "region").map((f) => f.id);
      expect(ids).toHaveLength(3);
      expect(ids.indexOf("slow")).toBe(2); // the 250ms region is written last
      const byId = Object.fromEntries(frames.filter((f) => f.type === "region").map((f) => [f.id, f.html]));
      expect(byId).toEqual({ slow: "<i>slow</i>", fast: "<i>fast</i>", price: "<b>12/9</b>" });
    });

    it("reports a failing server component region as a fatal error frame with a digest", async () => {
      const { frames } = await regions(body({ regions: [{ id: "bad" }, { id: "fast" }] }));
      const bad = frames.find((f) => f.id === "bad")!;
      expect(bad).toMatchObject({ type: "error", status: 500, fatal: true, error: "region failed" });
      expect(bad.digest).toMatch(/^[0-9a-f]{10}$/);
      expect(frames.find((f) => f.id === "fast")).toMatchObject({ type: "region", html: "<i>fast</i>" });
      expect(frames[frames.length - 1]).toEqual({ type: "end", count: 2 });
    });

    it("falls back to the manifest's regions when the host sends no list", async () => {
      const { frames } = await regions(body());
      expect(frames.filter((f) => f.type !== "end").map((f) => f.id).sort()).toEqual(["bad", "fast", "price", "slow"]);
    });

    it("skips an unknown region id, so its baked content stays", async () => {
      const { frames } = await regions(body({ regions: [{ id: "ghost" }] }));
      expect(frames).toEqual([{ type: "skip", id: "ghost" }, { type: "end", count: 1 }]);
    });

    it("renders a coarse page region through the page ISR path", async () => {
      const { frames } = await regions({ route: "/ssr/[id]", url: "/ssr/5", method: "GET", headers: {}, params: { id: "5" }, query: { q: "z" }, regions: [{ id: "page", kind: "page" }] });
      expect(frames[0]).toMatchObject({ type: "region", id: "page", kind: "page", status: 200, title: "T 5" });
      expect(String(frames[0].html)).toContain("<p>q=z</p>");
    });

    it("answers an unknown route with an error frame and an end frame", async () => {
      const { frames } = await regions({ route: "/none", url: "/none", method: "GET", headers: {} });
      expect(frames).toEqual([{ type: "error", status: 404, error: "no such page" }, { type: "end", count: 0 }]);
    });

    it("turns a malformed body into an error frame, not a crash", async () => {
      const res = await fetch(url("/__krate/regions"), { method: "POST", headers: secret, body: "{nope" });
      const frames = (await res.text()).split("\n").filter(Boolean).map((l) => JSON.parse(l));
      expect(frames[0]).toMatchObject({ type: "error", status: 500 });
      expect((await fetch(url("/__krate/ssr/health"))).status).toBe(200);
    });
  });

  it("keeps rendered ISR pages across a restart", async () => {
    // Warm an ISR variant, wait for the debounced write, bounce the process.
    const first = await (await fetch(url("/isr"), { headers: secret })).text();
    const deadline = Date.now() + 5000;
    while (!fs.existsSync(isrFile) && Date.now() < deadline) await new Promise((ok) => setTimeout(ok, 100));
    expect(fs.existsSync(isrFile)).toBe(true);
    const saved = JSON.parse(fs.readFileSync(isrFile, "utf-8"));
    expect(saved.entries.some((e: any) => e.key === "/isr" && e.html === first)).toBe(true);

    await stop();
    await start();
    const again = await fetch(url("/isr"), { headers: secret });
    expect(again.headers.get("x-krate-cache")).toBe("HIT");
    expect(await again.text()).toBe(first);
    expect(logs).toContain("Loaded 1 persisted ISR entries");
  }, 30000);
});
