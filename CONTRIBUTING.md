# Contributing to Krate

Thanks for your interest in contributing to Krate! We welcome bug reports,
feature requests, documentation improvements, and pull requests.

## Getting Started

Krate is a monorepo built with pnpm workspaces plus a Go compiler.

| Package | Location | Description |
|---------|----------|-------------|
| `@krate/core` | `packages/core` | npm wrapper + CLI entry point |
| `@krate/runtime` | `packages/runtime` | Client-side runtime (signals, hydration, router) |
| `@krate/components` | `packages/components` | Built-in component library |
| Compiler | `packages/compiler` | Go compiler (lexer, parser, bundler, renderer) |
| Docs example | `examples/` | Full docs site demonstrating the framework |

### Prerequisites

- [Go 1.26+](https://go.dev/dl/)
- [Node.js 20+](https://nodejs.org/)
- [pnpm](https://pnpm.io/)

### Setting up the repo

```sh
# Install JS dependencies (runtime, components, examples)
pnpm install

# Build the compiler
cd packages/compiler
go build -o krate ./cmd/krate/
```

### Running tests

```sh
# Go compiler tests
cd packages/compiler
go test ./...

# Build the docs example to smoke-test the full pipeline
cd ../..
./packages/compiler/krate build examples   # (or krate.exe on Windows)
```

## Project Layout

```
packages/
  compiler/    Go compiler (lexer, parser, bundler, renderer, build pipeline)
  runtime/     Client-side runtime — signals, hydration, SPA router, resources
  components/  Built-in UI components (shadcn/ui-style)
  core/       npm CLI wrapper that downloads/executes the platform binary
  core-*/      Per-platform binaries used by @krate/core (built in CI, not committed)
examples/      Docs + feature demo site (also used for integration tests)
```

## Development Workflow

1. Fork the repository and create a feature branch.
2. Make your changes. Keep them focused and small.
3. Add or update tests. New renderer/parser/bundler behavior should ship with
   Go unit tests; new runtime behavior should be covered by the docs example.
4. Run `go vet ./...` and `go test ./...` from `packages/compiler/`.
5. Run `pnpm build` from the repo root to verify the JS packages compile.
6. Open a pull request against `main`.

## Pull Request Guidelines

- One logical change per PR. Keep the diff reviewable.
- Use a descriptive title and reference related issues.
- Update documentation (README, AGENTS.md) if user-facing behavior changes.
- Verify CI passes: Go vet/tests, JS package build, and the `examples/` build.

## Coding Conventions

- **Go**: Follow standard `gofmt` formatting. Prefer small, single-purpose
  functions (~10-50 lines). The renderer pipeline is intentionally split into
  typed IR nodes — keep it that way rather than merging slot types.
- **TypeScript**: Prefer signals and plain functions. No classes in the runtime.
- **No comments unless they explain *why***, not what.

## Report a Security Issue

Please do **not** open a public issue for security problems. See
[SECURITY.md](SECURITY.md) for how to report vulnerabilities privately.

## Getting Help

- Open an issue for bugs or feature requests.
- Use the discussion area for questions about usage.
