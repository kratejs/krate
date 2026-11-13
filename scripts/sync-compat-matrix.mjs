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
  supported: "Supported",
  partial: "Partial",
  planned: "Planned",
  untested: "Untested",
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
  "description: The public Krate compatibility matrix for React source components, Preact islands, SSR, the browser conformance corpus, and Krate Native per operating system.",
  "---",
  "",
  "# Compatibility Matrix",
  "",
  "This is the support contract for React compatibility. A surface is only marked",
  "supported or stable when the referenced tests pass. Known divergences are",
  "listed instead of silently hidden.",
  "",
  `Last verified: **${matrix.updated}**. See the [roadmap](/roadmap/) for phase`,
  "status and the [React compatibility guide](/docs/guides/react-compatibility/)",
  "for usage.",
  "",
  "## Status key",
  "",
  "- **Stable** - covered by the corpus and the cross-browser gate.",
  "- **Supported** - implemented and backed by the tests listed under Evidence, which run in CI; edge cases may still change before v1.",
  "- **Partial** - a documented subset works; unsupported patterns must produce a diagnostic.",
  "- **Planned** - not part of the supported surface yet.",
  "- **Untested** - Krate Native only: the code exists and builds for that OS, but no test has run there. The row's known limits say what has and has not been checked.",
  "- **n/a** - not applicable; for Krate Native it can mean the API answers `ErrUnsupported` on that OS.",
  "",
  "## Surfaces",
  "",
  "| Surface | Source | Islands | SSR | v1 target |",
  "|---|---|---|---|---|",
];

const webRows = matrix.rows.filter((row) => row.native === undefined);
const nativeRows = matrix.rows.filter((row) => row.native !== undefined);

for (const row of webRows) {
  lines.push(
    `| ${cell(row.area)} | ${status(row.source)} | ${status(row.islands)} | ${status(row.ssr)} | ${cell(row.target)} |`,
  );
}

if (nativeRows.length > 0) {
  lines.push(
    "",
    "## Krate Native",
    "",
    "Krate Native is experimental: its API, `app.json` fields and SDK can change between",
    "releases. It targets Linux (GTK 3 and WebKitGTK 4.1 through purego), macOS (WKWebView)",
    "and Windows (WebView2), on x64 and arm64. The CI `native` job runs the GUI tests on",
    "all of them and blocks the pull request. A cell says Supported when tests that drive",
    "a real window on that OS pass there, and Untested when none do or the relevant ones",
    "skip. Real hardware (cameras, suspend), screen readers, notarization and store",
    "submission are listed as limits in the details.",
    "",
    "| Surface | Linux | macOS | Windows | v1 target |",
    "|---|---|---|---|---|",
  );
  for (const row of nativeRows) {
    lines.push(
      `| ${cell(row.area)} | ${status(row.native.linux)} | ${status(row.native.macos)} | ${status(row.native.windows)} | ${cell(row.target)} |`,
    );
  }
}

lines.push("", "## Details", "");

for (const row of matrix.rows) {
  lines.push(`### ${row.area}`, "");
  if (row.native !== undefined) {
    lines.push(
      `**Linux:** ${status(row.native.linux)} - **macOS:** ${status(row.native.macos)} - **Windows:** ${status(row.native.windows)}`,
      "",
    );
  } else {
    lines.push(
      `**Source:** ${status(row.source)} - **Islands:** ${status(row.islands)} - **SSR:** ${status(row.ssr)}`,
      "",
    );
  }
  lines.push(row.summary, "");
  if (Array.isArray(row.knownLimits) && row.knownLimits.length > 0) {
    lines.push("Known limits:", "");
    for (const limit of row.knownLimits) lines.push(`- ${limit}`);
    lines.push("");
  }
  if (Array.isArray(row.diagnostics) && row.diagnostics.length > 0) {
    lines.push(`Diagnostics: ${row.diagnostics.map((code) => `\`${code}\``).join(", ")}`, "");
  }
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
    console.error("compat-matrix: generated docs are stale; run pnpm sync:docs");
    process.exit(1);
  }
  console.log("compat-matrix: docs are in sync");
} else {
  fs.writeFileSync(target, output);
  console.log(`compat-matrix: wrote ${path.relative(root, target)}`);
}
