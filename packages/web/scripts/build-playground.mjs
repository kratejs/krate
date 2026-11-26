// Builds the in-browser playground engine into public/playground:
//
//   krate.wasm | krate.wasm.gz  the Krate compiler (GOOS=js GOARCH=wasm)
//   wasm_exec.js                Go's wasm loader, matching the Go toolchain
//   packages.json               @krate/runtime and @krate/components files for node_modules
//   manifest.json               what the engine should load
//
// engine.js and memfs.js in the same directory are committed sources.
//
// Cloudflare Pages rejects files over 25 MiB. The compiler is far larger than
// that raw, so unless the wasm fits, it is shipped gzipped (krate.wasm.gz) and
// the engine inflates it with DecompressionStream.
//
// Without Go on PATH the script warns and exits 0 so `pnpm build` still works
// (the playground page then shows its fallback); set KRATE_REQUIRE_PLAYGROUND=1
// to make that an error.

import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from "node:fs";
import { dirname, join, posix, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { brotliCompressSync, constants as zc, gzipSync } from "node:zlib";

const here = dirname(fileURLToPath(import.meta.url));
const webDir = resolve(here, "..");
const repoRoot = resolve(webDir, "../..");
const compilerDir = join(repoRoot, "packages/compiler");
const outDir = join(webDir, "public/playground");
const workDir = join(webDir, ".playground-build");
const CF_LIMIT = 25 * 1024 * 1024;
// Leave headroom under the limit for the raw form.
const RAW_MAX = 24 * 1024 * 1024;

const required = process.env.KRATE_REQUIRE_PLAYGROUND === "1";

function skip(message) {
  console.warn(`[playground] ${message}`);
  if (required) {
    console.error("[playground] KRATE_REQUIRE_PLAYGROUND=1: failing");
    process.exit(1);
  }
  console.warn("[playground] skipping the playground engine; the page will show its fallback");
  process.exit(0);
}

function run(cmd, args, opts = {}) {
  return spawnSync(cmd, args, { encoding: "utf8", ...opts });
}

const goVersion = run("go", ["version"], { cwd: compilerDir });
if (goVersion.error || goVersion.status !== 0) skip("Go was not found on PATH.");

const goroot = run("go", ["env", "GOROOT"], { cwd: compilerDir }).stdout.trim();
const execCandidates = [join(goroot, "lib/wasm/wasm_exec.js"), join(goroot, "misc/wasm/wasm_exec.js")];
const wasmExec = execCandidates.find((p) => existsSync(p));
if (!wasmExec) skip(`wasm_exec.js not found under ${goroot}.`);

mkdirSync(workDir, { recursive: true });
mkdirSync(outDir, { recursive: true });

// 1. The compiler.
const rawWasm = join(workDir, "krate.wasm");
const compilerVersion = JSON.parse(readFileSync(join(repoRoot, "packages/core/package.json"), "utf8")).version;
console.log(`[playground] ${goVersion.stdout.trim()}: building krate.wasm`);
const build = run(
  "go",
  ["build", "-trimpath", `-ldflags=-s -w -X main.buildVersion=${compilerVersion}`, "-o", rawWasm, "./cmd/krate-wasm"],
  { cwd: compilerDir, env: { ...process.env, GOOS: "js", GOARCH: "wasm", CGO_ENABLED: "0" }, stdio: ["ignore", "inherit", "inherit"] },
);
if (build.status !== 0) {
  if (required) {
    console.error("[playground] go build failed");
    process.exit(1);
  }
  skip("go build for js/wasm failed (see the output above).");
}

const wasm = readFileSync(rawWasm);
const gz = gzipSync(wasm, { level: 9 });
const br = brotliCompressSync(wasm, {
  params: { [zc.BROTLI_PARAM_QUALITY]: 9, [zc.BROTLI_PARAM_LGWIN]: 24, [zc.BROTLI_PARAM_SIZE_HINT]: wasm.length },
});
const mib = (n) => (n / 1048576).toFixed(2) + " MiB";
console.log(`[playground] krate.wasm raw ${mib(wasm.length)}, gzip ${mib(gz.length)}, brotli(q9) ${mib(br.length)}`);

for (const stale of ["krate.wasm", "krate.wasm.gz"]) rmSync(join(outDir, stale), { force: true });
let wasmName;
let wasmEncoding = "identity";
let wasmSize;
if (wasm.length <= RAW_MAX) {
  wasmName = "krate.wasm";
  wasmSize = wasm.length;
  writeFileSync(join(outDir, wasmName), wasm);
} else {
  if (gz.length > RAW_MAX) {
    skip(`krate.wasm is ${mib(wasm.length)} raw and ${mib(gz.length)} gzipped: both exceed the Cloudflare Pages 25 MiB file limit.`);
  }
  console.log(`[playground] raw wasm exceeds ${mib(CF_LIMIT)}; shipping krate.wasm.gz (the engine inflates it in the browser)`);
  wasmName = "krate.wasm.gz";
  wasmEncoding = "gzip";
  wasmSize = gz.length;
  writeFileSync(join(outDir, wasmName), gz);
}

// 2. wasm_exec.js from the same toolchain.
writeFileSync(join(outDir, "wasm_exec.js"), readFileSync(wasmExec));

// 3. The npm packages the compiler resolves from the project's node_modules.
const packages = {
  "@krate/runtime": findPackage("runtime", "dist/index.js", collectRuntimeFiles),
  "@krate/components": findPackage("components", "src/index.ts", collectComponentFiles),
};
const packagesJson = JSON.stringify({ packages });
writeFileSync(join(outDir, "packages.json"), packagesJson);
for (const [name, p] of Object.entries(packages)) {
  console.log(`[playground] ${name}@${p.version}: ${Object.keys(p.files).length} files from ${p.label}`);
}
console.log(`[playground] packages.json: ${mib(Buffer.byteLength(packagesJson))}, ${mib(gzipSync(packagesJson).length)} gzipped`);

// 4. The manifest.
const goVer = /go version (\S+)/.exec(goVersion.stdout)?.[1] ?? "";
writeFileSync(
  join(outDir, "manifest.json"),
  JSON.stringify(
    { version: compilerVersion, go: goVer, wasm: wasmName, wasmEncoding, wasmSize, wasmExec: "wasm_exec.js", packages: "packages.json" },
    null,
    2,
  ),
);
console.log(`[playground] wrote ${outDir}`);

// The workspace package matches the compiler of the same commit; the npm
// release the site depends on is the fallback.
function findPackage(dirName, probe, collect) {
  const candidates = [
    { dir: join(repoRoot, "packages", dirName), label: `packages/${dirName}` },
    { dir: join(webDir, "node_modules/@krate", dirName), label: `node_modules/@krate/${dirName}` },
  ];
  for (const c of candidates) {
    if (existsSync(join(c.dir, probe)) && existsSync(join(c.dir, "package.json"))) {
      return { version: JSON.parse(readFileSync(join(c.dir, "package.json"), "utf8")).version, label: c.label, files: collect(c.dir) };
    }
  }
  skip(`@krate/${dirName} is not available: build the workspace packages (\`pnpm build\`) or install dependencies.`);
}

// Every file under src (the package ships TSX and CSS sources).
function collectComponentFiles(dir) {
  const files = {};
  const visit = (rel) => {
    for (const entry of readdirSync(join(dir, rel), { withFileTypes: true })) {
      const next = posix.join(rel, entry.name);
      if (entry.isDirectory()) visit(next);
      else files[next] = readFileSync(join(dir, next), "utf8");
    }
  };
  visit("src");
  files["package.json"] = readFileSync(join(dir, "package.json"), "utf8");
  return files;
}

// Every JS file of the package reachable from package.json "exports" by
// relative imports, plus package.json itself (reduced to what resolution needs).
function collectRuntimeFiles(dir) {
  const pkg = JSON.parse(readFileSync(join(dir, "package.json"), "utf8"));
  const entries = new Set();
  const addTargets = (v) => {
    if (typeof v === "string") {
      if (/\.(m?js|cjs)$/.test(v)) entries.add(posix.normalize(v));
    } else if (v && typeof v === "object") {
      for (const [k, x] of Object.entries(v)) if (k !== "types") addTargets(x);
    }
  };
  addTargets(pkg.exports);
  addTargets(pkg.main);
  const files = {};
  const seen = new Set();
  const spec = /(?:\bfrom\s*|\bimport\s*\(?\s*|\bexport\s*\*\s*from\s*)["'](\.{1,2}\/[^"']+)["']/g;
  const visit = (rel) => {
    rel = posix.normalize(rel);
    if (seen.has(rel)) return;
    seen.add(rel);
    const file = join(dir, rel);
    if (!existsSync(file)) return;
    const text = readFileSync(file, "utf8");
    files[rel] = text;
    for (const m of text.matchAll(spec)) {
      let target = posix.join(posix.dirname(rel), m[1]);
      if (!/\.(m?js|cjs|json)$/.test(target)) target += ".js";
      visit(target);
    }
  };
  for (const e of entries) visit(e);
  files["package.json"] = JSON.stringify({
    name: pkg.name,
    version: pkg.version,
    type: pkg.type,
    main: pkg.main,
    exports: pkg.exports,
    sideEffects: pkg.sideEffects,
  });
  return files;
}
