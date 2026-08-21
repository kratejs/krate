---
title: Contributing
order: 4
---

# Contributing

Krate is built in the open. Contributions are welcome!

## Repository layout

```
packages/
  compiler/      Go compiler (lexer, parser, bundler, renderer, build pipeline)
  runtime/       Client runtime — signals, hydration, SPA router, resources, context
  components/    Built-in UI components (shadcn/ui-style)
  core/          npm CLI wrapper (@krate/core)
  web/           This documentation website
  core-*/        Per-platform binaries (built in CI, published to npm)
examples/        Docs + feature demo site (also used by the integration tests)
scripts/         Build tooling (platform packages, docfind WASM, versioning)
```

## Development workflow

Build the compiler:

```sh
cd packages/compiler
go build -o krate.exe ./cmd/krate
```

Run the example site in dev mode:

```sh
pnpm dev    # runs krate dev against examples/
```

## Testing

```sh
go test ./...                              # all unit + integration tests
go test ./internal/build/ -v               # full project build integration test
go vet ./...
```

Test suites:

- `internal/lexer/lexer_test.go` — 20+ lexer unit tests
- `internal/parser/parser_test.go` — 22 parser unit tests + edge cases
- `internal/renderer/renderer_test.go` — 20+ renderer unit tests
- `internal/css/*` — CSS DCE, minification, and @import tests
- `internal/build/build_test.go` — full project build integration test
- `internal/plugin/jsplugin_test.go` — JS community plugin runtime (QuickJS)
- `internal/docfind/docfind_test.go` — in-process WASM index build + search
- `internal/build/goapi_test.go` — Go API sidecar build + serve tests

## The docs website

This site lives in `packages/web`. Docs content is markdown under
`content/docs/`. Rebuild it to verify plugin changes:

```sh
cd packages/web
krate build
```

## CI/CD

- `.github/workflows/ci.yml` — 3 platforms (Ubuntu, Windows, macOS); build,
  vet, test, build the examples project.
- `.github/workflows/release.yml` — builds per-platform binary packages via
  `scripts/build-platform-packages.mjs` and publishes them to npm on a `v*`
  tag.

## Updating the vendored docfind WASM

The docs search index is built by a vendored copy of Microsoft's docfind
(`packages/compiler/third_party/docfind`) compiled to WASM. If you change it:

```sh
rustup target add wasm32-unknown-unknown
node scripts/build-docfind.mjs
```

Commit the regenerated `internal/docfind/embedded/*.wasm` artifacts so the repo
builds without a Rust toolchain.
