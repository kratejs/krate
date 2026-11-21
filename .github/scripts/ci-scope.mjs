// Decides whether this Krate Native job has work to do on this run and
// writes RUN=true|false to $GITHUB_ENV. Each native job runs it as its first
// step, so no job waits on a separate scope job.
//
// Pull requests are path-filtered: a change to native code runs every native
// runner (the full GUI set on Linux x64, macOS and Windows x64, a thin smoke
// set on the two arm64 runners); a change to the rest of the compiler or the
// runtime runs only the Linux x64 one; anything else (docs, other packages)
// runs none of them.
//
// Pushes to dev and main, the nightly schedule and manual runs are full runs:
// every native job, plus the slow steps pull requests skip.
//
// Inputs (env): EVENT (github.event_name), RUNNER (the matrix os, or "cross"
// for the cross-compile job).
import { execFileSync } from "node:child_process";
import fs from "node:fs";

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

// The runner that also runs on compiler-only changes.
const primary = "ubuntu-24.04";

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

// scope is "all", "linux" (compiler-only changes) or "none".
export function scopeOf(event, files) {
  if (event !== "pull_request" || files === null) return "all";
  if (files.some((f) => nativePaths.some((re) => re.test(f)))) return "all";
  if (files.some((f) => compilerPaths.some((re) => re.test(f)))) return "linux";
  return "none";
}

export function shouldRun(scope, runner) {
  if (scope === "all") return true;
  if (scope === "none") return false;
  return runner === primary || runner === "cross";
}

const event = process.env.EVENT ?? "";
const runner = process.env.RUNNER ?? "";
const files = event === "pull_request" ? changedFiles() : [];
const scope = scopeOf(event, files);
const run = shouldRun(scope, runner);

if (event === "pull_request") console.log(`${files?.length ?? "?"} changed files`);
console.log(`event=${event} scope=${scope} runner=${runner} run=${run}`);
if (!run) {
  console.log(
    `::notice title=Native skipped::No native or compiler change in this pull request reaches ${runner}; the merge to dev or main runs it in full.`,
  );
}
if (process.env.GITHUB_ENV) fs.appendFileSync(process.env.GITHUB_ENV, `RUN=${run}\n`);
