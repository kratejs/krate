// Writes src/demos/sources.json: the text of every demo file listed in
// src/demos/registry.ts, which the demo pages show with the Code styling.
//
// A page cannot `import "./demo.tsx?raw"` next to a normal import of the same
// file (the compiler then drops that module's stylesheets), so the sources are
// collected here instead. `node scripts/build-demo-sources.mjs --check` fails
// when the committed file is stale.
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, extname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { loadDemos } from "./playground-examples.mjs";

const root = join(dirname(fileURLToPath(import.meta.url)), "../src/demos");
const { demos } = await loadDemos();
const langs = { ".tsx": "tsx", ".ts": "typescript", ".css": "css", ".json": "json" };

const out = {};
for (const d of demos) {
  out[d.slug] = d.files.map((name) => ({
    name,
    lang: langs[extname(name)] || "text",
    code: readFileSync(join(root, d.slug, name), "utf8"),
  }));
}
// Snippets shown on the homepage, kept as real component files.
const home = { "toggle-demo.tsx": "../home/toggle-demo.tsx", "fetch-demo.tsx": "../home/fetch-demo.tsx" };
out.home = Object.entries(home).map(([name, rel]) => ({ name, lang: "tsx", code: readFileSync(join(root, rel), "utf8") }));

const text = JSON.stringify(out, null, 2) + "\n";
const file = join(root, "sources.json");
if (process.argv.includes("--check")) {
  let current = "";
  try { current = readFileSync(file, "utf8"); } catch {}
  if (current !== text) {
    console.error("demo sources are stale: run `node scripts/build-demo-sources.mjs`");
    process.exit(1);
  }
} else {
  writeFileSync(file, text);
  console.log(`wrote ${Object.keys(out).length} demo sources`);
}
