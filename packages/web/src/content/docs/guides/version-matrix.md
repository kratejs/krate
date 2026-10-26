---
title: Version Matrix
description: Supported React, Preact, Node, Bun, Deno, Go, and browser versions.
order: 11
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
| Node.js | 20, 22 | API routes, server actions, SSR, `generateStaticParams` |
| Bun | latest (2.x setup action) | Bun-native API sidecar, server actions, `generateStaticParams` |
| Deno | 2.x | Bundled `generateStaticParams` only; the action sidecar is not supported |

The Node and Bun/Deno jobs run the sidecar transport and
`generateStaticParams` tests. Node 20 and 22 are separate matrix jobs.

## Toolchain and browsers

| Component | Coverage |
| --- | --- |
| Go | The version in `packages/compiler/go.mod` on Ubuntu, macOS, and Windows |
| Chromium, Firefox, WebKit | Browser conformance and the React differential |
| Node package manager | pnpm 11.9.0 (CI and repo `packageManager`) |

Node is required for sidecars and for JavaScript package tooling. The core
compiler itself is a single Go binary; static sites do not need Node at runtime.
