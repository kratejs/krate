// Sets the mtime of every tracked file to a time derived from its git blob
// hash, and of every directory to one fixed time.
//
// go test caches a passing result keyed on, among other things, the mtime and
// size of every file the test opened. A fresh checkout gives every file the
// current time, so no result could ever be reused across runs. With mtimes
// that depend only on content, a file that changed gets a different mtime and
// its tests rerun; one that did not keeps its mtime.
//
// Files a test reads that git does not track (built dist/, node_modules) keep
// their fresh mtimes, so tests that read them always rerun.
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

const base = 1_000_000_000; // 2001-09-09, well past go test's "too new" cutoff
const out = execFileSync("git", ["ls-files", "-s", "-z"], { encoding: "utf8", maxBuffer: 1 << 28 });
const dirs = new Set();
let n = 0;
for (const entry of out.split("\0")) {
  if (!entry) continue;
  // "<mode> <blob> <stage>\t<path>"
  const tab = entry.indexOf("\t");
  const [mode, blob] = entry.slice(0, tab).split(" ");
  const file = entry.slice(tab + 1);
  if (mode === "160000") continue; // submodule
  const t = base + parseInt(blob.slice(0, 7), 16);
  try {
    fs.utimesSync(file, t, t);
    n++;
  } catch {
    continue; // deleted or a broken symlink
  }
  for (let d = path.dirname(file); d !== "." && !dirs.has(d); d = path.dirname(d)) dirs.add(d);
}
for (const d of dirs) {
  try {
    fs.utimesSync(d, base, base);
  } catch {}
}
console.log(`normalized the mtimes of ${n} files and ${dirs.size} directories`);
