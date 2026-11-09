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
| `krate dev [dir]` | Build + dev server (port 3000) + hot reload; restarts on config/env changes |
| `krate serve [dir]` | Build + HTTP server (production preview); `--no-build` serves the existing output |
| `krate start [dir]` | Serve the existing build output without rebuilding (production start) |
| `krate init [dir]` | Scaffold a new project (alias: `create`; via the npm wrapper) |
| `krate types [dir]` | Generate route/content TypeScript declarations only |
| `krate typecheck [dir]` | Generate types, then run `tsc --noEmit` without its errors about tier decorators |
| `krate go:init [dir]` | Set up `go.mod`/`go.work` + vendored helpers for Go intellisense |
| `krate check [dir]` | Build and run quality gates (a11y/SEO/perf); non-zero on failure |
| `krate fix [dir]` | Apply conservative, parse-gated source fixes (dry-run unless `--apply`) |
| `krate migrate nextjs [dir]` | Plan or apply a conservative Next.js migration |
| `krate inspect [dir]` | Build and print the compiled site model (`--json`) |
| `krate deploy [dir]` | Build, then write host adapter files (`--target`, `--static-only`) |
| `krate clean [dir]` | Remove build output and the compiler cache |
| `krate doctor [dir]` | Print a diagnostic summary of the project and toolchain |
| `krate upgrade [version]` | Upgrade the project's `@krate/*` packages to the latest or a given version (`--global`, `--dry-run`) |
| `krate info [dir]` | Print a paste-ready environment report for bug reports (`--json`) |
| `krate explain <code>` | Print the cause, the fix and a docs link for a diagnostic code |
| `krate config [dir]` | Print the fully resolved configuration (`--schema` for the JSON Schema) |
| `krate plugin add <pkg>` | Install a plugin and register it in the config |
| `krate mcp [dir]` | Start the MCP (Model Context Protocol) server (stdio, or `--http`) |
| `krate version` | Print the version |
| `krate help [command]` | Print usage; `krate <command> --help` prints one command's usage |

Flags that apply to more than one command: `--config <path>` (an explicit path
that does not exist fails the command with an error naming the path, rather than
falling back to the defaults; with no `--config`, a project without a config file
uses the defaults), `--out-dir <path>`,
`--mode <name>` (environment mode, like `KRATE_ENV`; selects `.env.<name>` and is
passed to a function-form `defineConfig`), `--verbose`, `--precompress`,
`--sourcemap`, `--no-dce`, `--json`, and `--apply` (for `fix` and `migrate`).

## `krate init`

Scaffolds a new project from `create-krate-app`. The command lives in the
`@krate/core` npm wrapper (not the native binary) so it works on every platform
and delegates to the same release channel as the running core package.

```sh
krate init my-app
krate create my-app            # alias
npx create-krate-app@latest my-app
```

| Flag | Description |
|------|-------------|
| `--use-npm` / `--use-pnpm` / `--use-yarn` / `--use-bun` | Choose the package manager |
| `--skip-install` (`--no-install`) | Skip dependency installation |
| `--no-git` | Do not initialize a git repository |

For a documentation site, use `create-krate-docs` instead.

## `krate build`

Builds the site into `outDir` (default `dist`). The build empties `outDir` first,
so it stops with `KR8350`, before deleting anything, when `outDir` is, or
contains, the project root, `pagesDir` or `publicDir` (for example `outDir: ".."`
or `"."`). A directory outside the project that holds none of those, such as
`../site-dist`, is fine.

```sh
krate build
krate build ./my-site
```

| Flag | Description |
|------|-------------|
| `--config <path>` | Path to a config file (default `krate.config.ts`). Relative paths resolve against the project directory; a path that does not exist is an error |
| `--out-dir <path>` | Override the output directory |
| `--watch` | Rebuild when files change |
| `--verbose` | Print diagnostic detail during the build (e.g. reactive validation warnings) |
| `--profile` | Print per-phase build timings and disk-cache hits/misses |
| `--sourcemap` | Emit source maps for generated JS (also disables the build cache) |
| `--no-dce` | Disable CSS/JS dead-code elimination |
| `--precompress` | Write `.br` + `.gz` siblings for compressible assets (same as `compress: { brotli: true, gzip: true }`) |
| `--mode <name>` | Set the environment mode (like `KRATE_ENV`); loads `.env.<name>` and is passed to a function-form `defineConfig` |

During the build the compiler:

1. Runs plugin `BeforeBuild` hooks (the docs plugin generates pages here).
2. Builds every page in parallel (SSR evaluation + hydration codegen).
3. Merges CSS, inlines `@import`s, folds duplicate declarations (always), and runs minification when enabled.
4. Writes hashed JS chunks, the shared runtime chunk, and `.krate/manifest.json`.
5. Copies `publicDir` assets, compiles API routes, middleware, and runtime components.

A file the build cannot write (a full disk, an unwritable `outDir`) fails the
build with a non-zero exit and an error naming the path, rather than leaving
HTML that links a stylesheet or script that does not exist.

## Build caching

`krate build` keeps a cross-run cache under `.krate/cache/build/`. On a rebuild,
any page whose inputs (source, imports, layouts, generated content, and config)
are byte-for-byte unchanged is replayed from the cache - bundling, compilation
and emit are skipped - which makes repeat and CI builds dramatically faster.

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

- Serves the site on port 3000 (configurable via `devServer.port`, which wins
  over `server.port` in dev), bound to `127.0.0.1` only. Use `devServer.host` to
  expose it on the network. If the port is taken, the next free one is used and
  the terminal says so.
- Watches source files, including new, renamed and deleted pages and
  directories, and rebuilds only what changed. `public/` edits are mirrored into
  the output immediately.
- Editing `krate.config.*`, `tailwind.config.*`, `tsconfig.json` or `.env*`
  reloads the settings and restarts the dev server in place (open tabs reload by
  themselves). A config that fails to load keeps the previous server running and
  shows the error in the overlay.
- Uses SSE hot reload to push updates to open tabs after a short (75 ms)
  debounce; stylesheet-only edits swap the CSS without reloading the page.
- Shows compilation errors inline via the dev error overlay. If the first build
  fails, the browser shows an error page with the overlay and recovers on the
  next successful build.
- Writes `.krate/dev-server.json` (URL and pid, removed on exit) and mirrors the
  console output, one timestamped line per line, to `.krate/dev.log`. The
  [MCP server](/docs/features/mcp/) uses them, and the loopback-only
  `/__krate/state` endpoint, to report build and browser errors to agents.

## `krate serve`

Builds the site and serves the production output over HTTP.

```sh
krate serve
krate serve --no-build     # serve the existing dist/ without rebuilding
```

Useful for previewing `dist/` exactly as a static host would serve it.
`preview` is an alias.

## `krate start`

Serves an existing build without rebuilding - the command to run in a
container or on a server after `krate build`. It exits with an error and a hint
when there is no build output.

```sh
krate build
krate start
```

### Port and host

`dev`, `serve` and `start` accept `--port <n>` and `--host <addr>`. For `serve`
and `start` the port is resolved as: `--port`, then `KRATE_SERVE_PORT`, then
`PORT`, then `server.port` / `devServer.port` (default 3000); `krate dev` uses
`--port` then the config. `KRATE_SERVE_HOST` sets the bind address the same
way. An invalid value (`--port abc`, `PORT=70000`) is an error. Internal
sidecars pick free ports automatically.

On `SIGINT`/`SIGTERM` the server drains in-flight requests (see
`server.shutdownTimeout`) and exits with status 0.

## `krate check`

Builds the site and runs the compiler-enforced quality gates
(accessibility, SEO, performance) against the emitted HTML. Exits non-zero
when a finding meets the configured `checks.failOn` severity (default
`error`) - ideal for CI.

```sh
krate check
```

See [Quality Checks](/docs/features/quality-checks/) for configuration and the
built-in rule list.

## `krate fix`

Applies safe codemods for diagnostics with an unambiguous rewrite. Dry-run by
default; `--apply` writes atomically. Files that fail to parse are skipped and
never partially rewritten. Fixes include `KR5002` lowercase event names on
built-in elements (`onclick` -> `onClick`) and `KR7125`, the `tsconfig.json`
`baseUrl` of a 0.4 project (`"./src"` -> `"."`).

```sh
krate fix
krate fix --apply
krate fix ./my-site --apply
```

See [Safe Fixes](/docs/guides/safe-fixes/) for the rewrite list and the manual
path for every other diagnostic.

## `krate migrate`

Plans a framework migration. Nothing is written until `--apply`; overwriting
existing targets requires `--force`. Every apply creates a rollback manifest
under `.krate/migrate/<framework>-<timestamp>/`.

```sh
krate migrate nextjs
krate migrate nextjs ./my-site
krate migrate nextjs --apply
krate migrate nextjs --apply --force
krate migrate nextjs --apply --verify
krate migrate rollback .krate/migrate/nextjs-20261027-190904
```

| Flag | Description |
|------|-------------|
| `--apply` | Write the plan atomically |
| `--verify` | After applying, build, run checks, and diff the route inventory |
| `--force` | Allow overwriting existing target files |
| `--update-package` | Add Krate scripts and dependencies to `package.json` |
| `--report <path>` | Write the plan as JSON before applying |
| `--json` | Print the plan (or apply result) as JSON |

The Next.js migrator copies App Router and Pages Router pages, layouts, loading
and 404 files, and API route handlers into `src/pages` and `src/api`. App-local
relative imports are copied under `src/migrated/` and their specifiers are
rewritten. It keeps `"use client"` (Krate honors it as a client boundary inside
server component trees) and strips default imports for `next/link`,
`next/image`, and `next/head` (Krate provides those as globals), keeps route
groups, `@slot` directories, `default.tsx` and intercepting `(.)x` segments in
place (the file router understands them), turns `template.tsx` into
`_template.tsx`, maps `next/cache` to `@krate/runtime/server`, translates static
`next.config.*` redirects/rewrites/headers/basePath/images, maps static root metadata to
`site` config, wraps static page metadata in `<Head>`, and rewrites
`middleware.ts` to the web `Request`/`Response` API, including static matchers
and cookie/IP/geo shims. `next/navigation` hooks are generated into a
compatibility helper on the Krate router (`notFound`/`redirect` get a hook-free
server helper), `next/headers` maps to `@krate/runtime/server`, and common
`next/server` API helpers become `Request`/`Response`, simple `next/dynamic`
loaders become React `lazy()`, and `next/font/google` imports use a generated
helper/stylesheet. It generates
`krate.config.ts` when missing, merges missing Next-derived keys into an existing
config, merges/generates `tsconfig.json`, inlines `NEXT_PUBLIC_*` values, and
reports everything it will not rewrite. With
`--update-package` it also adds Krate scripts and dependencies while leaving the
Next.js dependencies in place for verification. Existing targets, including
`package.json` with `--update-package`, require `--force`. It never deletes the
original Next.js tree. See [Migrating from Next.js](/docs/guides/migrating-from-nextjs/).

## `krate inspect`

Builds the site and prints its machine-readable model - every route with its
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
`krate://graph` resource - see [MCP Server](/docs/features/mcp/).

## `krate config`

```sh
krate config [dir] [--mode <mode>] [--config <path>] [--schema]
```

Prints the fully resolved configuration (defaults applied, `site` folded into
`seo`, plugin options, absolute paths) as JSON. `--schema` prints a JSON Schema
for `krate.config.*` instead; builds also write `.krate/config.schema.json` so
editors can use it via `$schema`. Config files may be `krate.config.ts`, `.js`,
`.mjs`, or `.json`.

## `krate deploy`

Builds the site and writes host adapter files so `outDir` can be uploaded as-is:

```sh
krate deploy --target netlify
krate deploy --target vercel
krate deploy --target cloudflare
krate deploy --target gh-pages
krate deploy --target docker
krate deploy --target node
```

| Target | Files written |
|--------|---------------|
| `netlify`, `cloudflare` | `_redirects` (from `redirects`/`rewrites`), `_headers` (from `headers`) |
| `vercel` | `vercel.json` at the project root (redirects, rewrites, headers, `outputDirectory`) |
| `gh-pages` | `.nojekyll`, `404.html` |
| `docker` | `Dockerfile` + `.dockerignore` at the project root (runs `krate serve`) |
| `node` | `dist/.krate/server/server.mjs`, a plain Node server (SSR, ISR, API routes, actions, middleware; no Go binary) |

For a project with server features, `vercel`, `netlify` and `cloudflare` write
the server as well: `.vercel/output/` (Build Output API v3, with a Node.js
function), `netlify/functions/krate.mjs` + `netlify/server/krate-server.mjs` + `netlify.toml`, and
`.krate/deploy/cloudflare/worker.mjs` + `wrangler.jsonc`.

Go API routes, Go components and a custom API sidecar cannot run on the
serverless targets: `krate deploy` fails naming them (use `--target docker`).
Static-only hosts (`gh-pages`) fail for projects with server features unless you
pass `--static-only`, which publishes only the static pages.

An unknown `--target` fails before the build starts, listing the valid targets.

See [Deployment](/docs/guides/deployment/) for details.

## `krate doctor`

Prints the krate and toolchain versions, the resolved config, runtimes, and the
cache directory. It exits with status 1 when the config does not load or the
cache directory cannot be written, so it can gate a CI step; missing optional
tools only print. It also warns when the installed `@krate/core` and the running
binary are different versions.

`krate doctor --fix` repairs what it can and says what it did:

| Problem | Fix |
|---------|-----|
| Installed `@krate/core` is older than the running binary | Runs [`krate upgrade`](#krate-upgrade) to the binary's version. A newer package is not downgraded; use `krate upgrade --global` to update the binary |
| `.krate` or the output directory is missing from `.gitignore` | Appends the missing entries (only inside a git repository) |
| The `.krate` page cache was filled by another krate version, or has unreadable entries | Removes `.krate/cache` |

Without `--fix`, doctor lists the issues it could fix. A fix that fails makes the
command exit with status 1.

## `krate upgrade`

Moves every `@krate/*` package your `package.json` declares to one version, so
the packages never drift apart:

```sh
krate upgrade                 # latest
krate upgrade 0.5.1           # a specific version
krate upgrade next            # a dist-tag
krate upgrade --dry-run       # show the plan and the commands, change nothing
krate upgrade --global        # upgrade the globally installed CLI instead
```

It resolves a dist-tag such as `latest` with the npm registry (the one in
`npm_config_registry`, else the public registry), prints each package's installed
version next to the target, and runs your package manager (found from the
`packageManager` field or the lockfile): `npm install`, `pnpm add`, `yarn add` or
`bun add`, once for dependencies and once for dev dependencies. In a pnpm
workspace root it adds `-w`. Packages already at the target are left alone. A
project that declares no `@krate/*` package is pointed to `--global`.

Passing a directory (`krate upgrade ./site`) targets another project. See
[Upgrading](/docs/guides/upgrading/) for what changes between versions.

## `krate info`

Prints a Markdown report to paste into a bug report: the krate version, OS and
architecture, Go and Node versions, the package manager and its version, the
installed `@krate/*` package versions, and a short config summary (output mode,
runtime, directories, base path, plugin names).

```sh
krate info
krate info --json
```

The report holds no environment variables and no config values beyond those
structural settings, and the home directory is shown as `~`. A config that fails
to load is reported instead of failing the command.

## `krate explain`

Prints what a diagnostic code means and how to fix it, with a link to its entry in
the [diagnostics reference](/docs/reference/diagnostics/):

```sh
krate explain KR7110
krate explain 7110 --json
```

The code is case-insensitive and the `KR` prefix is optional. An unknown code
fails and lists the nearest registered codes. `--json` prints the code, title,
explanation and docs URL as JSON.

## `krate clean`

Removes the output directory (`dist`) and the compiler cache (`.krate/cache`),
and prints how much space each removal freed:

```sh
krate clean           # dist and .krate/cache
krate clean --dist    # only the build output
krate clean --cache   # only the compiler cache
krate clean --all     # dist and the whole .krate directory
```

`--all` also removes the generated types, sidecar manifests and run logs under
`.krate`; the next `krate dev`, `build` or `types` writes the types again. Do not
run it while `krate dev` is running.

`krate clean` refuses to run when `outDir` is, or contains, the project root,
`pagesDir` or `publicDir` (for example `outDir: "."`), so a misconfigured
output directory cannot delete your sources. `krate build` applies the same check.

## `krate go:init`

Sets up the Go workspace so editors resolve the helper imports
(`github.com/kratejs/krate/packages/compiler/goext/...`) for Go API routes and
Go components. It:

- vendors the helper module under `.krate/goext`,
- writes a project `go.mod` (only when one is absent) and a `go.work`,
- adds `.krate/types` to your `tsconfig.json` `include`.

```sh
krate go:init
```

Safe to run repeatedly; it never overwrites an existing `go.mod`, `go.work`, or
user-authored workspace. See
[Go Components](/docs/features/go-components/) and
[API Routes](/docs/features/api-routes/).

## `krate version`

Prints the version of the native compiler binary (`krate v<version>`), which is
the code that actually runs. `krate --version` and `krate -v` do the same. When
it differs from the `@krate/core` npm package, a warning goes to stderr; reinstall
to get the matching platform package.

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
  .krate/manifest.json       # page + SSR/ISR metadata (build metadata, not published)
  _krate/images/...          # processed <Image> variants
```
