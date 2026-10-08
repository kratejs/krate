---
title: Environment Variables
description: .env files, precedence, and process.env access.
order: 10
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
overrides) are covered by the repo's `.gitignore` so secrets are not
committed. Keep non-secret defaults in committed `.env` files.

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
- Single-quoted values are literal — no expansion — matching dotenv.

See the `krate-examples` project for a working setup: `examples/.env` and
`examples/.env.production`, exercised by the `src/api/env.ts` route at
`/api/env`.

## Where variables are visible

`.env` values reach `process.env` in every server-side execution context:

- **API routes** — `src/api/*.ts` (embedded QuickJS) and `src/api/*.go` (the
  Go sidecar subprocess).
- **Plugins** — Go plugin hooks and JS/TS community plugins running in
  QuickJS.
- **SSR / ISR / streaming pages** — the renderer sidecar subprocess.
- **`generateStaticParams`** — the `npx tsx` subprocess that computes
  statically generated params.

In every case the embedded/child process sees the merged map: shell env
overridden only where files define a variable the shell does not.

## Security

Environment values are **build/serve-time only** and are **never emitted into
client HTML, JavaScript, or hydration payloads**. Do not put secrets in values
you render into a page component tree; only server-only contexts (API routes,
server components, plugins) may read them. Krate logs only that a given env
file was found/loaded — never its values.