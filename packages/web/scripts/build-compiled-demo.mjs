// Builds the "compiled output" demo's component with the playground engine and
// writes what it emitted to src/demos/compiled/output.json (committed), so the
// demo page can show the real HTML and JavaScript next to the TSX.
//
// Needs public/playground/krate.wasm.gz (node scripts/build-playground.mjs).
// Without it the script keeps the committed file and exits 0.

import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";
import { Worker } from "node:worker_threads";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "..");
const outFile = join(root, "src/demos/compiled/output.json");

if (!existsSync(join(root, "public/playground/manifest.json"))) {
  console.warn("[compiled-demo] no playground engine; keeping " + outFile);
  process.exit(0);
}

const worker = new Worker(new URL("./playground-smoke-worker.mjs", import.meta.url));
const pending = new Map();
let nextId = 1;
worker.on("message", (msg) => {
  if (msg.type === "progress" || msg.type === "worker-ready") return;
  const p = pending.get(msg.id);
  if (!p) return;
  pending.delete(msg.id);
  if (msg.ok) p.resolve(msg.result);
  else p.reject(new Error(msg.error?.message || JSON.stringify(msg).slice(0, 400)));
});
const request = (type, payload = {}) =>
  new Promise((resolve, reject) => {
    const id = nextId++;
    pending.set(id, { resolve, reject });
    worker.postMessage({ id, type, ...payload });
  });

const demo = readFileSync(join(root, "src/demos/compiled/demo.tsx"), "utf8");
const css = readFileSync(join(root, "src/demos/compiled/demo.css"), "utf8");
const files = {
  "src/pages/index.tsx": `import Demo from "../components/demo";\n\nexport default function Home() {\n  return (\n    <main>\n      <Demo />\n    </main>\n  );\n}\n`,
  "src/components/demo.tsx": demo,
  "src/components/demo.css": css,
};

// Indents block elements; a block that holds only inline content stays on one line.
const INLINE = new Set(["strong", "em", "span", "a", "code", "b", "i"]);
const VOID = new Set(["br", "hr", "img", "input", "meta", "link"]);
function pretty(markup) {
  const root = { children: [] };
  const stack = [root];
  for (const tok of markup.match(/<[^>]+>|[^<]+/g)) {
    const top = stack[stack.length - 1];
    if (tok.startsWith("</")) {
      stack.pop();
    } else if (tok.startsWith("<") && !tok.startsWith("<!--")) {
      const name = tok.match(/^<([a-z0-9]+)/i)[1].toLowerCase();
      const node = { name, open: tok, children: [], close: "</" + name + ">" };
      top.children.push(node);
      if (!VOID.has(name) && !tok.endsWith("/>")) stack.push(node);
    } else {
      top.children.push({ text: tok });
    }
  }
  const flat = (n) => (n.text ?? n.open + n.children.map(flat).join("") + (n.close || ""));
  const hasBlock = (n) => n.children.some((c) => c.name && (!INLINE.has(c.name) || hasBlock(c)));
  const out = [];
  const walk = (n, depth) => {
    const pad = "  ".repeat(depth);
    if (n.text !== undefined || !hasBlock(n)) return out.push(pad + flat(n));
    out.push(pad + n.open);
    for (const c of n.children) walk(c, depth + 1);
    out.push(pad + n.close);
  };
  for (const c of root.children) walk(c, 0);
  return out.join("\n");
}

try {
  await request("init");
  const res = await request("build", { files, opts: { minify: false } });
  if (!res.ok) throw new Error("build failed: " + JSON.stringify(res.diagnostics));
  const html = res.files["index.html"];
  const main = html.match(/<main[\s\S]*?<\/main>/);
  const scriptSrc = [...html.matchAll(/<script[^>]*\bsrc="([^"]+)"/g)].map((m) => m[1].replace(/^\//, ""));
  const pageJs = scriptSrc.find((s) => /^index\.[a-z0-9]+\.js$/.test(s)) || scriptSrc[scriptSrc.length - 1];
  const js = res.files[pageJs];
  if (!main || !js) throw new Error("unexpected build output: " + Object.keys(res.files).join(", "));
  const gz = (s) => gzipSync(s).length;
  const out = {
    html: pretty(main[0]),
    htmlBytes: Buffer.byteLength(main[0]),
    js,
    jsBytes: Buffer.byteLength(js),
    jsGzipBytes: gz(js),
    runtimeGzipBytes: Object.entries(res.files)
      .filter(([k]) => /^chunks\/.*\.js$/.test(k))
      .reduce((n, [, v]) => n + gz(v), 0),
  };
  writeFileSync(outFile, JSON.stringify(out, null, 2) + "\n");
  console.log(`[compiled-demo] wrote ${outFile} (html ${out.htmlBytes} B, page js ${out.jsGzipBytes} B gzip)`);
} finally {
  await worker.terminate();
}
