#!/usr/bin/env node
/**
 * Release-readiness gate for the v1 contract. This is intentionally stricter
 * than any individual check: it proves the version matrix is exercised, the
 * performance budgets are configured, diagnostic docs and codemods are
 * present, and the migration verification suite still exists.
 */
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const read = (rel) => fs.readFileSync(path.join(root, rel), "utf8");
const failures = [];
const fail = (message) => failures.push(message);

const ci = read(".github/workflows/ci.yml");
const release = read(".github/workflows/release.yml");
const versionDoc = read("packages/web/src/content/docs/guides/version-matrix.md");
const webConfig = read("packages/web/krate.config.ts");
const exampleConfig = read("examples/krate.config.ts");
const matrix = JSON.parse(read("packages/web/src/data/compat-matrix.json"));

function matrixValues(key) {
  const match = ci.match(new RegExp(`${key}:\\s*\\[([^\\]]+)\\]`));
  if (!match) return [];
  return match[1].split(",").map((v) => v.trim().replace(/^["']|["']$/g, "")).filter(Boolean);
}

// 1. The version matrix page must claim only versions CI actually exercises.
const nodeVersions = matrixValues("node");
const preactVersions = matrixValues("preact");
const browsers = matrixValues("browser");
if (nodeVersions.length === 0) fail("CI has no Node version matrix");
if (preactVersions.length === 0) fail("CI has no Preact version matrix");
if (browsers.length === 0) fail("CI has no browser matrix");
const versionDocLower = versionDoc.toLowerCase();
for (const v of [...nodeVersions, ...preactVersions, ...browsers]) {
  if (!versionDocLower.includes(v.toLowerCase())) fail(`version-matrix.md does not mention CI-tested ${v}`);
}
for (const runtime of ["Node.js", "Bun", "Deno", "Chromium", "Firefox", "WebKit", "pnpm 11.9.0"]) {
  if (!versionDoc.includes(runtime)) fail(`version-matrix.md is missing ${runtime}`);
}
if (!/Go\s*\|\s*The version in `packages\/compiler\/go.mod`/.test(versionDoc)) {
  fail("version-matrix.md does not tie Go to packages/compiler/go.mod");
}

// 2. Performance budgets must be configured and enforced by CI.
for (const [label, source] of [["web", webConfig], ["examples", exampleConfig]]) {
  if (!/checks\s*:\s*\{[\s\S]*?budget\s*:\s*\{[^}]*js:[^}]*hydration:[^}]*islandJS:[^}]*route:[^}]*\}/.test(source)) {
    fail(`${label} config is missing a complete checks.budget block`);
  }
}
if (!/checks\s*:\s*\{[\s\S]*?failOn\s*:\s*["']error["']/.test(webConfig)) {
  fail("web config must fail checks on errors");
}
if (!ci.includes("KRATE_HYDRATION_BUDGET_MS")) {
  fail("browser CI does not enforce the hydration budget");
}

// 3. Every diagnostic code must be documented; the registry owns the list.
function walk(dir, out = []) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === "node_modules" || entry.name === "dist") continue;
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(full, out);
    else if (entry.name.endsWith(".go")) out.push(full);
  }
  return out;
}
const codes = new Set();
for (const file of walk(path.join(root, "packages/compiler/internal/diag"))) {
  for (const match of fs.readFileSync(file, "utf8").matchAll(/"(KR\d{4})"/g)) codes.add(match[1]);
}
if (codes.size === 0) fail("no diagnostic codes found");
const diagDoc = read("packages/web/src/content/docs/reference/diagnostics.md");
for (const code of codes) {
  if (!diagDoc.includes(`### ${code}\n`)) fail(`diagnostics reference is missing ${code}`);
}

// 4. The safe codemods and their tests/docs must remain wired.
const fixTests = read("packages/compiler/internal/fix/fix_test.go");
for (const testName of ["TestFixLowercaseEvents", "TestFixSafeDiagnosticCodemods", "TestPlanWithCodesFiltersCodemods"]) {
  if (!fixTests.includes(`func ${testName}(`)) fail(`codemod test missing: ${testName}`);
}
const safeFixesDoc = read("packages/web/src/content/docs/guides/safe-fixes.md");
for (const code of ["KR5002", "KR5005", "KR5007"]) {
  if (!safeFixesDoc.includes(code)) fail(`safe-fixes guide is missing ${code}`);
}
if (!read("packages/compiler/internal/mcp/service.go").includes('Name:        "apply_safe_fixes"')) {
  fail("MCP apply_safe_fixes tool is not registered");
}

// 5. Matrix divergences stay explicit.
for (const row of matrix.rows ?? []) {
  const label = row.id || row.area || "<unknown>";
  if ([row.source, row.islands, row.ssr].includes("partial")) {
    if (!Array.isArray(row.knownLimits) || row.knownLimits.length === 0) {
      fail(`${label}: partial surface has no documented divergence`);
    }
  }
  if (row.roadmap && [row.source, row.islands, row.ssr].includes("planned")) {
    fail(`${label}: roadmap surface is still planned`);
  }
}

// 6. Migration verification and rollback must remain part of the release gate.
if (!read("packages/compiler/internal/build/migration_verify.go").includes("func VerifyMigration(")) {
  fail("migration verification API is missing");
}
if (!read("packages/compiler/internal/build/next_migrate_verify_test.go").includes("TestNextMigrateReferenceApps")) {
  fail("three-reference-app migration verification test is missing");
}
if (!read("packages/compiler/internal/migrate/nextjs.go").includes("func Rollback(")) {
  fail("migration rollback API is missing");
}

if (!ci.includes("check:release")) fail("CI does not run check:release");
if (!release.includes("check:release")) fail("release workflow does not run check:release");

if (failures.length > 0) {
  console.error("check-release: the v1 release contract is not satisfied");
  for (const message of failures) console.error(`  ${message}`);
  process.exit(1);
}
console.log(`check-release: ok (${nodeVersions.length} Node, ${preactVersions.length} Preact, ${browsers.length} browser versions; ${codes.size} diagnostics)`);
