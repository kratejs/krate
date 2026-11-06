---
title: Environment Variables
description: .env files, precedence, and process.env access.
order: 11
---

# Environment Variables

Krate loads `.env` files at build and serve time and makes the resolved values
available to everything that runs server-side via `process.env`.

## File names and precedence

Krate reads, in increasing precedence:

| File | When loaded |
|------|-------------|
| `.env` | Always (base values) |
| `.env.<mode>` | Always, after `.env` |
| `.env.local` | Always, after `.env.<mode>` |
| `.env.<mode>.local` | Always, last (highest precedence) |

Later files override earlier ones. Values already present in the shell
environment always win over every file (dotenv `override=false` semantics),
which is how you override config at deploy time without editing files.

The mode depends on the command:

| Command | Mode |
|---------|------|
| `krate dev` | `development` |
| `krate build` | `production` |
| `krate serve` | `production` |

`NODE_ENV` or `KRATE_ENV` overrides the default mode.

Files named `.env.local` and `.env.*.local` (your local, machine-specific
overrides) are listed in the `.gitignore` that `create-krate-app` generates so
secrets are not committed; add them to the `.gitignore` of an existing project. Keep non-secret defaults in committed `.env` files.

## File syntax

```
# A comment line
APP_NAME=krate-examples
export APP_MODE=production   # optional `export ` prefix is stripped
SECRET="double quoted"       # quotes are stripped
PATH_VAR=${BASE}/sub         # $VAR / ${VAR} expansion
LITERAL='$HOME'              # single-quoted: no expansion
INLINE=value # trailing comment after whitespace
```

- `KEY=VALUE` only; keys are trimmed of surrounding whitespace.
- `#` starts a comment on its own line; a `#` preceded by whitespace inside an
  unquoted value trims the rest of the line.
- `$VAR` and `${VAR}` expand to the value of `VAR` (in-file definitions are
  chained first, then the shell environment). Missing variables expand to the
  empty string.
- Single-quoted values are literal - no expansion - matching dotenv.

See the `krate-examples` project for a working setup: `examples/.env` and
`examples/.env.production`, exercised by the `src/api/env.ts` route at
`/api/env`.

## Where variables are visible

`.env` values reach `process.env` in every server-side execution context:

- **API routes** - `src/api/*.ts` (embedded QuickJS) and `src/api/*.go` (the
  Go sidecar subprocess).
- **Plugins** - Go plugin hooks and JS/TS community plugins running in
  QuickJS.
- **SSR / ISR / streaming pages** - the renderer sidecar subprocess.
- **`generateStaticParams`** - the embedded or bundled-runtime evaluation that
  computes statically generated params.

In every case, on every runtime (embedded QuickJS, Node, Bun, Deno) and in both
`krate dev` and `krate serve`, `process.env` holds the shell environment plus the
`.env` files, with the shell winning, and `NODE_ENV` set to `"development"` or
`"production"` when the shell does not set it. This also covers middleware and
the embedded `generateStaticParams` and metadata evaluation at build time.
`FOO=bar krate serve` therefore makes `process.env.FOO` readable in an API route
whichever runtime serves it. Shell variables never reach client output; only the
`KRATE_PUBLIC_` rule below inlines values.

## Client-side variables

Variables whose name starts with `KRATE_PUBLIC_` are inlined as string literals
at build time into both the statically rendered HTML and the browser
JavaScript, so a page can read them with either spelling:

```tsx
<a href={import.meta.env.KRATE_PUBLIC_API_URL}>API</a>
const base = process.env.KRATE_PUBLIC_API_URL;
```

Values come from the `.env` files described above and from the shell
environment (the shell wins). These build constants are always available, with
no prefix: `import.meta.env.MODE` (the environment mode), `DEV`, `PROD`,
`BASE_URL` (the base path with a trailing slash) and `SSR` (always `false` in
client and static code); `process.env.NODE_ENV` is `"development"` or
`"production"`.

Reading any other `process.env.X` or `import.meta.env.X` in a client component
or a statically rendered page is not possible: the value is never written to
output and the read is `undefined`. Krate reports it as warning
[KR8310](/docs/reference/diagnostics/) with the file, line and column.
Server components, API routes, plugins and server actions keep full access to
`process.env`.

## Security

Environment values are **build/serve-time only** and are **never emitted into
client HTML, JavaScript, or hydration payloads**, except variables you opt in
with the `KRATE_PUBLIC_` prefix. Do not put secrets in values
you render into a page component tree; only server-only contexts (API routes,
server components, plugins) may read them. Krate logs only that a given env
file was found/loaded - never its values.

## Compiler tuning variables

These are read from the shell environment (not `.env`) and tune robustness
limits. All are optional.

| Variable | Default | Effect |
|----------|---------|--------|
| `KRATE_DEBUG` | unset | Re-throw internal panics with their original stack instead of reporting them as build errors. |
| `KRATE_EXEC_TIMEOUT` | unset | Fallback timeout for external processes without a specific override (Go duration, e.g. `90s`). |
| `KRATE_GOAPI_TIMEOUT` | `15m` | Timeout for `go build` of the Go host (API routes + components). |
| `KRATE_GOAPI_TIDY_TIMEOUT` | `10m` | Timeout for `go mod tidy` when `go.tidy` is enabled. |
| `KRATE_GIT_TIMEOUT` | `60s` | Timeout for `git` reads used by `lastUpdated`/sitemap. |
| `KRATE_INSTALL_TIMEOUT` | `30m` | Timeout for `krate plugin add` package installs. |
| `KRATE_JS_MEMORY_MB` | `32` | Embedded QuickJS heap limit. |
| `KRATE_JS_STACK_KB` | `256` | Embedded QuickJS stack limit. |
| `KRATE_JS_TIMEOUT` | `30s` | Embedded QuickJS eval timeout. |
| `KRATE_PLUGIN_MAX_FILES` | `10000` | Max files a single plugin hook may emit. |
| `KRATE_PLUGIN_MAX_FILE_BYTES` | `67108864` | Max size of a single plugin-emitted file. |
| `KRATE_ALLOW_UNSAFE_OUTDIR` | unset | Allow an `outDir` that is a filesystem root or the project root (normally refused). |

Set a timeout to `0` (or `off`) to disable it. Other operational variables
include `KRATE_NO_BUILD_CACHE` (skip the disk build cache) and
`KRATE_REQUIRE_E2E=1` (turn environment-missing test skips into failures).

Browser and performance gates:

| Variable | Default | Effect |
|----------|---------|--------|
| `KRATE_BROWSER` | unset | Selects `chromium`, `firefox`, or `webkit` for browser conformance and the React differential. |
| `KRATE_HYDRATION_BUDGET_MS` | `1500` | Maximum island hydration time from navigation start in the browser conformance gate. CI uses `2500`. |
| `KRATE_REACT_EXIT` | unset | Fail the React differential on any P0/P1 divergence (CI sets this). |
| `KRATE_REACTBROWSER_REPORT` | unset | Write the React differential report to this path. |
