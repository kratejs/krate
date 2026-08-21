# Vendored: docfind (microsoft/docfind)

A vendored copy of [microsoft/docfind](https://github.com/microsoft/docfind)
(v0.5.1, Apache-2.0), a high-performance document search engine built in Rust
with WebAssembly support (FST + FSST + RAKE keyword extraction).

## Why it's here

Krate's docs plugin ships a search bar backed by a docfind index that is
**embedded into a WASM module** at docs-build time. To keep that a pure
in-process operation — no subprocess, no temp JSON file — krate embeds the
docfind WASM modules into the krate binary (`internal/docfind/embedded`, via
`go:embed`) and drives the builder module through a Go WebAssembly runtime
(`github.com/tetratelabs/wazero`).

## Modifications vs. upstream

The upstream repo has `core/` (index building + search), `cli/` (a standalone
build tool), and `wasm/` (wasm-bindgen bindings). Krate does **not** shell out
to the `cli`. Instead:

- `core/` — unchanged.
- `wasm/` — rewritten to a raw C-ABI search module (no wasm-bindgen): exports
  `docfind_search`, `docfind_alloc`, `docfind_free`, plus the `INDEX_BASE` /
  `INDEX_LEN` globals that get patched when an index is embedded.
- `builder/` — new crate: a port of the upstream `cli`'s index-embedding phase
  exposed as a C-ABI `docfind_build(docs_json, template_wasm) -> wasm` export.
- The browser glue is `internal/docfind/embedded/docfind.js` (hand-written,
  maintained there).

## Rebuilding the WASM modules

Requires Rust + the `wasm32-unknown-unknown` target:

```sh
rustup target add wasm32-unknown-unknown
node scripts/build-docfind.mjs
```

This regenerates `internal/docfind/embedded/search.wasm` and `builder.wasm`.
Commit the artifacts so the repo builds without a Rust toolchain.
