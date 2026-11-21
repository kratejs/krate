#!/usr/bin/env node
// Benchmarks the same 3D scene on @krate/3d (Filament), @krate/three compiled
// and @krate/three with compile={false}. See README.md.
//
//   node run.mjs [run|report] [--runs 3] [--variants a,b] [--pages static,animated]
//                [--work DIR] [--krate BIN] [--shots DIR] [--frames 600] [--warmup 120]
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import http from "node:http";
import zlib from "node:zlib";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { VARIANTS, SCENE, page as scenePage, emptyPage } from "./scene.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, "../../..");

function parseArgs(argv) {
  const o = { cmd: "run", runs: 3, frames: 600, warmup: 120, pages: "static,animated", variants: Object.keys(VARIANTS).join(",") };
  const rest = [];
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a.startsWith("--")) {
      const k = a.slice(2);
      if (["skip-build", "gl-default"].includes(k)) o[k] = true;
      else o[k] = argv[++i];
    } else rest.push(a);
  }
  if (rest[0]) o.cmd = rest[0];
  for (const k of ["runs", "frames", "warmup"]) o[k] = Number(o[k]);
  return o;
}
const args = parseArgs(process.argv.slice(2));
const work = path.resolve(args.work || path.join(here, ".work"));
const shots = path.resolve(args.shots || path.join(work, "shots"));
const krateBin = args.krate || "krate";
const resultsPath = path.join(here, "results.json");

// Playwright writes its browser profile under the temp dir, which is RAM
// backed on some machines; keep it next to the work dir.
fs.mkdirSync(path.join(work, "tmp"), { recursive: true });
process.env.TMPDIR = path.join(work, "tmp");

const median = (xs) => {
  const s = xs.filter((x) => Number.isFinite(x)).sort((a, b) => a - b);
  if (!s.length) return null;
  const m = s.length >> 1;
  return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2;
};
const pct = (xs, p) => {
  const s = [...xs].sort((a, b) => a - b);
  return s[Math.min(s.length - 1, Math.floor((p / 100) * s.length))];
};
const round = (x, d = 2) => (typeof x === "number" ? Math.round(x * 10 ** d) / 10 ** d : x);

// ---------------------------------------------------------------- build

const buildInfo = {};

function setupApps(variants) {
  for (const v of variants) {
    const d = path.join(work, v);
    fs.rmSync(d, { recursive: true, force: true });
    fs.mkdirSync(path.join(d, "src", "pages"), { recursive: true });
    const w = (f, s) => fs.writeFileSync(path.join(d, "src", "pages", f), s);
    w("animated.tsx", scenePage(v, true));
    w("static.tsx", scenePage(v, false));
    w("empty.tsx", emptyPage(v));
    fs.writeFileSync(path.join(d, "package.json"), JSON.stringify({ name: "bench-" + v, private: true, type: "module" }));
    fs.mkdirSync(path.join(d, "node_modules", "@krate"), { recursive: true });
    const link = (name, target) => fs.symlinkSync(target, path.join(d, "node_modules", name));
    link("@krate/runtime", path.join(repo, "packages", "runtime"));
    if (v === "filament") {
      link("@krate/3d", path.join(repo, "packages", "3d"));
      link("filament", path.join(repo, "packages", "3d", "node_modules", "filament"));
    } else {
      link("@krate/three", path.join(repo, "packages", "three"));
      link("three", path.join(repo, "packages", "three", "node_modules", "three"));
    }
  }
}

function buildApps(variants) {
  for (const v of variants) {
    const d = path.join(work, v);
    const t0 = Date.now();
    // Production build; minify is the default for `krate build`.
    const out = execFileSync(krateBin, ["build", "."], { cwd: d, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
    const fallback = /KR8701/.test(out);
    buildInfo[v] = { buildMs: Date.now() - t0, canvasFallbackWarning: fallback };
    if (v === "filament" && fallback) console.warn(`warning: ${v} hit KR8701 (runtime fallback); the compiled path is not being measured`);
  }
}

// ---------------------------------------------------------------- server

const MIME = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".wasm": "application/wasm", ".json": "application/json" };

function serve(root) {
  const requests = new Set();
  const server = http.createServer((req, res) => {
    let p = decodeURIComponent(req.url.split("?")[0]);
    if (p.endsWith("/")) p += "index.html";
    else if (!path.extname(p)) p += "/index.html";
    const file = path.join(root, p);
    if (!file.startsWith(root)) {
      res.statusCode = 403;
      return res.end();
    }
    fs.readFile(file, (err, body) => {
      if (err) {
        res.statusCode = 404;
        return res.end("not found");
      }
      requests.add(p);
      res.setHeader("content-type", MIME[path.extname(p)] || "application/octet-stream");
      res.setHeader("cache-control", "no-store");
      res.end(body);
    });
  });
  return new Promise((resolve) => server.listen(0, "127.0.0.1", () => resolve({ server, requests, base: `http://127.0.0.1:${server.address().port}` })));
}

// ---------------------------------------------------------------- processes

function procs(rootPattern) {
  // The browser process carries --user-data-dir under our work dir; its
  // children (zygote, gpu, renderer, utility) are found through ppid.
  const all = new Map();
  for (const pid of fs.readdirSync("/proc").filter((x) => /^\d+$/.test(x))) {
    try {
      const stat = fs.readFileSync(`/proc/${pid}/stat`, "utf8");
      const ppid = Number(stat.slice(stat.lastIndexOf(")") + 2).split(" ")[1]);
      const cmd = fs.readFileSync(`/proc/${pid}/cmdline`, "utf8").split("\0");
      all.set(Number(pid), { ppid, cmd });
    } catch {}
  }
  const mine = new Set([...all].filter(([, v]) => v.cmd.some((c) => c.includes(rootPattern))).map(([k]) => k));
  for (let grew = true; grew; ) {
    grew = false;
    for (const [pid, v] of all) if (!mine.has(pid) && mine.has(v.ppid)) (mine.add(pid), (grew = true));
  }
  return [...mine].map((pid) => {
    // Forked children rewrite their argv into one space-joined string.
    const t = all.get(pid).cmd.join(" ").match(/--type=([\w-]+)/);
    return { pid, type: t ? t[1] : "browser" };
  });
}

function readProc(pid) {
  try {
    const status = fs.readFileSync(`/proc/${pid}/status`, "utf8");
    const kb = (k) => Number((status.match(new RegExp(`^${k}:\\s+(\\d+)`, "m")) || [])[1] || 0);
    const stat = fs.readFileSync(`/proc/${pid}/stat`, "utf8");
    const f = stat.slice(stat.lastIndexOf(")") + 2).split(" ");
    const cpuTicks = Number(f[11]) + Number(f[12]);
    let pss = 0;
    try {
      pss = Number((fs.readFileSync(`/proc/${pid}/smaps_rollup`, "utf8").match(/^Pss:\s+(\d+)/m) || [])[1] || 0);
    } catch {}
    return { rssKB: kb("VmRSS"), hwmKB: kb("VmHWM"), pssKB: pss, cpuSec: cpuTicks / 100 };
  } catch {
    return null;
  }
}

function procSnapshot() {
  const cats = {};
  for (const p of procs(path.join(work, "tmp"))) {
    const r = readProc(p.pid);
    if (!r) continue;
    const c = (cats[p.type === "gpu-process" ? "gpu" : p.type === "renderer" ? "renderer" : p.type === "browser" ? "browser" : "other"] ||= { rssKB: 0, hwmKB: 0, pssKB: 0, cpuSec: 0 });
    for (const k of Object.keys(r)) c[k] += r[k];
  }
  return cats;
}

// ---------------------------------------------------------------- browser

const GL_ARGS = ["--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--ignore-gpu-blocklist", "--disable-gpu-vsync", "--disable-frame-rate-limit"];

// Per-rAF timestamps, recorded in a preallocated array so the probe itself
// allocates nothing in the measured window.
const RAF_PROBE = `
(() => {
  const buf = new Float64Array(400000);
  window.__raf = buf;
  window.__rafN = 0;
  // gl.finish() at the start of each frame waits for the GPU to complete the
  // previous frame, so the rAF interval is the real cost of a frame rather
  // than how fast commands can be queued (identical for all variants).
  let gl = null;
  const getContext = HTMLCanvasElement.prototype.getContext;
  HTMLCanvasElement.prototype.getContext = function (type, ...a) {
    const ctx = getContext.call(this, type, ...a);
    if (ctx && !gl && /webgl/.test(type)) gl = ctx;
    return ctx;
  };
  const loop = () => {
    if (gl) gl.finish();
    if (window.__rafN < buf.length) buf[window.__rafN++] = performance.now();
    requestAnimationFrame(loop);
  };
  requestAnimationFrame(loop);
  const M = WebAssembly.Memory;
  window.__mems = [];
  WebAssembly.Memory = function (...a) { const m = new M(...a); window.__mems.push(m); return m; };
  WebAssembly.Memory.prototype = M.prototype;
})();
`;

// Counts WebGL calls on both context prototypes, identically for all variants.
const GL_PROBE = `
(() => {
  const DRAW = new Set(["drawElements", "drawArrays", "drawElementsInstanced", "drawArraysInstanced", "drawRangeElements", "multiDrawArraysWEBGL"]);
  const c = { calls: 0, draws: 0, byName: {} };
  window.__gl = c;
  for (const proto of [WebGL2RenderingContext.prototype, WebGLRenderingContext.prototype]) {
    for (const k of Object.getOwnPropertyNames(proto)) {
      const d = Object.getOwnPropertyDescriptor(proto, k);
      if (k === "constructor" || typeof d.value !== "function") continue;
      const orig = d.value;
      const isDraw = DRAW.has(k);
      proto[k] = function (...a) {
        c.calls++;
        if (isDraw) { c.draws++; c.byName[k] = (c.byName[k] || 0) + 1; }
        return orig.apply(this, a);
      };
    }
  }
})();
`;

let chromium;
// A persistent context with its own profile dir under the work dir: Playwright
// would otherwise put the profile in /tmp, and the dir name is how this
// script finds its own browser processes.
async function launch() {
  if (!chromium) ({ chromium } = await import("playwright"));
  const dir = fs.mkdtempSync(path.join(work, "tmp", "profile-"));
  const ctx = await chromium.launchPersistentContext(dir, {
    headless: true,
    args: args["gl-default"] ? [] : GL_ARGS,
    viewport: { width: SCENE.width, height: SCENE.height },
    deviceScaleFactor: 1,
  });
  return {
    ctx,
    version: ctx.browser().version(),
    async close() {
      await ctx.close();
      fs.rmSync(dir, { recursive: true, force: true });
    },
  };
}

async function freshPage(browser, init) {
  const ctx = browser.ctx;
  const page = ctx.pages()[0] || (await ctx.newPage());
  const errors = [];
  page.on("pageerror", (e) => errors.push("pageerror: " + e.message));
  page.on("console", (m) => {
    if (m.type() === "error") errors.push("console.error: " + m.text());
  });
  for (const s of init) await page.addInitScript(s);
  return { ctx, page, errors };
}

// Waits for n frames and for at least `sec` seconds, so a variant that renders
// very fast still gets a window long enough for CPU-time sampling.
async function waitWindow(page, n, sec, timeout) {
  const until = (await page.evaluate(() => performance.now())) + sec * 1000;
  await page.waitForFunction(([n, until]) => window.__rafN >= n && performance.now() >= until, [n, until], { polling: 200, timeout });
}

// One run: startup, frame cost and memory, with no GL instrumentation.
async function measure(v, pageName, base) {
  const browser = await launch();
  try {
    const { page, errors, ctx } = await freshPage(browser, [RAF_PROBE]);
    const cdp = await ctx.newCDPSession(page);
    await cdp.send("Performance.enable");
    await cdp.send("HeapProfiler.enable");
    await page.goto(`${base}/${pageName}/`, { waitUntil: "load" });
    await page.waitForFunction(() => window.__firstFrame, null, { polling: 100, timeout: 120000 });
    const startup = await page.evaluate(() => {
      const nav = performance.getEntriesByType("navigation")[0];
      const res = performance.getEntriesByType("resource");
      const engineRes = res.filter((r) => /filament|\.filamat/.test(r.name));
      return {
        domContentLoadedMs: nav.domContentLoadedEventEnd,
        loadMs: nav.loadEventEnd,
        engineAssetsEndMs: engineRes.length ? Math.max(...engineRes.map((r) => r.responseEnd)) : null,
        createdMs: window.__created,
        readyMs: window.__ready,
        firstFrameMs: window.__firstFrame,
      };
    });
    const n0 = await page.evaluate(() => window.__rafN);
    await waitWindow(page, n0 + args.warmup, 2, 600000);
    const metrics = async () => Object.fromEntries((await cdp.send("Performance.getMetrics")).metrics.map((m) => [m.name, m.value]));
    const afterWarm = { m: await metrics(), p: procSnapshot() };
    const a = await page.evaluate(() => ({ n: window.__rafN, ef: window.__engineFrames, t: performance.now() }));
    const m0 = afterWarm.m;
    const p0 = afterWarm.p;
    await waitWindow(page, a.n + args.frames, 5, 900000);
    const b = await page.evaluate(() => ({ n: window.__rafN, ef: window.__engineFrames, t: performance.now() }));
    const m1 = await metrics();
    const p1 = procSnapshot();
    const frames = b.n - a.n;
    const iv = await page.evaluate(([i, j]) => Array.from(window.__raf.slice(i, j)), [a.n, b.n]);
    const dts = iv.slice(1).map((x, i) => x - iv[i]);
    const wallSec = (b.t - a.t) / 1000;
    const perFrame = (k) => ((m1[k] - m0[k]) * 1000) / frames;
    const frame = {
      frames,
      engineFrames: b.ef - a.ef,
      fps: frames / wallSec,
      intervalMedianMs: median(dts),
      intervalP95Ms: pct(dts, 95),
      intervalMeanMs: dts.reduce((x, y) => x + y, 0) / dts.length,
      mainThreadTaskMsPerFrame: perFrame("TaskDuration"),
      scriptMsPerFrame: perFrame("ScriptDuration"),
      layoutMsPerFrame: perFrame("LayoutDuration"),
      rendererCpuMsPerFrame: ((p1.renderer.cpuSec - p0.renderer.cpuSec) * 1000) / frames,
      gpuProcessCpuMsPerFrame: ((p1.gpu.cpuSec - p0.gpu.cpuSec) * 1000) / frames,
    };
    // Memory: before GC (as it stands after warmup + measurement), then after GC.
    const preGc = { heapUsedMB: m1.JSHeapUsedSize / 2 ** 20, heapTotalMB: m1.JSHeapTotalSize / 2 ** 20 };
    await cdp.send("HeapProfiler.collectGarbage");
    await cdp.send("HeapProfiler.collectGarbage");
    const m2 = await metrics();
    const p2 = procSnapshot();
    const wasm = await page.evaluate(() => {
      const mem = window.Filament && window.Filament.HEAPU8 ? window.Filament.HEAPU8.buffer.byteLength : null;
      const patched = (window.__mems || []).reduce((s, m) => s + m.buffer.byteLength, 0);
      return { filamentHeapBytes: mem, wasmMemoriesBytes: patched };
    });
    const mb = (x) => x / 2 ** 20;
    const memory = {
      jsHeapUsedMBBeforeGC: preGc.heapUsedMB,
      jsHeapTotalMBBeforeGC: preGc.heapTotalMB,
      jsHeapUsedMB: mb(m2.JSHeapUsedSize),
      jsHeapTotalMB: mb(m2.JSHeapTotalSize),
      wasmMemoryMB: wasm.filamentHeapBytes != null ? mb(wasm.filamentHeapBytes) : wasm.wasmMemoriesBytes ? mb(wasm.wasmMemoriesBytes) : 0,
      rendererRssMB: p2.renderer.rssKB / 1024,
      rendererPssMB: p2.renderer.pssKB / 1024,
      rendererHwmMB: p2.renderer.hwmKB / 1024,
      gpuRssMB: p2.gpu ? p2.gpu.rssKB / 1024 : 0,
      gpuPssMB: p2.gpu ? p2.gpu.pssKB / 1024 : 0,
      gpuHwmMB: p2.gpu ? p2.gpu.hwmKB / 1024 : 0,
      browserRssMB: p2.browser ? p2.browser.rssKB / 1024 : 0,
      totalRssMB: Object.values(p2).reduce((s, c) => s + c.rssKB, 0) / 1024,
      totalPssMB: Object.values(p2).reduce((s, c) => s + c.pssKB, 0) / 1024,
    };
    return { startup, frame, memory, errors: errors.slice(0, 5) };
  } finally {
    await browser.close();
  }
}

// Draw/GL calls per frame (instrumented), the GL renderer string, canvas size
// and screenshots at a frozen animation time.
async function countsAndShots(v, pageName, base, shotPath) {
  const browser = await launch();
  try {
    const { page, errors } = await freshPage(browser, [RAF_PROBE, GL_PROBE]);
    await page.goto(`${base}/${pageName}/`, { waitUntil: "load" });
    await page.waitForFunction(() => window.__firstFrame, null, { polling: 100, timeout: 120000 });
    const info = await page.evaluate(() => {
      const c = document.createElement("canvas");
      const g = c.getContext("webgl2");
      const e = g && g.getExtension("WEBGL_debug_renderer_info");
      const cv = document.querySelector("canvas#scene, #scene canvas, canvas");
      return { renderer: e ? g.getParameter(e.UNMASKED_RENDERER_WEBGL) : null, vendor: e ? g.getParameter(e.UNMASKED_VENDOR_WEBGL) : null, canvas: [cv.width, cv.height] };
    });
    const e0 = await page.evaluate(() => window.__engineFrames);
    await page.waitForFunction((n) => window.__engineFrames >= n, e0 + args.warmup, { polling: 200, timeout: 600000 });
    const snap = () => page.evaluate(() => ({ ef: window.__engineFrames, calls: window.__gl.calls, draws: window.__gl.draws, byName: { ...window.__gl.byName } }));
    const a = await snap();
    await page.waitForFunction((n) => window.__engineFrames >= n, a.ef + 60, { polling: 200, timeout: 600000 });
    const b = await snap();
    const fr = b.ef - a.ef;
    const byName = {};
    for (const k of Object.keys(b.byName)) byName[k] = round((b.byName[k] - (a.byName[k] || 0)) / fr);
    const counts = { frames: fr, glCallsPerFrame: round((b.calls - a.calls) / fr), drawCallsPerFrame: round((b.draws - a.draws) / fr), drawsByName: byName };
    // Freeze the animation at a fixed time, let a few frames render, shoot.
    await page.evaluate(() => window.__freeze(0.5));
    const f0 = await page.evaluate(() => window.__engineFrames);
    await page.waitForFunction((n) => window.__engineFrames >= n, f0 + 6, { polling: 100, timeout: 120000 });
    fs.mkdirSync(path.dirname(shotPath), { recursive: true });
    await page.screenshot({ path: shotPath, clip: { x: 0, y: 0, width: SCENE.width, height: SCENE.height }, timeout: 180000 });
    return { info, counts, errors: errors.slice(0, 5) };
  } finally {
    await browser.close();
  }
}

// ---------------------------------------------------------------- payload

function fileSizes(dist, urlPath) {
  const buf = fs.readFileSync(path.join(dist, urlPath));
  return { path: urlPath, bytes: buf.length, gzip: zlib.gzipSync(buf, { level: 9 }).length, brotli: zlib.brotliCompressSync(buf).length };
}

function kind(p) {
  if (/\/assets\/3d\/materials-/.test(p)) return "materials";
  if (/\.wasm$/.test(p)) return "wasm";
  if (/\/assets\/3d\/filament-.*\.js$/.test(p)) return "engineJs";
  if (/\.js$/.test(p)) return "appJs";
  if (/\.html$/.test(p)) return "html";
  return "other";
}


async function pagePayload(dist, pageName, base, requests) {
  // Load the page in a browser with the server's request log reset, so the
  // list is exactly what the page fetches (the engine assets of filament are
  // requested at runtime, not named in the HTML).
  requests.clear();
  const browser = await launch();
  try {
    const { page } = await freshPage(browser, [RAF_PROBE]);
    await page.goto(`${base}/${pageName}/`, { waitUntil: "load" });
    if (pageName !== "empty") await page.waitForFunction(() => window.__firstFrame, null, { polling: 100, timeout: 120000 });
    else await page.waitForTimeout(3000);
  } finally {
    await browser.close();
  }
  const files = [...requests].filter((p) => fs.existsSync(path.join(dist, p))).map((p) => ({ ...fileSizes(dist, p), kind: kind(p) }));
  const sum = (k, f) => files.filter((x) => (f ? f(x) : true)).reduce((s, x) => s + x[k], 0);
  const by = (kd) => ({ bytes: sum("bytes", (x) => x.kind === kd), gzip: sum("gzip", (x) => x.kind === kd), brotli: sum("brotli", (x) => x.kind === kd) });
  return {
    files,
    appJs: by("appJs"),
    engineJs: by("engineJs"),
    wasm: by("wasm"),
    materials: by("materials"),
    html: by("html"),
    total: { bytes: sum("bytes"), gzip: sum("gzip"), brotli: sum("brotli") },
  };
}

// ---------------------------------------------------------------- run

function flatten(o, prefix = "", out = {}) {
  for (const [k, v] of Object.entries(o)) {
    if (v && typeof v === "object" && !Array.isArray(v)) flatten(v, prefix + k + ".", out);
    else if (typeof v === "number") out[prefix + k] = v;
  }
  return out;
}
function setPath(o, p, v) {
  const parts = p.split(".");
  let cur = o;
  for (const k of parts.slice(0, -1)) cur = cur[k] ||= {};
  cur[parts.at(-1)] = v;
}
function medianOf(runs) {
  const keys = new Set(runs.flatMap((r) => Object.keys(flatten(r))));
  const out = {};
  for (const k of keys) setPath(out, k, round(median(runs.map((r) => flatten(r)[k])), 3));
  return out;
}

function machine() {
  let chromiumVersion = null;
  const cpus = os.cpus();
  let gitSha = "";
  try {
    gitSha = execFileSync("git", ["rev-parse", "--short", "HEAD"], { cwd: repo, encoding: "utf8" }).trim();
  } catch {}
  return { cpu: cpus[0].model, cores: cpus.length, ramGB: Math.round(os.totalmem() / 2 ** 30), loadavg: os.loadavg(), node: process.version, kernel: os.release(), gitSha, chromiumVersion };
}

async function run() {
  const variants = args.variants.split(",");
  const pages = args.pages.split(",");
  if (!args["skip-build"]) {
    setupApps(variants);
    buildApps(variants);
  }
  const prev = fs.existsSync(resultsPath) && args.merge ? JSON.parse(fs.readFileSync(resultsPath, "utf8")) : {};
  const result = { machine: machine(), config: { runs: args.runs, warmupFrames: args.warmup, measuredFrames: args.frames, scene: SCENE, glArgs: args["gl-default"] ? [] : GL_ARGS }, variants: prev.variants || {} };
  { const b = await launch(); result.machine.chromiumVersion = b.version; await b.close(); }
  const servers = {};
  for (const v of variants) servers[v] = await serve(path.join(work, v, "dist"));
  try {
    for (const v of variants) {
      result.variants[v] ||= { pages: {} };
      result.variants[v].build = buildInfo[v];
      for (const p of [...pages, "empty"]) {
        const s = servers[v];
        const entry = (result.variants[v].pages[p] = { runs: [] });
        entry.payload = await pagePayload(path.join(work, v, "dist"), p, s.base, s.requests);
        if (p === "empty") continue;
        const c = await countsAndShots(v, p, s.base, path.join(shots, `${v}-${p}.png`));
        entry.glRenderer = c.info.renderer;
        entry.canvas = c.info.canvas;
        entry.counts = c.counts;
        entry.errors = c.errors;
      }
    }
    // Interleave variants across rounds so drift in machine load spreads.
    for (let r = 0; r < args.runs; r++) {
      for (const p of pages) {
        for (const v of variants) {
          const s = servers[v];
          console.log(`run ${r + 1}/${args.runs} ${v} ${p}`);
          const m = await measure(v, p, s.base);
          result.variants[v].pages[p].runs.push(m);
          fs.writeFileSync(resultsPath + ".partial", JSON.stringify(result, null, 2));
        }
      }
    }
  } finally {
    for (const s of Object.values(servers)) s.server.close();
  }
  for (const v of variants)
    for (const p of pages) {
      const e = result.variants[v].pages[p];
      e.median = medianOf(e.runs.map(({ errors, ...rest }) => rest));
      e.runErrors = e.runs.flatMap((x) => x.errors);
    }
  fs.writeFileSync(resultsPath, JSON.stringify(result, null, 2));
  fs.rmSync(resultsPath + ".partial", { force: true });
  console.log("wrote", resultsPath);
}

// ---------------------------------------------------------------- report

function report() {
  const R = JSON.parse(fs.readFileSync(resultsPath, "utf8"));
  const vs = Object.keys(R.variants);
  const kb = (x) => (x / 1024).toFixed(1);
  const lines = [];
  const row = (label, f) => `| ${label} | ${vs.map((v) => f(v)).join(" | ")} |`;
  const head = `| | ${vs.join(" | ")} |\n|---|${vs.map(() => "---:").join("|")}|`;
  for (const p of ["static", "animated"]) {
    const g = (v, path) => path.split(".").reduce((o, k) => (o == null ? o : o[k]), R.variants[v].pages[p].median);
    const f = (path, d = 1) => (v) => { const x = g(v, path); return x == null ? "n/a" : x.toFixed(d); };
    lines.push(`### ${p} scene\n`, head);
    lines.push(row("startup: first frame (ms)", f("startup.firstFrameMs", 0)));
    lines.push(row("startup: engine ready / scene created (ms)", f("startup.readyMs", 0)));
    lines.push(row("startup: engine assets fetched (ms)", f("startup.engineAssetsEndMs", 0)));
    lines.push(row("FPS", f("frame.fps")));
    lines.push(row("rAF interval mean (ms)", f("frame.intervalMeanMs")));
    lines.push(row("rAF interval median (ms)", f("frame.intervalMedianMs")));
    lines.push(row("rAF interval p95 (ms)", f("frame.intervalP95Ms")));
    lines.push(row("main-thread task (ms/frame)", f("frame.mainThreadTaskMsPerFrame")));
    lines.push(row("main-thread script (ms/frame)", f("frame.scriptMsPerFrame")));
    lines.push(row("renderer process CPU (ms/frame)", f("frame.rendererCpuMsPerFrame")));
    lines.push(row("GPU process CPU (ms/frame)", f("frame.gpuProcessCpuMsPerFrame")));
    lines.push(row("draw calls / frame", (v) => String(R.variants[v].pages[p].counts.drawCallsPerFrame)));
    lines.push(row("GL calls / frame", (v) => String(R.variants[v].pages[p].counts.glCallsPerFrame)));
    lines.push(row("JS heap used after GC (MB)", f("memory.jsHeapUsedMB")));
    lines.push(row("JS heap total after GC (MB)", f("memory.jsHeapTotalMB")));
    lines.push(row("wasm memory (MB)", f("memory.wasmMemoryMB")));
    lines.push(row("renderer RSS (MB)", f("memory.rendererRssMB", 0)));
    lines.push(row("GPU process RSS (MB)", f("memory.gpuRssMB", 0)));
    lines.push(row("browser process RSS (MB)", f("memory.browserRssMB", 0)));
    lines.push(row("total RSS, all processes (MB)", f("memory.totalRssMB", 0)));
    lines.push(row("total PSS, all processes (MB)", f("memory.totalPssMB", 0)), "");
  }
  lines.push("### Payload (page `animated`; the static page differs by a few KB)\n", head);
  const pl = (v, k, m) => R.variants[v].pages.animated.payload[k][m];
  for (const m of ["bytes", "gzip", "brotli"]) {
    lines.push(row(`app + runtime JS, ${m} (KB)`, (v) => kb(pl(v, "appJs", m))));
  }
  const emp = (v, m) => R.variants[v].pages.empty.payload.appJs[m];
  lines.push(row("of which empty-Canvas baseline JS, gzip (KB)", (v) => kb(emp(v, "gzip"))));
  lines.push(row("engine JS (filament.js), gzip (KB)", (v) => kb(pl(v, "engineJs", "gzip"))));
  lines.push(row("wasm, gzip (KB)", (v) => kb(pl(v, "wasm", "gzip"))));
  lines.push(row("wasm, raw (KB)", (v) => kb(pl(v, "wasm", "bytes"))));
  lines.push(row("materials (.filamat) fetched, raw (KB)", (v) => kb(pl(v, "materials", "bytes"))));
  lines.push(row("total transferred, raw (KB)", (v) => kb(R.variants[v].pages.animated.payload.total.bytes)));
  lines.push(row("total transferred, gzip (KB)", (v) => kb(R.variants[v].pages.animated.payload.total.gzip)));
  lines.push(row("total transferred, brotli (KB)", (v) => kb(R.variants[v].pages.animated.payload.total.brotli)), "");
  const tables = lines.join("\n");
  const notesPath = path.join(here, "notes.md");
  const notes = fs.existsSync(notesPath) ? fs.readFileSync(notesPath, "utf8") : "";
  const m = R.machine;
  const gl = R.variants[vs[0]].pages.static.glRenderer;
  const md = `# 3D benchmark results

Generated by \`node run.mjs report\` from \`results.json\` plus the hand-written \`notes.md\`.

## Machine

- CPU: ${m.cpu}, ${m.cores} logical cores, ${m.ramGB} GB RAM
- OS: Linux ${m.kernel}, Node ${m.node}, Chromium ${m.chromiumVersion} (Playwright, headless)
- WebGL renderer: \`${gl}\`
- Branch commit: ${m.gitSha}; load average at start: ${m.loadavg.map((x) => x.toFixed(2)).join(" ")}
- Runs: median of ${R.config.runs}, ${R.config.warmupFrames} warmup frames, ${R.config.measuredFrames} measured frames; fresh browser per run, one at a time

${notes}

## Results (median of ${R.config.runs})

${tables}
`;
  fs.writeFileSync(path.join(here, "RESULTS.md"), md);
  console.log("wrote RESULTS.md");
}

if (args.cmd === "report") report();
else await run();
