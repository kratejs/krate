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
import { decodeFrames, REGION_FRAMES_TYPE } from "./region-wire.js";
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

// Frames with the fields a client reads: empty values and the renderer's
// internal "cached" flag dropped, so NDJSON and binary frames compare equal.
function norm(f: Record<string, any>) {
  const out: Record<string, any> = {};
  // A stack's async frames vary between renders, so only its first line counts;
  // the digest hashes the stack, so only its presence does.
  for (const [k, v] of Object.entries(f)) if (v !== "" && v !== false && v !== 0 && k !== "cached") out[k] = k === "stack" ? String(v).split("\n")[0] : k === "digest" ? true : v;
  return out;
}

async function binaryRegions(body: object, headers: Record<string, string> = secret) {
  const res = await fetch(url("/__krate/regions"), {
    method: "POST",
    headers: { ...headers, accept: REGION_FRAMES_TYPE },
    body: JSON.stringify(body),
  });
  const data = new Uint8Array(await res.arrayBuffer());
  return { res, frames: res.status === 200 ? decodeFrames(data) : [] };
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

  describe("dev manifest reload", () => {
    it("serves a page added to the manifest after startup once the host invalidates", async () => {
      const probe = { route: "/added/[id]", url: "/added/1", regions: [{ id: "page", kind: "page" }], params: { id: "1" } };
      expect((await regions(probe)).frames[0]).toMatchObject({ type: "error", status: 404 });

      write("pages/added.mjs", jsxImport + `export default (props) => jsx("p", { children: "added " + props.params.id });\n`);
      const file = path.join(out, ".krate", "server-manifest.json");
      const manifest = JSON.parse(fs.readFileSync(file, "utf-8"));
      manifest.pages.push({ route: "/added/[id]", source: "/added/[id].tsx", mode: "ssr", bundlePath: "pages/added.mjs" });
      fs.writeFileSync(file, JSON.stringify(manifest));

      // A rebuild that adds a page sends an invalidation without a route when
      // no known page matched.
      const res = await fetch(url("/__krate/ssr/invalidate"), { method: "POST", headers: secret, body: JSON.stringify({ route: "", source: "", bundlePath: "" }) });
      expect(res.status).toBe(200);
      const { frames } = await regions(probe);
      expect(frames.find((f) => f.type === "region" || f.html)).toBeTruthy();
      expect(JSON.stringify(frames)).toContain("added 1");
    });
  });

  describe("page module graph in dev", () => {
    it("loads the modules a page bundle imports afresh after each dev invalidation", async () => {
      const page = { route: "/graph", url: "/graph", regions: [{ id: "page", kind: "page" }] };
      const dep = (v: string) => `export const label = ${JSON.stringify(v)};\nexport const identity = globalThis.__graphId = (globalThis.__graphId || 0) + 1;\n`;
      write(".krate/server-bundles/client/dep.mjs", dep("one"));
      write(
        ".krate/server-bundles/graph.server.mjs",
        jsxImport + `import { label } from "./client/dep.mjs";\nexport default () => jsx("p", { children: "graph " + label });\n`,
      );
      const file = path.join(out, ".krate", "server-manifest.json");
      const manifest = JSON.parse(fs.readFileSync(file, "utf-8"));
      manifest.pages.push({ route: "/graph", source: "/graph.tsx", mode: "ssr", bundlePath: ".krate/server-bundles/graph.server.mjs" });
      fs.writeFileSync(file, JSON.stringify(manifest));
      const invalidate = () =>
        fetch(url("/__krate/ssr/invalidate"), {
          method: "POST",
          headers: secret,
          body: JSON.stringify({ route: "/graph", source: "/graph.tsx", bundlePath: ".krate/server-bundles/graph.server.mjs" }),
        });
      expect((await invalidate()).status).toBe(200);
      expect(JSON.stringify((await regions(page)).frames)).toContain("graph one");

      // The page bundle does not change; a module it imports does.
      write(".krate/server-bundles/client/dep.mjs", dep("two"));
      expect((await invalidate()).status).toBe(200);
      expect(JSON.stringify((await regions(page)).frames)).toContain("graph two");
    });
  });

  describe("region frame formats", () => {
    const page = { id: "page", kind: "page" };
    const cases: Array<[string, object]> = [
      ["component and server regions", { route: "/regions", url: "/regions", regions: [{ id: "fast", kind: "component" }, { id: "bad", kind: "component" }, { id: "price", kind: "component" }, { id: "nope", kind: "component" }] }],
      ["manifest regions when none are listed", { route: "/regions", url: "/regions" }],
      ["page region with params and query", { route: "/ssr/[id]", url: "/ssr/7?q=a%26b", params: { id: "7" }, query: { q: "a&b" }, regions: [page] }],
      ["page region that throws", { route: "/boom", url: "/boom", regions: [page] }],
      ["page region that redirects", { route: "/redirect", url: "/redirect", regions: [page] }],
      ["page region that is not found", { route: "/missing", url: "/missing", regions: [page] }],
      ["unknown page", { route: "/nowhere", url: "/nowhere", regions: [page] }],
    ];
    for (const [name, body] of cases) {
      it(`binary frames match NDJSON frames: ${name}`, async () => {
        const nd = await regions(body);
        const bin = await binaryRegions(body);
        expect(nd.res.headers.get("content-type")).toContain("application/x-ndjson");
        expect(bin.res.headers.get("content-type")).toBe(REGION_FRAMES_TYPE);
        expect(bin.frames.length).toBeGreaterThan(0);
        const key = (f: Record<string, any>) => `${f.type}:${f.id || ""}`;
        const sort = (l: Array<Record<string, any>>) => l.map(norm).sort((a, b) => key(a).localeCompare(key(b)));
        expect(sort(bin.frames)).toEqual(sort(nd.frames));
        expect(bin.frames[bin.frames.length - 1].type).toBe("end");
      });
    }

    it("still checks the secret before answering in binary", async () => {
      expect((await binaryRegions({ route: "/regions", url: "/regions" }, {})).res.status).toBe(403);
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
