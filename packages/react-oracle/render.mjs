// Renders pre-transformed fixture modules with react-dom/server.
// Usage: node render.mjs <dir-with-.mjs-fixtures>
// Prints a JSON object mapping fixture name to {html} or {error}.
import { readdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { renderToStaticMarkup } from "react-dom/server";
import * as React from "react";

const dir = resolve(process.argv[2]);
const out = {};
for (const f of readdirSync(dir).filter((n) => n.endsWith(".mjs")).sort()) {
  const name = f.slice(0, -4);
  try {
    const mod = await import(pathToFileURL(join(dir, f)).href);
    out[name] = { html: renderToStaticMarkup(React.createElement(mod.default)) };
  } catch (e) {
    out[name] = { error: String((e && e.message) || e) };
  }
}
process.stdout.write(JSON.stringify(out));
