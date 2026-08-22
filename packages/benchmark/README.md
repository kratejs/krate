# @krate/benchmark

Benchmarks for Krate: build time and API throughput versus other web
frameworks. The results are rendered to `results/latest.md` (Markdown) and
`results/latest.json` (machine-readable).

## What's measured

1. **Build time** — a cold production build of a single page (reactive counter +
   a 100-item list) across Krate, Next.js, Vite + React, Astro, SvelteKit, and
   Remix. Krate's fixture uses a TypeScript API route (mirroring Next.js's route
   handler). Dependencies are installed once; the median of N timed runs is
   reported, along with the emitted output size.

2. **API throughput** — `GET /api/json` served by Krate's **Go API routes**
   (compiled to a native Go sidecar), Krate's **TypeScript API routes** (served
   by `krate serve`, which runs the compiled route on its embedded JS runtime),
   and Next.js route handlers — plus two baselines: a plain Node `http` server
   and a plain Go `net/http` server. Each server is warmed up and load-tested
   with a fixed concurrency of keep-alive connections; requests/second and
   p50/p99 latency are reported.

> **Note on installs:** dependencies are installed with `--ignore-scripts`
> (lifecycle scripts are not needed by any fixture), which also avoids npm
> 11.16+ / 12 script-blocking (`EALLOWSCRIPTS`). Next.js fixtures declare their
> TypeScript devDependencies explicitly so `next build` never triggers its own
> nested package install.

## Prerequisites

- **Node.js 20+** (runs the harness and the JS fixtures)
- **Go 1.25+** (builds the Krate CLI, the Go API sidecar, and the Go baseline)
- Network access on the first run (dependencies for every fixture are installed
  automatically)

## Quick start

```bash
# full build + API matrix (installs dependencies on first run)
pnpm --filter @krate/benchmark bench

# individual commands
node src/run.mjs build            # build-time only
node src/run.mjs api              # API throughput only
node src/run.mjs report           # print latest.md to stdout
```

## Options

| Flag | Description |
|------|-------------|
| `--filter <id>` | Run one target: `krate`, `nextjs`, `vite`, `astro`, `sveltekit`, `remix`, `krate-go`, `krate-ts`, `node-http`, `go-nethttp` |
| `--runs N` | Build timing runs (default `3`) |
| `--duration ms` | API load-test duration (default `5000`) |
| `--connections N` | API load-test concurrency (default `32`) |
| `--pm <name>` | Installer: `npm` (default), `pnpm`, `yarn`, or `bun` |

## Methodology

- **Fair comparison** — the Krate CLI is compiled once (cached in
  `.krate-bench/`) and reused for every timed run, so its own Go compilation is
  excluded from build times, just like the preinstalled binaries of the other
  frameworks.
- **Cold builds** — every timed build starts from a clean output directory.
- **Stable samples** — the median of several runs is reported to reduce noise;
  each API server is warmed up before the timed window. When `autocannon` is
  installed (a devDependency), it drives the load test; otherwise a built-in
  keep-alive client is used.
- **Apples-to-apples payload** — every API endpoint returns the same
  `{"message":"hello","id":42}` JSON body.

## Fixtures

Each framework has a minimal, committed fixture under `fixtures/`:

```
fixtures/
  krate/        Krate site with a signal counter + a TypeScript API route
  krate-go/     Krate site with a Go API route (for the API throughput benchmark)
  next/         Next.js app router with a client counter + a route handler
  vite/         Vite + React SPA
  astro/        Astro static site
  sveltekit/    SvelteKit static site
  remix/        Remix (Vite) app
  node-http/    Node http baseline (API)
  go-nethttp/   Go net/http baseline (API)
```

## Output

```
results/
  latest.md    Markdown report (tables)
  latest.json  Raw results with toolchain + hardware metadata
```

## Disclaimer

Absolute numbers depend heavily on the machine, toolchain versions, and
OS. Treat them as relative comparisons, not fixed facts, and re-run on your own
hardware before drawing conclusions.
