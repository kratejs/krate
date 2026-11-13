// Decides how much of CI a run needs and writes it to $GITHUB_OUTPUT.
//
// Pull requests run the fast set. The Krate Native jobs are path-filtered:
// a change to native code runs them on all five runners (the full GUI set on
// Linux x64, macOS and Windows x64, a thin smoke set on the two arm64
// runners); a change to the rest of the compiler or the runtime runs only the
// Linux x64 one; anything else (docs, other packages) skips them.
//
// Pushes to dev and main, the nightly schedule and manual runs are full runs:
// every native job, plus the slow steps PRs skip (packaging and update smoke
// tests, the repeated and Wayland GUI passes, budgets, memory reports), and
// go test without the test cache.
import { execFileSync } from "node:child_process";
import fs from "node:fs";

const full = process.env.EVENT !== "pull_request";

const runners = {
  "ubuntu-latest": { os: "ubuntu-latest", role: "primary" },
  "ubuntu-24.04-arm": { os: "ubuntu-24.04-arm", role: "thin" },
  "macos-latest": { os: "macos-latest", role: "platform" },
  "windows-latest": { os: "windows-latest", role: "platform" },
  // Bun and Deno publish no Windows on ARM builds; the runner image may also
  // lack the Windows SDK tools and NSIS.
  "windows-11-arm": {
    os: "windows-11-arm",
    role: "thin",
    optional_runtimes: "bun,deno",
    optional_tools: "makensis,signtool,makeappx",
  },
};

const nativePaths = [
  /^packages\/native\//,
  /^packages\/app\//,
  /^packages\/compiler\/internal\/native\//,
  /^packages\/compiler\/internal\/build\/native/,
  /^packages\/compiler\/cmd\/krate\/native/,
  /^examples\/native-/,
  /^scripts\/native-/,
  /^\.github\//,
];
const compilerPaths = [/^packages\/compiler\//, /^packages\/runtime\//, /^packages\/core\//, /^pnpm-lock\.yaml$/];

function changedFiles() {
  try {
    // actions/checkout fetches the PR merge commit with fetch-depth 2, so its
    // first parent is the base branch the PR merges into.
    return execFileSync("git", ["diff", "--name-only", "HEAD^1", "HEAD"], { encoding: "utf8" })
      .split("\n")
      .filter(Boolean);
  } catch {
    return null;
  }
}

let scope;
if (full) {
  scope = "all";
} else {
  const files = changedFiles();
  if (files === null) scope = "all";
  else if (files.some((f) => nativePaths.some((re) => re.test(f)))) scope = "all";
  else if (files.some((f) => compilerPaths.some((re) => re.test(f)))) scope = "linux";
  else scope = "none";
  console.log(`${files?.length ?? "?"} changed files`);
}

const matrix =
  scope === "all" ? Object.values(runners) : scope === "linux" ? [runners["ubuntu-latest"]] : [];

console.log(`full=${full} native=${scope}`);
const out = process.env.GITHUB_OUTPUT;
const lines = [`full=${full}`, `native=${scope}`, `native_matrix=${JSON.stringify(matrix)}`];
if (out) fs.appendFileSync(out, lines.join("\n") + "\n");
else console.log(lines.join("\n"));
