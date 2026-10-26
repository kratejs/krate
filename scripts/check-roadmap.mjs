import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const roadmapPath = path.join(root, "packages/web/src/pages/roadmap.tsx");
const matrixPath = path.join(root, "packages/web/src/data/compat-matrix.json");
const roadmap = fs.readFileSync(roadmapPath, "utf8");
const matrix = JSON.parse(fs.readFileSync(matrixPath, "utf8"));

function fail(message) {
  console.error(`roadmap: ${message}`);
  process.exitCode = 1;
}

for (const id of ["R1", "R2", "R3", "R4", "R5"]) {
  if (!new RegExp(`id:\\s*"${id}"`).test(roadmap)) fail(`missing React phase ${id}`);
}
if (!/id:\s*"R5"[\s\S]*?status:\s*"v1 gate"/.test(roadmap)) {
  fail("R5 must remain the v1 gate");
}
if (!/id:\s*"R4"[\s\S]*?status:\s*"Complete"/.test(roadmap)) {
  fail("R4 must be marked Complete");
}

const planned = matrix.rows.filter((row) =>
  row.roadmap && [row.source, row.islands, row.ssr].includes("planned"),
);
if (planned.length > 0) {
  fail(`roadmap rows still planned: ${planned.map((row) => row.id).join(", ")}`);
}
if (!matrix.rows.some((row) => row.id === "ssr-islands" && row.ssr === "beta")) {
  fail("ssr-islands must be beta for the v1 gate");
}

if (!process.exitCode) console.log("roadmap: React phases and v1 gates are consistent");