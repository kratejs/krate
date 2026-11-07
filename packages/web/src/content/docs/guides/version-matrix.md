---
title: Version Matrix
description: Supported React, Preact, Node, Bun, Deno, Go, and browser versions.
order: 15
---

# Version Matrix

Krate's compatibility claims are backed by the CI jobs in
`.github/workflows/ci.yml`. A version is listed here only when CI exercises it,
or the limitation is called out explicitly.

## React and Preact

| Surface | Versions | Coverage |
| --- | --- | --- |
| React source | React 18 semantics; React 19 actions and hooks | React 18 corpus plus the React 19 reference app in the browser differential |
| React 19 reference | React 19 | `packages/react-oracle` (oracle side of the differential) |
| Compiled packages / islands | Preact 10.20.0, 10.24.0, 10.29.8 | Preact conformance jobs in Chromium, plus browser conformance |
| Preact minimum | 10.20.0 | The build warns below this; islands use hooks and compat APIs that older releases can break |

Compiled packages run on Preact, not React. Preact's compat layer follows React
18 behavior closely; React 19-only semantics inside a compiled package remain a
documented island limit (for example `useFormStatus` is idle).

## JavaScript runtimes

| Runtime | Versions | Scope |
| --- | --- | --- |
| Node.js | 20, 22, 24, 26 | API routes, server actions, SSR, `generateStaticParams` |
| Bun | latest (2.x setup action) | Bun-native API sidecar, server actions, `generateStaticParams` |
| Deno | 2.x | Bundled `generateStaticParams` only; the action sidecar is not supported |

The Node and Bun/Deno jobs run the sidecar transport and
`generateStaticParams` tests. Node 20, 22, 24, and 26 are separate matrix jobs.

Node 20 reached end of life on 2026-04-30 and no longer gets security fixes.
Krate still declares `engines.node >= 20` and CI still runs it, but new
projects should use an active or maintenance LTS line (22, 24, or 26).

## Toolchain and browsers

| Component | Coverage |
| --- | --- |
| Go | The version in `packages/compiler/go.mod` on Ubuntu, macOS, and Windows |
| Chromium, Firefox, WebKit | Browser conformance and the React differential |
| Node package manager | pnpm 11.9.0 (CI and repo `packageManager`) |

Node is required for sidecars and for JavaScript package tooling. The core
compiler itself is a single Go binary; static sites do not need Node at runtime.

## Release gate

`pnpm check:release` is the v1 release-readiness gate. It verifies that this
page names every CI-tested Node, Preact, and browser version; the docs and
examples define complete performance budgets and CI enforces the hydration
budget; every diagnostic code has a generated reference entry; the safe codemod
tests and guides are present; every partial compatibility row has a documented
divergence; and the three-reference-app migration verification plus rollback
APIs still exist. CI and the release workflow run it with the other version,
matrix, roadmap, and docs checks.