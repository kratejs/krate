// Rebuilds the embedded docfind WASM modules from the vendored Rust sources in
// packages/compiler/third_party/docfind and copies them (plus the hand-written
// browser glue) into packages/compiler/internal/docfind/embedded so they get
// `go:embed`-ed into the krate binary.
//
// Requires: Rust + the wasm32-unknown-unknown target.
//
//   rustup target add wasm32-unknown-unknown
//   node scripts/build-docfind.mjs
//
// The committed WASM artifacts should be regenerated whenever the vendored
// docfind source under third_party/docfind changes.
import { execFileSync } from "node:child_process";
import { copyFileSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const docfindDir = join(root, "packages", "compiler", "third_party", "docfind");
const embeddedDir = join(root, "packages", "compiler", "internal", "docfind", "embedded");
const releaseDir = join(docfindDir, "target", "wasm32-unknown-unknown", "release");

mkdirSync(embeddedDir, { recursive: true });

// 1) Compile the workspace (search module + builder module).
execFileSync("cargo", ["build", "--release", "--target", "wasm32-unknown-unknown"], {
  cwd: docfindDir,
  stdio: "inherit",
});

// 2) Copy the modules.
const searchOut = join(releaseDir, "docfind_wasm.wasm");
const builderOut = join(releaseDir, "docfind_builder.wasm");
copyFileSync(searchOut, join(embeddedDir, "search.wasm"));
copyFileSync(builderOut, join(embeddedDir, "builder.wasm"));

// 3) Verify the search module still exposes the expected exports so krate
//    doesn't silently break if the Rust source is refactored. WASM export
//    names are length-prefixed UTF-8, so the raw name bytes appear verbatim.
const check = (path, names) => {
  const buf = readFileSync(path);
  for (const n of names) {
    if (buf.indexOf(Buffer.from(n, "utf8")) === -1) {
      throw new Error(`${path} is missing export "${n}"`);
    }
  }
};
check(searchOut, ["docfind_search", "docfind_alloc", "docfind_free", "INDEX_BASE", "INDEX_LEN"]);
check(builderOut, ["docfind_build", "docfind_alloc", "docfind_free"]);

// 4) The browser glue is maintained in the embedded dir itself; keep it in sync
//    (nothing to regenerate here, but re-write it to normalize line endings).
const glueSrc = readFileSync(join(embeddedDir, "docfind.js"), "utf8");
writeFileSync(join(embeddedDir, "docfind.js"), glueSrc);

const size = (f) => readFileSync(f).length;
console.log("docfind rebuilt:");
console.log(`  search.wasm  ${size(searchOut)} bytes`);
console.log(`  builder.wasm ${size(builderOut)} bytes`);
