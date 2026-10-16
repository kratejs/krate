---
title: Browser Support
description: Supported browsers, the JavaScript baseline, and the compat modes for older engines.
order: 8
---

# Browser Support

Krate targets **evergreen engines that implement ES2020**. Hydration chunks,
workers, and the client runtime bundle are all emitted as ES2020; there is no
ES5 downleveling and no polyfills.

## Baseline

| Browser | Minimum |
|---------|---------|
| Chrome / Edge (Chromium) | 87 |
| Firefox | 78 |
| Safari (macOS/iOS) | 14 |
| Android Chrome | 87 |

The policy lives in the repo's `.browserslistrc`. The compiler emits ES2020 for
hydration chunks and workers; the client runtime bundle uses the same baseline.
This baseline governs JavaScript and non-interactive content; zero-JS
**CSS Signals** additionally require `:has()` (see below).

## CSS Signals and `:has()`

Zero-JS CSS Signals (hidden `<input>` controllers + `:has()` stylesheets,
see [CSS Signals](/docs/core-concepts/css-signals/)) require **`:has()`**:
Chrome/Edge 105+, Safari 15.4+, Firefox 121+. Krate's default (`modern`) output
also uses CSS anchor positioning for menus/popovers (Chromium 125+), which is
progressive — browsers without it fall back to the component's own positioning.

By default Krate emits a `@supports not selector(:has(*))` fallback that
**reveals all panels** (the interactive reveal degrades to "everything shown").
For real interactivity on older engines, use a compat mode.

## Compat modes

Set `compat` in `krate.config.ts` (see
[Browser compatibility](/docs/reference/config/#browser-compatibility)):

```typescript
compat: { mode: "hybrid" },
```

| Mode | CSS Signals | Client JS | Browser floor |
|------|-------------|-----------|---------------|
| `modern` (default) | `:has()` selectors | zero | `:has()` (2023+) |
| `legacy` | class-based selectors + shim | tiny scoped shim | ES2020 baseline |
| `hybrid` | both, `@supports`-selected | shim only where needed | union |

- **`legacy`** keeps the exact same DOM (hidden inputs, labels, panel wrappers)
  but compiles the state selectors to classes (`:has(.krc0-r-mac:checked)` ->
  `.krc0.krc0-r-mac-on`) driven by a small delegated shim. Interactive pages that
  are zero-JS today will load the shim.
- **`hybrid`** emits the `:has()` rules under `@supports selector(:has(*))` and
  the class rules under `@supports not selector(:has(*))`, and gates the shim
  behind a `CSS.supports('selector(:has(*))')` check. Modern browsers keep the
  zero-JS path; legacy browsers download and run the shim. One document, no
  server logic, works on any host or CDN.

`legacy` uses a static colour fallback ahead of `color-mix()` and omits CSS
anchor positioning (components fall back to their own positioning).

If scripting is disabled, `legacy`/`hybrid` emit a `<noscript>` reveal-all
fallback, so engines that lack `:has()` still show their panels instead of hiding
them. `hybrid` gates the rule on `@supports not selector(:has(*))` (a modern
browser with scripting off keeps native `:has()`); `legacy` is unconditional
(its stylesheet never uses `:has()`). The shim also observes DOM mutations, so
panels inserted after load (e.g. SPA navigation) are synced too.

## UA-selected documents (opt-in)

`compat: { mode: "hybrid", userAgent: true }` additionally writes per-page
sibling documents (`index.modern.html`, `index.legacy.html`), each linking a
variant-only stylesheet so a classified visitor downloads only its half of the
hybrid CSS. `krate serve` sends the matching sibling by User-Agent and sets
`Vary: User-Agent`. The default document stays the full hybrid, so static hosts
(which cannot select per request) still serve something that works everywhere.
Detection is conservative: anything not recognized as a modern engine gets the
legacy document, which works on every browser. Where available, Chromium's
User-Agent Client Hint (`Sec-CH-UA`, requested via `Accept-CH`) is preferred over
the `User-Agent` string for accuracy.

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
interactive pages load the shared runtime chunk plus their page bundle. In
`legacy`/`hybrid` modes, pages with CSS Signals load the small scoped shim.

## Supporting older browsers

To go below the ES2020 baseline, post-process the emitted `dist/` JS with your
own transpiler/polyfill step (e.g. Babel + `core-js`) and add required
polyfills to a global script in your layout `<Head>`. The compiler's Go-native
pipeline does not downlevel JavaScript.
