---
title: Browser Support
description: Supported browsers and the JavaScript baseline Krate emits.
order: 8
---

# Browser Support

Krate targets **evergreen engines that implement ES2020**. There is no ES5
downleveling, no polyfills, and no differential (legacy/modern) serving.

## Baseline

| Browser | Minimum |
|---------|---------|
| Chrome / Edge (Chromium) | 87 |
| Firefox | 78 |
| Safari (macOS/iOS) | 14 |
| Android Chrome | 87 |

Roughly: anything released since late 2020. The policy lives in the repo's
`.browserslistrc`; update it and this page together when the baseline changes.

## What the compiler emits

| Output | Target |
|--------|--------|
| Hydration chunks & workers | `es2020` (esbuild) |
| Client runtime bundle (`krate-runtime.js`, `krate-hydrate.js`) | `es2020` |
| Generated standalone pages | static HTML — no JS required to read |

Hydration code uses `const`/`let`, `Map`/`Set`, `fetch`, `EventSource`,
`history.pushState`, and `document.createTreeWalker`. All are present in the
baseline.

## No-JS behavior

Every page is pre-rendered. Pages that use no signals or event handlers ship
**zero JavaScript**, so they work in any browser (and with JS disabled). Only
interactive pages load the shared runtime chunk plus their page bundle.

## Supporting older browsers

Krate does not ship a legacy build. If you must support pre-ES2020 engines,
post-process the emitted `dist/` JS with your own transpiler/polyfill step
(e.g. Babel + `core-js`) and add any required polyfills to a global script in
your layout `<Head>`. The compiler's Go-native pipeline will not do this for
you.
