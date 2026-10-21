import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const source = path.join(root, "packages/web/src/data/compat-matrix.json");
const target = path.join(root, "packages/web/src/content/docs/guides/compatibility.md");
const check = process.argv.includes("--check");

const matrix = JSON.parse(fs.readFileSync(source, "utf8"));
const labels = {
  stable: "Stable",
  beta: "Beta",
  partial: "Partial",
  planned: "Planned",
  "n/a": "n/a",
};

function status(value) {
  return labels[value] || value;
}

function cell(value) {
  return String(value).replaceAll("|", "\\|");
}

function evidence(tests) {
  if (!tests || tests.length === 0) return "Planned; no stable test reference yet.";
  return tests.map((test) => `\`${test}\``).join(", ");
}

const lines = [
  "---",
  "title: Compatibility Matrix",
  "order: 6",
  "description: The public Krate compatibility matrix for React source components, Preact islands, SSR, and the browser conformance corpus.",
  "---",
  "",
  "# Compatibility Matrix",
  "",
  "This is the support contract for React compatibility. A surface is only marked",
  "stable when the referenced corpus and browser tests pass. Known divergences are",
  "listed instead of silently hidden.",
  "",
  `Last verified: **${matrix.updated}**. See the [roadmap](/roadmap/) for phase`,
  "status and the [React compatibility guide](/docs/guides/react-compatibility/)",
  "for usage.",
  "",
  "## Status key",
  "",
  "- **Stable** - covered by the corpus and the cross-browser gate.",
  "- **Beta** - implemented and tested; edge cases may still change before v1.",
  "- **Partial** - a documented subset works; unsupported patterns must produce a diagnostic.",
  "- **Planned** - not part of the supported surface yet.",
  "",
  "## Surfaces",
  "",
  "| Surface | Source | Islands | SSR | v1 target |",
  "|---|---|---|---|---|",
];

for (const row of matrix.rows) {
  lines.push(
    `| ${cell(row.area)} | ${status(row.source)} | ${status(row.islands)} | ${status(row.ssr)} | ${cell(row.target)} |`,
  );
}

lines.push("", "## Details", "");

for (const row of matrix.rows) {
  lines.push(`### ${row.area}`, "");
  lines.push(
    `**Source:** ${status(row.source)} · **Islands:** ${status(row.islands)} · **SSR:** ${status(row.ssr)}`,
    "",
  );
  lines.push(row.summary, "");
  lines.push(`Evidence: ${evidence(row.tests)}`, "");
}

lines.push(
  "## How claims are verified",
  "",
  "The compiler and runtime suites pin lowering, diagnostics, and runtime",
  "primitives. The browser corpus runs Chromium, Firefox, and WebKit against the",
  "same built output, including the React demo, shadcn-style dashboard, forms,",
  "islands, styles, and event interactions. The Preact jobs cover the minimum,",
  "intermediate, and workspace versions.",
  "",
  "Corpus:",
  "",
  "- Radix suite and compiled-package islands: `examples/src/pages/react-demo.tsx`",
  "- shadcn-style dashboard and derived lists: `examples/src/pages/react-dashboard.tsx`",
  "- Forms and controlled inputs: `examples/src/pages/react-forms.tsx`",
  "- Class components: `examples/src/pages/react-demo.tsx`",
  "- Next.js App Router fixture: `packages/benchmark/fixtures/next`",
  "",
  "A documented limitation is acceptable; a silent one is a bug. Report a",
  "divergence with a minimal component and a browser.",
  "",
);

const output = lines.join("\n");
if (check) {
  const current = fs.existsSync(target) ? fs.readFileSync(target, "utf8") : "";
  if (current.replaceAll("\r\n", "\n") !== output) {
    console.error("compat-matrix: generated docs are stale; run pnpm sync:matrix");
    process.exit(1);
  }
  console.log("compat-matrix: docs are in sync");
} else {
  fs.writeFileSync(target, output);
  console.log(`compat-matrix: wrote ${path.relative(root, target)}`);
}
