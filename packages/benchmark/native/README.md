# Native shell benchmark

Krate Native, Electron and Tauri v2 running the same small app (a list of 100
items and a button that calls host code). The harness builds each app, runs it
N times and measures cold start (spawn to first contentful paint), idle memory
over the whole process tree, installer sizes and on-disk size.

```sh
node packages/benchmark/native/bench.mjs --runs 5 --idle 5   # Linux: under xvfb-run -a
node --test packages/benchmark/native/bench.test.mjs         # harness unit tests
```

Results go to `results/native-bench-<os>-<arch>.{json,md}` (not committed).
Method, caveats, Linux numbers and the CI workflow are in
`packages/web/src/content/docs/native/benchmarks.md`.

The three apps are standalone, not pnpm workspace packages: `krate/` links
`@krate/*` from the monorepo at build time, `electron/` and `tauri/` are
installed with `npm ci` from their own lockfiles (versions are pinned).
