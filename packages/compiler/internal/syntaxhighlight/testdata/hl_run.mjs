// Runs the runtime's tokenizer over files and prints the HTML for each, for the
// parity test. usage: node hl_run.mjs <repo-root> <bundle.json> <cases.json> <out.json>
// cases.json is [{id, lang, file, lines?}]; the TypeScript source is compiled
// in memory so the test does not depend on a built runtime.
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";

const [repo, bundlePath, casesPath, outPath] = process.argv.slice(2);
const require = createRequire(path.join(repo, "packages", "runtime", "package.json"));
const esbuild = require("esbuild");
const src = fs.readFileSync(path.join(repo, "packages", "runtime", "src", "highlight.ts"), "utf8");
const js = esbuild.transformSync(src, { loader: "ts", format: "esm", target: "es2022" }).code;
const mod = await import("data:text/javascript;base64," + Buffer.from(js).toString("base64"));

const bundle = JSON.parse(fs.readFileSync(bundlePath, "utf8"));
const names = new Map();
for (const [k, v] of Object.entries(bundle.keys)) {
  if (!names.has(v)) names.set(v, []);
  names.get(v).push(k);
}
for (const t of bundle.tables) mod.registerLanguage(t, names.get(t.n.toLowerCase()) || []);
if (bundle.fences) {
  const fences = bundle.fences;
  mod.setFenceResolver((n) => Object.prototype.hasOwnProperty.call(fences, n.trim().toLowerCase()));
}

const cases = JSON.parse(fs.readFileSync(casesPath, "utf8"));
const out = [];
for (const c of cases) {
  const text = fs.readFileSync(c.file, "utf8");
  const t0 = process.hrtime.bigint();
  let html;
  try {
    html = mod.highlight(mod.trimSpace(text), c.lang, c.lines || undefined, { maxLength: Infinity, maxLine: Infinity, budgetMs: Infinity });
  } catch (e) {
    html = null;
    out.push({ id: c.id, error: String((e && e.stack) || e) });
    continue;
  }
  out.push({ id: c.id, html, ms: Number(process.hrtime.bigint() - t0) / 1e6 });
}
fs.writeFileSync(outPath, JSON.stringify(out));
