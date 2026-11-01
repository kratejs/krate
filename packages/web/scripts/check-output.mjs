// Post-build assertions for the docs output. Catches regressions that only show
// up in generated HTML (empty href attributes, missing social/manifest assets).
import { readFileSync, readdirSync, statSync, existsSync } from "node:fs";
import { join, relative } from "node:path";

const root = process.cwd();
const dist = join(root, "dist");

function walk(dir) {
  const out = [];
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    const st = statSync(path);
    if (st.isDirectory()) out.push(...walk(path));
    else out.push(path);
  }
  return out;
}

const failures = [];

if (!existsSync(dist)) {
  console.error("check-output: dist/ is missing; run the build first");
  process.exit(1);
}

const htmlFiles = walk(dist).filter((f) => f.endsWith(".html"));
for (const file of htmlFiles) {
  const html = readFileSync(file, "utf8");
  if (html.includes('href=""')) {
    failures.push(`${relative(root, file)}: contains href=""`);
  }
}

for (const asset of ["site.webmanifest", "og.png"]) {
  if (!existsSync(join(dist, asset))) {
    failures.push(`dist/${asset}: missing`);
  }
}

if (failures.length > 0) {
  console.error("check-output: failed");
  for (const failure of failures) console.error("  - " + failure);
  process.exit(1);
}

console.log(`check-output: ok (${htmlFiles.length} HTML files)`);
