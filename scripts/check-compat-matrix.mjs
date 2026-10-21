import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const matrixPath = path.join(root, "packages/web/src/data/compat-matrix.json");
const statuses = new Set(["stable", "beta", "partial", "planned", "n/a"]);

function walk(dir, out = []) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === "node_modules" || entry.name === "dist" || entry.name === ".krate") continue;
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(full, out);
    else if (entry.name.endsWith(".go")) out.push(full);
  }
  return out;
}

function goTests() {
  const names = new Set();
  const compiler = path.join(root, "packages/compiler");
  for (const file of walk(compiler)) {
    if (!file.endsWith("_test.go")) continue;
    const source = fs.readFileSync(file, "utf8");
    for (const match of source.matchAll(/^func (Test\w+)\(/gm)) names.add(match[1]);
  }
  return names;
}

function fail(message) {
  console.error(`compat-matrix: ${message}`);
  process.exitCode = 1;
}

const matrix = JSON.parse(fs.readFileSync(matrixPath, "utf8"));
if (!matrix.updated || Number.isNaN(Date.parse(matrix.updated))) {
  fail("updated must be an ISO date");
}
if (!Array.isArray(matrix.rows) || matrix.rows.length === 0) {
  fail("rows must be a non-empty array");
  process.exit();
}

const tests = goTests();
const seen = new Set();
let roadmapRows = 0;

for (const row of matrix.rows) {
  const label = row.id || row.area || "<unknown>";
  if (!row.id || !row.area || !row.target || !row.summary) {
    fail(`${label}: id, area, target, and summary are required`);
  }
  if (seen.has(row.id)) fail(`${label}: duplicate id`);
  seen.add(row.id);
  for (const key of ["source", "islands", "ssr"]) {
    if (!statuses.has(row[key])) fail(`${label}: invalid ${key} status ${row[key]}`);
  }
  if (!Array.isArray(row.tests)) fail(`${label}: tests must be an array`);
  if (row.source === "planned" || row.islands === "planned" || row.ssr === "planned") {
    // Planned surfaces may intentionally have no evidence yet.
  } else if (row.tests.length === 0) {
    fail(`${label}: supported rows need at least one test reference`);
  }
  for (const ref of row.tests) {
    if (ref.startsWith("Test")) {
      if (!tests.has(ref)) fail(`${label}: missing Go test ${ref}`);
    } else if (ref.startsWith("fixture:")) {
      const target = ref.slice("fixture:".length);
      if (!fs.existsSync(path.join(root, target))) fail(`${label}: missing fixture ${target}`);
    } else if (ref.startsWith("browser:")) {
      if (!ref.slice("browser:".length).startsWith("/")) fail(`${label}: browser ref ${ref} must start with /`);
    } else if (!ref.startsWith("ci:")) {
      fail(`${label}: unsupported test reference ${ref}`);
    }
  }
  if (row.roadmap) roadmapRows++;
}

if (roadmapRows === 0) fail("at least one row must be marked roadmap");
if (!process.exitCode) {
  console.log(`compat-matrix: ${matrix.rows.length} rows valid (${roadmapRows} roadmap rows)`);
}
