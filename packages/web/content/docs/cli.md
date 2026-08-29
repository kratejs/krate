---
title: CLI Reference
order: 4
---

# CLI Reference

The `krate` CLI is the single entry point for building and serving Krate sites.

| Command | Description |
|---------|-------------|
| `krate build [dir]` | Production build |
| `krate dev [dir]` | Build + dev server (port 3000) + hot reload |
| `krate serve [dir]` | Build + static HTTP server (production preview) |
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

During the build the compiler:

1. Runs plugin `BeforeBuild` hooks (the docs plugin generates pages here).
2. Builds every page in parallel (SSR evaluation + hydration codegen).
3. Merges and deduplicates CSS, inlines `@import`s, and runs minification.
4. Writes hashed JS chunks, the shared runtime chunk, and `manifest.json`.
5. Copies `publicDir` assets, compiles API routes, middleware, and runtime components.

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

## `krate version`

Prints the compiler version:

```sh
krate version
```

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
