# AGENTS.md

Guidance for AI agents and human contributors working on **Krate** itself (the
monorepo). For the AGENTS.md that ships inside scaffolded apps, see
`packages/create-krate-app/templates/app/AGENTS.md`.

Krate is a Go-native web framework. A single Go binary (`krate`) lexes, parses
and bundles TSX into static HTML plus a tiny signal-based hydration runtime, with
optional SSR/ISR/streaming and JS/Go API routes served via Node (`node`/`bun`/
`deno`) and Go sidecars. There is no required bundler/JS toolchain on the client:
the compiler emits plain HTML/CSS/JS.

Notable internal subsystems (`packages/compiler/internal/`):

- `lexer`, `parser`, `astprint`, `astjson` - TSX/JSX front end and AST tooling.
- `bundler`, `resolver`, `css`, `csssignals`, `irtree`, `renderer` - module
  graph, CSS pipeline, IR, and codegen.
- `build` - the build/dev/serve orchestration (`Builder`), dev watcher, SSR and
  API sidecars, `krate inspect`/`check`, deploy adapters, `gitinfo`.
- `jseval` - esbuild + embedded QuickJS (`modernc.org/quickjs`) evaluation, used
  to run `krate.config.ts` and `tailwind.config.*` in-process (no `tsx`).
- `diag`, `escape`, `config`, `check`, `mcp`, `plugin`/`pluginapi`, `deploy`.

## Prerequisites

- **Go** - the version in `packages/compiler/go.mod`.
- **Node.js 20+** and **pnpm** - for the JS packages, the compiler's edge-case
  sidecars (`generateStaticParams` still shells out to `npx tsx`), and the
  benchmark harness.

## Repository layout

```
packages/
  compiler/            Go compiler + CLI (`krate`)
    cmd/krate/         CLI entry point
    internal/          lexer, parser, bundler, renderer, build, css, config,
                       jseval, check, mcp, plugin, deploy, diag, escape, ...
    ast/               AST types (public, used by pluginsdk)
    pluginsdk/         Go plugin SDK
  runtime/             @krate/runtime - signals, DOM, router, server renderer (TS)
  components/          @krate/components - component library (TSX)
  core/                @krate/core - npm CLI wrapper + platform binaries
  plugin/              @krate/plugin - plugin authoring types (TS)
  base-docs-theme/     @krate/base-docs-theme - default docs theme
  web/                 the krate.js.org docs site (also a build smoke test)
  create-krate-app/    app scaffolder + templates
  create-krate-docs/   docs-site scaffolder + templates
  benchmark/           cross-framework build/API benchmark harness + fixtures
examples/              example app used by integration tests
scripts/               repo tooling (docs sync, platform packages, versioning)
```

## Commands

```sh
# From repo root:
pnpm dev               # run the compiler in dev against examples/
pnpm build             # sync docs + build every JS package
pnpm test              # sync docs + JS (Vitest) tests + go test ./...
pnpm test:js           # JS tests only (Vitest, per package)
pnpm typecheck         # tsc --noEmit across runtime/components/core/plugin/web
pnpm vet               # sync docs + go vet ./...
pnpm sync:docs         # regenerate embedded framework docs
pnpm check:docs        # fail if embedded docs are stale

# From packages/compiler:
go build ./...
go test ./...                      # full suite (includes examples/ integration)
go test -race ./internal/build ./internal/bundler   # concurrency-sensitive
go vet ./...
gofmt -l .                         # must be empty
```

`KRATE_REQUIRE_E2E=1` turns environment-missing test skips (Node, built runtime,
docs) into hard failures. CI sets it on all OSes.

## CI

`.github/workflows/ci.yml` runs, on every PR/push:

- `go build`, `go vet`, `gofmt -l`, `golangci-lint` (config `.golangci.yml`).
- `go test -race` with coverage on ubuntu, plus a plain `go test ./...` matrix on
  macOS and Windows (both with `KRATE_REQUIRE_E2E=1`).
- `pnpm run typecheck`, `pnpm run test:js`, `node scripts/sync-krate-docs.mjs
  --check`, a 30s `gofmt`/docs gate, informational benchmarks, and 30s fuzzers
  (`FuzzLexer`, `FuzzParse`).

`.github/workflows/release.yml` gates publishing on `go build/vet/test` and
`check:docs`, and syncs embedded docs before packaging binaries.

## Workflow

- New compiler/lexer/parser/bundler/renderer/CSS behavior ships with **Go unit
  tests** (and, where relevant, a fixture under `examples/`).
- New runtime or component behavior ships with a **Vitest** test
  (`packages/runtime/src/*.test.ts`, `packages/components/test/*.test.tsx`) - run
  `pnpm test:js`; run `pnpm typecheck` for any TS change.
- User-facing behavior changes update the docs under
  `packages/web/src/content/docs/`, then `pnpm sync:docs` + `pnpm check:docs`.
- Do not commit `dist/`, `.krate/`, or the generated embedded docs
  (`packages/compiler/internal/kratedocs/docs`).
- The `TODO*.md` / `PLAN.md` files at the repo root are working planning notes;
  they are intentionally **not committed**.

## Conventions

- **Errors:** use `internal/diag` (a `Diagnostic` carries `file:line:col` +
  source + an actionable `Hint`). Prefer a specific hint over a bare error;
  in the parser use `p.errWithMsg(msg, hint)`.
- **Escaping:** all HTML/JS interpolation goes through `internal/escape`. Never
  build HTML/JS strings from raw user content.
- **Paths:** validate anything derived from content against the project root
  (`pluginapi.WithinRoot`).
- **Config:** new keys go in `internal/config` and are added to
  `knownTopLevelKeys`/`knownNestedKeys` in `validate.go` and parsed in
  `tsconfig.go`, so unknown-key warnings stay accurate.
- **Comments:** plain ASCII text only. No box-drawing separators (`----`),
  arrows (`->`), smart quotes, emoji, or mojibake; no decorative banner lines.
  Write short, meaningful comments; omit them when the code is self-evident.
- **Dependencies:** prefer the standard library and the single-binary story;
  avoid adding runtime dependencies.

## See also

- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [Krate docs](https://krate.js.org/docs/)
