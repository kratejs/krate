---
title: CLI Reference
description: Reference for every krate CLI command.
order: 4
---

# CLI Reference

The `krate` CLI is the single entry point for building and serving Krate sites.

| Command | Description |
|---------|-------------|
| `krate build [dir]` | Production build |
| `krate dev [dir]` | Build + dev server (port 3000) + hot reload |
| `krate serve [dir]` | Build + static HTTP server (production preview) |
| `krate types [dir]` | Generate route/content TypeScript declarations only |
| `krate check [dir]` | Build and run quality gates (a11y/SEO/perf); non-zero on failure |
| `krate inspect [dir]` | Build and print the compiled site model (`--json`) |
| `krate deploy [dir]` | Build, then write host adapter files (`--target`) |
| `krate clean [dir]` | Remove build output and the compiler cache |
| `krate mcp [dir]` | Start the MCP (Model Context Protocol) server over stdio |
| `krate version` | Print the version |

## `krate build`

Builds the site into `outDir` (default `dist`).

```sh
krate build
krate build ./my-site
```

| Flag | Description |
|------|-------------|
| `--config <path>` | Path to a config file (default `krate.config.ts`) |
| `--out-dir <path>` | Override the output directory |
| `--watch` | Rebuild when files change |
| `--verbose` | Print diagnostic detail during the build (e.g. reactive validation warnings) |
| `--profile` | Print per-phase build timings and disk-cache hits/misses |
| `--sourcemap` | Emit source maps for generated JS (also disables the build cache) |
| `--no-dce` | Disable CSS/JS dead-code elimination |

During the build the compiler:

1. Runs plugin `BeforeBuild` hooks (the docs plugin generates pages here).
2. Builds every page in parallel (SSR evaluation + hydration codegen).
3. Merges CSS, inlines `@import`s, folds duplicate declarations (always), and runs minification when enabled.
4. Writes hashed JS chunks, the shared runtime chunk, and `manifest.json`.
5. Copies `publicDir` assets, compiles API routes, middleware, and runtime components.

## Build caching

`krate build` keeps a cross-run cache under `.krate/cache/build/`. On a rebuild,
any page whose inputs (source, imports, layouts, generated content, and config)
are byte-for-byte unchanged is replayed from the cache — bundling, compilation
and emit are skipped — which makes repeat and CI builds dramatically faster.

The cache is transparent and always produces identical output. It is disabled
automatically when a page can't be faithfully replayed:

- when `sourcemap` is enabled,
- when a native plugin registers per-page hooks (`AfterParse`,
  `AfterMarkdownParse`, `AfterRender`, `AfterPage`), or
- when any community (JS/TS) plugin is configured, since its hooks can't be
  statically verified.

Set `KRATE_NO_BUILD_CACHE=1` to force a clean build, and run `krate clean` to
remove the cache.

## `krate dev`

Starts a build + HTTP dev server with hot reload.

```sh
krate dev
```

- Serves the site on port 3000 (configurable via `devServer.port`).
- Watches source files and rebuilds changed pages.
- Uses SSE hot reload to push updates to open tabs.
- Shows compilation errors inline via the dev error overlay.

## `krate serve`

Builds the site and serves the production output over HTTP.

```sh
krate serve
```

Useful for previewing `dist/` exactly as a static host would serve it.

## `krate check`

Builds the site and runs the compiler-enforced quality gates
(accessibility, SEO, performance) against the emitted HTML. Exits non-zero
when a finding meets the configured `checks.failOn` severity (default
`error`) — ideal for CI.

```sh
krate check
```

See [Quality Checks](/docs/features/quality-checks/) for configuration and the
built-in rule list.

## `krate inspect`

Builds the site and prints its machine-readable model — every route with its
render mode, source file, dependency/dependent edges, and emitted output files:

```sh
krate inspect            # human-readable listing
krate inspect --json     # machine-readable (pipeable; build chatter suppressed)
```

The `--json` form emits a single JSON document on stdout:

```json
{
  "root": ".",
  "routes": [
    {
      "route": "/about",
      "source": "src/pages/about.tsx",
      "mode": "ssg",
      "built": true,
      "dependencies": ["src/pages/about.tsx", "src/_layout.tsx"],
      "outputs": ["about/index.html"],
      "bytes": 5120
    }
  ],
  "files": { "src/pages/about.tsx": ["/about"] }
}
```

`files` maps each depended-on source file to the routes that reference it (what
links here). The same model backs the MCP `explain` tool and the
`krate://graph` resource — see [MCP Server](/docs/features/mcp/).

## `krate deploy`

Builds the site and writes host adapter files so `outDir` can be uploaded as-is:

```sh
krate deploy --target netlify
krate deploy --target vercel
krate deploy --target cloudflare
krate deploy --target gh-pages
```

| Target | Files written |
|--------|---------------|
| `netlify`, `cloudflare` | `_redirects` (from `redirects`/`rewrites`) |
| `vercel` | `vercel.json` (redirects + rewrites) |
| `gh-pages` | `.nojekyll`, `404.html` |

See [Deployment](/docs/guides/deployment/) for details.

## `krate clean`

Removes the output directory (`dist`) and the compiler cache (`.krate/cache`):

```sh
krate clean
```

## `krate version`

Prints the compiler version:

## `krate mcp`

Starts the agent-native MCP server over stdio, exposing the compiler to AI
agents (tools, resources, and structured diagnostics).

```sh
krate mcp
krate mcp ./my-site
```

See [MCP Server](/docs/features/mcp/) for client setup and the tool/resource
reference.

## Build output

A production build produces:

```
dist/
  index.html                 # pre-rendered pages (one per route)
  docs/...                   # plugin-generated pages (e.g. the docs site)
  index.<hash>.js            # per-page hydration bundles
  styles.<hash>.css          # merged, deduplicated CSS
  chunks/runtime.<hash>.js   # shared runtime chunk
  manifest.json              # page + SSR/ISR metadata
  _krate/images/...          # processed <Image> variants
```
