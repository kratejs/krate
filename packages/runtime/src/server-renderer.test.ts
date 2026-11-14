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
    `globalThis.__krate_render = (json) => { const p = typeof json === "string" ? JSON.parse(json) : json; return "<b>" + p.price + "/" + (p.params ? p.params.id : "-") + "</b>"; };\n`,
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

  describe("internal endpoints need the host's secret", () => {
    it("refuses regions, revalidation and direct renders without it", async () => {
      expect((await regions({ route: "/regions", url: "/regions" }, {})).res.status).toBe(403);
      expect((await fetch(url("/__krate/ssr/cache-invalidate"), { method: "POST", body: "{}" })).status).toBe(403);
      expect((await fetch(url("/__krate/ssr/cache-invalidate"), { method: "POST", body: "{}", headers: { "x-krate-internal": "wrong" } })).status).toBe(403);
      expect((await fetch(url("/ssr/[id]"))).status).toBe(403);
    });
  });

  describe("direct page requests", () => {

    it("serves the build's 500 page for a failing render, without the error text", async () => {
      const r = await fetch(url("/boom"), { headers: secret });
      expect(r.status).toBe(500);
      const body = await r.text();
      expect(body).toBe("<h1>custom error</h1>");
      expect(body).not.toContain("secret detail");
      const digest = createHash("sha256").update("secret detail\nat x").digest("hex").slice(0, 10);
      expect(logs).toContain(`digest ${digest}`);
    });
  });
});
