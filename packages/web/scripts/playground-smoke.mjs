// Smoke test for the playground engine (public/playground/engine.js): loads
// the compiler wasm in a worker thread and builds a few projects.
//
//   node scripts/build-playground.mjs && node scripts/playground-smoke.mjs
//
// The wasm runs on an in-memory file system and never touches the real disk.

import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { Worker } from "node:worker_threads";

const here = dirname(fileURLToPath(import.meta.url));
if (!existsSync(join(here, "../public/playground/manifest.json"))) {
  console.error("playground-smoke: public/playground/manifest.json is missing; run `node scripts/build-playground.mjs` first");
  process.exit(process.env.KRATE_REQUIRE_PLAYGROUND === "1" ? 1 : 0);
}

const worker = new Worker(new URL("./playground-smoke-worker.mjs", import.meta.url));
const pending = new Map();
let nextId = 1;
const progress = [];
worker.on("message", (msg) => {
  if (msg.type === "worker-ready") return;
  if (msg.type === "progress") {
    progress.push(msg.stage);
    return;
  }
  const p = pending.get(msg.id);
  if (!p) return;
  pending.delete(msg.id);
  if (msg.ok) p.resolve(msg.result);
  else p.reject(new Error(msg.error.message));
});
worker.on("error", (err) => {
  console.error("worker crashed:", err);
  process.exit(1);
});

function request(type, payload = {}) {
  const id = nextId++;
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    worker.postMessage({ id, type, ...payload });
  });
}

const files = {
  "src/pages/index.tsx": `import { createSignal } from "@krate/runtime";
import { Badge } from "../components/badge";
import "../styles.css";

export default function Home() {
  const [count, setCount] = createSignal(0);
  return (
    <main class="wrap">
      <h1>Counter</h1>
      <Badge label="demo" />
      <button onClick={() => setCount(count() + 1)}>Count: {count()}</button>
    </main>
  );
}
`,
  "src/components/badge.tsx": `export function Badge(props: { label: string }) {
  return <span class="badge">{props.label}</span>;
}
`,
  "src/styles.css": `.wrap { font-family: sans-serif; padding: 2rem; }
.badge { background: #eee; padding: 2px 6px; border-radius: 4px; }
`,
  "src/pages/about.tsx": `export default function About() {
  return <p>About page</p>;
}
`,
};

let t = performance.now();
const init = await request("init");
console.log(`init: ${(performance.now() - t).toFixed(0)} ms (compiler ${init.version}; stages: ${[...new Set(progress)].join(", ")})`);
assert.ok(init.version, "the compiler reports a version");

// (a) A counter page with an imported component and a CSS file.
let res = await request("build", { files, opts: { minify: false } });
assert.equal(res.ok, true, "counter build ok; diagnostics: " + JSON.stringify(res.diagnostics));
const out = res.files;
const html = out["index.html"];
assert.ok(html, "index.html is in the output");
assert.match(html, /<h1[^>]*>Counter<\/h1>/, "the page is pre-rendered");
assert.match(html, /<span class="badge">demo<\/span>/, "the imported component is pre-rendered");
assert.match(html, /Count: /, "the signal text is pre-rendered");
const scripts = [...html.matchAll(/<script[^>]*\bsrc="([^"]+)"/g)].map((m) => m[1]);
const modulepreloads = [...html.matchAll(/<link[^>]*rel="modulepreload"[^>]*href="([^"]+)"/g)].map((m) => m[1]);
assert.ok(scripts.length > 0 || /<script[^>]*type="module"/.test(html), "the page has a hydration script");
const sheets = [...html.matchAll(/<link[^>]*rel="stylesheet"[^>]*href="([^"]+)"/g)].map((m) => m[1]);
assert.ok(sheets.length > 0, "the page links a stylesheet");
for (const ref of [...scripts, ...sheets, ...modulepreloads]) {
  const key = ref.replace(/^\//, "").replace(/^\.\//, "");
  assert.ok(key in out, `referenced file ${ref} exists in the output (have: ${Object.keys(out).join(", ")})`);
}
const css = Object.entries(out).find(([k]) => k.endsWith(".css"));
assert.ok(css && css[1].includes(".badge"), "the CSS file is emitted");
assert.ok(out["about/index.html"] || out["about.html"], "the second page is built");
console.log(`counter: ok, ${Object.keys(out).length} files, timings ${JSON.stringify(res.timingsMs)}`);

// (b) A syntax error comes back as diagnostics.
const broken = { ...files, "src/pages/index.tsx": `export default function Home() {\n  return <main>{</main>;\n}\n` };
res = await request("build", { files: broken, opts: {} });
assert.equal(res.ok, false, "a syntax error fails the build");
assert.ok(res.diagnostics.length > 0, "diagnostics are returned");
const d = res.diagnostics.find((x) => x.file && x.line > 0);
assert.ok(d, "a diagnostic has a file and line: " + JSON.stringify(res.diagnostics));
assert.match(d.file, /index\.tsx$/);
assert.equal(d.severity, "error");
assert.ok(d.message, "the diagnostic has a message");
console.log(`syntax error: ok -> ${d.file}:${d.line}:${d.col} ${d.message}${d.hint ? " (hint: " + d.hint + ")" : ""}`);

// The instance keeps working after a failed build.
res = await request("build", { files, opts: {} });
assert.equal(res.ok, true, "build after an error ok");

// (c) Many consecutive rebuilds, alternating edits.
const times = [];
for (let i = 0; i < 20; i++) {
  const edit = { ...files, "src/components/badge.tsx": files["src/components/badge.tsx"].replace("props.label", `props.label + " ${i}"`) };
  const src = i % 7 === 6 ? broken : edit;
  t = performance.now();
  res = await request("build", { files: src, opts: { minify: i % 2 === 0 } });
  times.push(performance.now() - t);
  if (src === broken) assert.equal(res.ok, false, `rebuild ${i} fails`);
  else {
    assert.equal(res.ok, true, `rebuild ${i}: ${JSON.stringify(res.diagnostics)}`);
    assert.match(res.files["index.html"], new RegExp(`demo ${i}`), `rebuild ${i} picked up the edit`);
  }
}
const sorted = [...times].sort((a, b) => a - b);
console.log(
  `20 rebuilds: ok. ms per build: first ${times[0].toFixed(0)}, median ${sorted[10].toFixed(0)}, min ${sorted[0].toFixed(0)}, max ${sorted[19].toFixed(0)}`,
);

await worker.terminate();
console.log("playground smoke test passed");
