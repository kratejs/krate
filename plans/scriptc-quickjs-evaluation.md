# scriptc and QuickJS: what Krate can learn

Evaluated 2026-11-10 against https://github.com/vercel-labs/scriptc (commit
63d5d957, CLI 0.4.11) and Krate's `internal/jsruntime` and `internal/jseval`.

## 1. How scriptc uses QuickJS today

scriptc no longer embeds QuickJS in compiled programs. 0.3.0 removed the
`--dynamic` option and the embedded JavaScript runtime (`CHANGELOG.md`, 0.3.0,
"Compilation is native-only"). QuickJS (quickjs-ng v0.15.1, vendored under
`packages/runtime/vendor/quickjs-ng`) survives in two roles:

1. **Build-time `comptime` helper.** A separate executable, `scriptc-comptime`
   (`native/llvm-codegen/comptime/comptime.c`), runs `comptime(() => ...)`
   callbacks during the build.
   - The callback must be an inline, non-async literal with no outer bindings
     (`lower-comptime.ts`, SC1090).
   - Only source text goes in; only tagged JSON comes out (`comptime.ts`).
   - Limits are 2000 ms, 256 MB and a 2 MiB stack. Memory is enforced with
     custom `JSMallocFunctions`, the deadline with a bytecode interrupt
     handler, and a watchdog thread kills the process 500 ms past the deadline
     to cover long builtins such as sort.
   - No host bindings are installed. Locale-dependent methods (`localeCompare`,
     `toLocale*`, `Date.prototype.toString`) are replaced with throwing stubs so
     the build output doesn't depend on the build machine's locale.
   - Results are validated before baking: 64 MB, 512 levels deep, 1M array
     elements. NaN, Infinity, symbols, cycles and pending promises are refused.
2. **Regex and Unicode only.** `scr_regex.c` links `libregexp` and
   `libunicode` directly, with two local patches: faster character-class
   building, and a WTF-8 subject mode that matches strings in their native
   storage without converting to UTF-16.

Everything else is a hand-written C runtime (`packages/runtime/src/scr_*.c`):
- **Event loop:** its own epoll, kqueue and WSAPoll loop, not libuv.
- **Async:** async functions compile to stackless coroutines.
- **Numbers:** Ryu for number formatting and a port of fdlibm for `Math`.
- **Libraries vendored from Node's `deps/`:** zlib, brotli, zstd, mbedTLS, and
  ICU 78 for Intl, so output bytes match Node v24.
- **Code it can't lower:**
  - It compiles to a native "dynamic tier" with runtime type checks.
  - Or it runs in an opt-in "Node island": real Node/V8 loaded in-process
    through a versioned C ABI (`scr_node_bridge.h`). This exists on Linux glibc
    and macOS only, not Windows or musl.
- `eval`, `new Function` and `vm` throw `EvalError`.

### What scriptc does around the engine

- **Compatibility ledger.**
  - `internal/compatibility/node-v24.json` pins the Node API set being
    measured.
  - `pnpm node-compat` generates a per-API status table: supported, partial,
    island-served, refused, not-implemented, by-design, unreviewed or
    not-applicable.
  - Each row records how it was verified (test-backed, explicit refusal,
    verified absence), and the table is published on the docs site.
- **Differential corpus.** `tests/corpus` runs each program under Node and as a
  compiled binary and compares stdout, stderr and exit code byte for byte.
  There are also ASan lanes and a number-formatting fuzzer that compares with
  Node's `String(x)`.
- **`scriptc coverage`.** It reports, per statement, whether code compiles,
  with SC codes and `hint:` lines, plus "differs from Node" sites.
  `--fail-on=...` turns it into a CI gate.
- **Feature-selected runtime.**
  - `NativeLinkFeatures` (regex, fetch, wasm, intl, workers, ...) decides which
    runtime units and vendor archives get linked.
  - Linker dead-stripping does the rest.
  - A "zero-cost gate" checks that unused features leave no symbols behind.

## 2. How Krate uses QuickJS today

One wrapper, `internal/jsruntime`, uses `modernc.org/quickjs` (pure Go). Its
uses:
- config and Tailwind evaluation
- `generateStaticParams` and metadata, falling back to Node
- compile-time expression folding (pooled VMs)
- content chains, islands, JS plugins and custom check rules
- API routes and middleware (worker pool, default `min(NumCPU, 8)`)

Each VM gets 32 MB of memory, a 256 KB stack and a 30 s timeout.

The Web API surface is one JS prelude, `webapi.js`. It is compiled to bytecode
once per process and evaluated in every new VM. Crypto, fetch, URL parsing and
multipart parsing are bridged to Go.
- **Stubs that throw:** `WritableStream`, `TransformStream`,
  `CompressionStream`, `WebSocket`, `EventSource`, most of `crypto.subtle`, and
  the `ReadableStream` pipe methods. They throw `NotSupportedError` *at run
  time*.
- **Binary data** crosses as base64, because the wrapper only creates ints,
  floats, strings and objects.
- **Timers** live in a JS queue that Go drains per request; there is no
  persistent event loop.
- **Node-only routes** are detected by imports that stay external, and run on
  the Node sidecar.
- **Testing:** about 17 differential cases compare QuickJS with Node
  (`api_runtime_conformance_test.go`). `compat-matrix.json` has no QuickJS row.

## 3. Improvements for Krate

Ordered by value. Items marked **Native** are required by
`plans/krate-native.md`, because `@host` code on QuickJS has no Node to fall
back to.

1. **Report unsupported APIs at build time, not run time.** (Native; also helps
   web.)
   - Today a route that uses `TransformStream` or `crypto.subtle.encrypt`
     builds fine and throws in production.
   - Scan each QuickJS bundle (route, middleware, `@host`) for references to
     APIs that are stubs or missing, the way scriptc's coverage pass does.
   - Report each one as a `KR` diagnostic with file, line and a hint (use the
     Node sidecar on web; on native, use a Go host function or a node
     island).
   - The runtime `NotSupportedError` messages should carry the same codes. They
     are plain strings today.
2. **Add a compatibility ledger for the embedded runtime.**
   - Keep `internal/jsruntime/compat.json`, one row per Web API and Node global
     against a pinned denominator (WinterTC minimum common API, plus the Node
     globals Krate claims).
   - Use scriptc's statuses and verification basis.
   - Generate the docs table from it, and have `check:matrix` fail when a row
     claims support without a test.
   - The same file drives item 1's scan, so the docs and the diagnostics can't
     disagree.
3. **Grow the differential tests into a corpus.**
   - Move the 17 inline cases to `testdata/quickjs-corpus/*.ts` files.
   - Run each under QuickJS and Node, and compare output byte for byte.
   - Cover every polyfill in `webapi.js`: URL edge cases, TextDecoder fatal
     modes, structuredClone of Map/Set/Date, Headers ordering, FormData, the
     stream readers, HMAC.
   - Native's dual-engine test lane (QuickJS in dev, scriptc in release) reuses
     the same corpus.
4. **Trim the prelude per bundle.** (Speeds up every fresh VM.)
   - Every new VM evaluates the whole `webapi.js`, which `jsruntime.go` calls
     the dominant cost of VM creation.
   - Split it into feature modules (events, timers, encoding, url, fetch,
     streams, crypto, formdata) and install only what the bundle references.
     The esbuild metafile and an identifier scan give that list.
   - This is the same idea as Krate's trimmed client runtime and scriptc's
     feature-selected link.
   - Biggest win: the paths that make a fresh VM per call (config, GSP,
     metadata, islands, dev and isolation-mode API requests).
5. **Use bare VMs for compile-time evaluation.**
   - `EvaluateExpr` takes VMs from `New()`, which installs the full prelude,
     including `__go_fetch`, `__go_random` and the body readers. Today the
     purity check is the only thing keeping an expression away from them.
   - Follow scriptc's comptime design: a separate constructor with no host
     bindings and no prelude.
   - It's faster, and it removes the dependence on the purity check alone.
   - Also stub the locale-dependent methods to throw. That matches the
     compile-time-evaluation guide's rule that locale output stays a runtime
     value.
6. **Validate baked values.**
   - Values from config, metadata, GSP and `@build` come back as JSON strings.
   - Add scriptc's checks before baking them: size, depth and array-length
     caps, and refusal of NaN, Infinity, cycles and pending promises. Each
     failure gets a `KR` code instead of a silent `null` from
     `JSON.stringify`.
7. **Pass binary data without base64.** (Native: section 2 promises raw
   `[]byte` and `Uint8Array`.)
   - The `modernc.org/quickjs` wrapper has no ArrayBuffer constructor, but the
     transpiled `modernc.org/libquickjs` underneath exposes
     `JS_NewArrayBufferCopy` and `JS_GetArrayBuffer`.
   - Add two wrapper functions on top of it. That removes the base64 step from
     request bodies, fetch bodies and Native host calls.
   - This is an M2 spike item for Native.
8. **Give Native host JS a persistent event loop.** (Native.)
   - Request-scoped timer draining works for API routes. `@host` code needs
     intervals, concurrent fetches, streams and background work that outlives
     any single call.
   - Design: one goroutine owns the VM. It keeps a timer heap and a job queue,
     and I/O goroutines post completions to that queue. This is the Go
     equivalent of scriptc's per-OS loop.
9. **Fill the host-relevant API gaps in Go.**
   - `TransformStream` and `WritableStream` in JS.
   - `CompressionStream` and `DecompressionStream` over `compress/flate` and
     `compress/gzip`.
   - `crypto.subtle` AES-GCM, ECDSA, Ed25519 and `generateKey`/`exportKey` over
     the Go standard library.
   - A `WebSocket` client over `golang.org/x/net`, which is already a
     dependency.
   - A `Buffer` in the API runtime, reusing the config-path one in
     `jseval/nodeglobals.go`.
   - All of these stay pure Go, so the no-cgo rule holds.
10. **Node islands per package for Native.**
    - scriptc's answer to "this package can't run here" is to run *that
      package* in real Node and compile the rest.
    - Krate Native can do the same without cgo: `native.host.islands: {
      "sharp": "node" }` runs only that module in a node child, over the
      existing pipe protocol, while the rest of the host code stays on
      QuickJS.
    - This replaces the plan's "one JS runtime per app": the app-wide
      `runtime` stays the default, and islands override it per package.

## 4. Corrections to the scriptc row in the Native plan

- **Toolchain:** release builds need clang on macOS and glibc Linux, and zig on
  Windows, musl and every cross-compile. Install `@scriptc/runtime-<target>` at
  the compiler's version.
- **In-process loading isn't possible without cgo.**
  - Library mode (`build --lib --profile`) emits a static `.a` archive or a
    WASI reactor, not a shared library.
  - It also doesn't support async, promises or timers.
  - So scriptc host code runs as a child executable over the pipe protocol.
    The "load it with purego" idea is dropped.
- **There's no runtime fallback.** Code scriptc can't compile fails the build,
  and `eval`/`new Function` throw. `krate native check --scriptc` should wrap
  `scriptc coverage --fail-on=...` and turn SC codes into `KR` diagnostics.
- **scriptc's Node islands won't help Krate on Windows:** they need the libnode
  packs, which exist only for Linux glibc and macOS. Krate's own per-package
  islands (item 10) cover every OS.
