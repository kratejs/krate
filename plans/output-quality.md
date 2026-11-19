# Output quality: what Krate ships to the browser

Status: implemented in PR #45 (2026-11-18), all three phases. Measured on `dev` at b7ff0aab
(after PR #44).

## Goal

Make the HTML, CSS and JS in `dist/` smaller, better cached and correct, without
changing what pages look like or do. Most of the waste is not in the minifiers
(they are within 1-3% of esbuild); it is in what gets included, how it is split,
and what is loaded eagerly.

## How this was measured

Two production builds of `dev`: the docs site (`packages/web`, 118 routes,
base-docs-theme) and `examples/` (54 routes, Tailwind, React islands). Sizes are
gzip -9 or brotli 11 unless noted. Coverage and request counts come from
headless Chromium at 1280x720 and 1280x800, in light and dark mode. Every
finding below was reproduced against the built files or the producing code.

Baseline for a typical docs page (`/docs/getting-started`):

| Part | gzip | Notes |
|---|---|---|
| HTML | 5.0 kB | 63 kB raw, 28% of it the sidebar |
| CSS | 16.4 kB | 99 kB stylesheet shared by 91 pages; ~30% used |
| JS (own) | 60.8 kB | runtime 46.7 kB + page bundle |
| Mermaid from CDN | ~200 kB | 19 modules, on 110 of 118 pages; one page has a diagram |
| Viewport prefetch | ~400 kB | 20+ pages' HTML and JS fetched on idle |

The build's own size table reports only the first three rows, so the page
looks like ~87 kB when a first visit actually moves ~700 kB.

## Phase 1: quick wins (small, low risk)

Each item is about one PR. They are independent, so they can land in any order.

### 1.1 Load mermaid and KaTeX only where they are used

- Problem: `docsHeadExtras` adds a static `import mermaid from 'https://cdn.jsdelivr.net/npm/mermaid@11/...'`
  to every page when `markdown.mermaid` is on. Module scripts are deferred, so
  DOMContentLoaded waits for it. The version floats (`@11`) and has no SRI.
  KaTeX (`markdown.math`) adds a render-blocking CDN stylesheet and two scripts
  to every page in the same way.
- Code: `internal/plugin/feed.go:295-321`.
- Fix: emit the loader only on pages whose markdown has a mermaid block or math
  (the markdown pipeline knows). In the loader, `if (document.querySelector('.mermaid')) import(url)`,
  and run the same check from the existing `krate:navigate` handler so client
  navigation to a diagram page still works. Pin exact versions and add
  `integrity`, or self-host as a hashed asset (preferred: no third-party origin,
  works with CSP).
- Impact: about -200 kB gzip and -20 requests on every page without a diagram.
  This is the biggest single item in this plan.
- Verify: browser test that a page without diagrams makes no request to the
  CDN, and that navigating client-side to the markdown guide renders its diagram.

### 1.2 Make viewport prefetch opt-in

- Problem: every `Link` gets `data-prefetch`. The router prefetches each one
  that scrolls within 200px of the viewport (HTML plus its page JS), on top of
  Speculation Rules at `moderate`. On `/docs/reference/config` that is 52
  requests and about 405 kB gzip per page view. Nothing checks Save-Data or
  connection type.
- Code: `packages/runtime/src/router.ts:444-515`, `:1922`; the `Link` default in
  `internal/build/runtimecomponent.go:76`, `internal/irtree/jscode.go:836`,
  `internal/renderer/ssreval.go:1429`.
- Fix: default to intent prefetch (hover/focus/touchstart, already
  implemented). Keep viewport prefetch for `prefetch="viewport"` links, capped
  per page (for example 5). Skip all prefetching when `navigator.connection.saveData`
  is set or `effectiveType` is 2g. When Speculation Rules are supported, let
  them do the work and skip the observer.
- Impact: about -400 kB gzip and -40 requests per docs page view.
- Risk: navigations that were instant become hover-fast. Document the
  `prefetch` prop values.

### 1.3 Stop shipping the whole component library's CSS for `Code` and `Aside`

- Problem: markdown pages get `import { Code } from "@krate/components"`. The
  bundler follows every `export *` in the barrel and keeps each component's CSS
  import, so 91 docs pages carry 52 kB raw / 7.6 kB gzip of CSS for toasts,
  sheets, selects and the like. `/tabs-demo` in the examples app uses 7.5% of
  its 62 kB stylesheet.
- Code: the auto-import at `internal/bundler/bundler.go:1297,1300` and
  `internal/plugin/docs_plugin.go:756`; CSS retention in
  `internal/bundler/cssbuild.go:407,412`.
- Fix (now): import from the subpaths the package already exports
  (`@krate/components/code`, `/aside`), and do the same in
  `packages/web/src/pages/index.tsx`. The real fix is 2.4.
- Impact: about -5.5 kB gzip (-33% of CSS) on 91 docs pages, -6 kB on the home
  page.

### 1.4 Preload only the fonts a page uses, and make the theme ship its fonts

- Problem: every `@font-face` in the global sheet is preloaded on every page, up
  to 4. The docs site preloads JetBrains Mono (40 kB) on 15 pages with no code.
  `base-docs-theme/src/theme.css:174-182` points at `/fonts/*.woff2` but the
  theme does not ship them, so any app other than a scaffolded docs site
  preloads two 404s and renders no web font.
- Code: `internal/build/fonts.go:68,151`, `build.go:5731`.
- Fix: preload the body font always and the mono font only when the page HTML
  contains `pre` or `code`. The pipeline already has the HTML in `finish()`.
  Ship the theme fonts in the package, referenced by relative `url()` so they
  are copied and hashed. Skip preloads whose URL does not resolve to a site
  file, with a warning.
- Impact: -40 kB of high-priority download on code-free pages; fixes the 404s.

### 1.5 Compression and cache headers by default

- Precompression is opt-in (`compress` in config). Without it `krate serve`
  gzips on the fly and never sends brotli. Turn `.br` and `.gz` on by default
  for production builds.
- HTML, JSON and XML are brotli level 6; JS and CSS are 11. Level 11 for
  HTML is 10.6% smaller site-wide (getting-started 9.4 kB to 8.4 kB, config
  29.9 kB to 26.5 kB). Files are skipped by mtime, so the cost is a one-time
  build cost. Code: `internal/build/precompress.go:34-50`.
- Deploy adapters (`_headers`, `vercel.json`, ...) only write rules the user
  configured, while `krate serve` already marks hashed files immutable. Emit
  `Cache-Control: public, max-age=31536000, immutable` for every hashed asset
  the manifest knows. Code: `internal/deploy/deploy.go:188-226`,
  `internal/build/manifest.go:25`.

### 1.6 Unminified and unhashed assets

- `/docs/search/search.js` (13 kB, on every docs page) is shipped as written,
  comments included. esbuild minify: 3.8 kB to 2.3 kB gzip. Minify and hash it
  at build, and load it on first search intent (the `DocsSearch` component
  already detects intent). Code: `internal/plugin/searchassets/assets.go`,
  `internal/plugin/docs_plugin.go:1001`.
- `packages/web/public/site.css` (44.8 kB, 8.8 kB gzip) is linked from the
  layout as a public file, so it is neither minified (esbuild: -23%) nor
  hashed. Import it from the layout instead. Have the build warn when a layout
  links a `.css` file from `public/`.

### 1.7 Correctness fixes in generated HTML

These are small, each with a test:

- Raw `<Profiler>`, `<Suspense>`, `<appId>`, `this.<name>` in generated docs are
  parsed as elements, so the text disappears and `<p>` nesting breaks (22
  places, on the compatibility and diagnostics pages). Escape `<` and `&` in
  `scripts/sync-compat-matrix.mjs:25` (`cell()`) and in the diagnostics page
  generator (`internal/diag/docs_test.go:73`).
- Head de-duplication keys `<meta>` by `name` only, so the light `theme-color`
  is dropped on 109 pages, and `<link rel=icon>` variants with different
  `sizes`/`type` collapse to one. Include `media` in the meta key and
  `sizes`/`type`/`media` in the icon key. Code: `internal/build/htmlgen.go:483-497`.
- 36 of 54 example pages have no `<title>` even though `og:title` gets the
  site-name fallback. Emit `<title>` from the same fallback
  (`htmlgen.go:375-389`). Add a `site.icon` option; 109 docs pages 404 on
  `/favicon.ico`.
- CSP: the meta tag is written at the end of `<head>`, after the inline
  scripts it is meant to cover, and hashes are computed on trimmed content
  while browsers hash exact bytes. Move it right after `charset`, hash the
  exact bytes, and add the CDN origins when mermaid or math is on (or self-host,
  see 1.1). Code: `internal/build/build.go:2327-2331`, `htmlgen.go:333`.
- Heading anchors are `<a aria-label="Link to this section">#</a>` inside the
  heading, so screen readers announce it as part of every heading name. Move
  the anchor after the heading text with `aria-hidden` on the `#`, or label it
  with the heading. Code: `internal/docs/toc.go:123`.
- `Slugify` collapses double hyphens, unlike GitHub, which breaks
  `#go-components--api-routes` on 4 pages (`internal/markdown/render.go:335`).
- The sitemap stamps 110 of 117 docs URLs with the build date, because it
  reads git history of the generated `.krate/gen` file. Use the markdown
  source's date, as JSON-LD already does (`internal/plugin/sitemap.go:114-120`).
- The docs search input's `aria-controls` points at an id that exists only
  inside a `<template>`. `Select` and `Command` render an empty id until mount,
  which produces duplicate ids (`-opt-0`) and `aria-controls=-list`
  (`components/src/command/command.tsx:27-31`, `select/select.tsx:86,181`).
  Generate ids at render with `useId`.

## Phase 2: structural fixes (medium effort)

These change how output is composed, so each needs browser conformance and
the React differential before landing.

### 2.1 Decide runtime features from the IR, not from a text scan

- Problem: `runtime.*.js` is 137 kB raw / 46.7 kB gzip on the docs site
  because all 6 optional features ship. Every one was pulled in by a false
  positive: the scan includes string literals and matches substrings, so a TOC
  title `requestFormReset` adds `actions`, `krateHighlight` adds `highlight`,
  a slot id `cn` adds `helpers`, and "Children, keys, and refs" adds the
  compat layer. A doc heading can therefore change the runtime hash of the whole
  site.
- Code: `internal/build/runtime_features.go:73,108-133,180-205`,
  `internal/build/runtime.go:59-79`.
- Fix: have the IR builder and renderer record the runtime APIs they emit (a
  set on the page result), and union those sets per site. Keep the text scan
  only for user-authored client code the compiler does not generate, and scan
  it with a tokenizer that skips string literals. Find out why `classes` also
  ships when no page references it (an `unknownClientCodeMarkers` hit is the
  likely cause).
- Impact: 46.7 kB to about 32 kB gzip on first visit (-14.6 kB), and a runtime
  hash that only changes when code changes.
- Risk: missing a feature breaks interactivity, so add a test that compares the
  recorded set with the old scan on every example route and fails on a feature
  the scan found that the IR did not, unless allow-listed as a known false
  positive.

### 2.2 Serialize only the props client code reads

- Problem: `__krate_props` registrations are 1.94 MB of the 4.40 MB of docs
  page bundles (44%). `BaseDocsLayout` receives the sidebar tree, TOC, title
  HTML, breadcrumbs, authors and more but reads none of them on the client;
  `SidebarNav` gets the same tree again. All of it is already in the HTML.
- Code: `internal/irtree/builder_ssr.go:1096` (`buildChildPropsRegDecl`).
- Fix: emit only the `props.X` the child's client code reads. Fall back to all
  props when `props` escapes (spread, passed whole, dynamic key).
- Impact: page-bundle gzip 944 kB to 683 kB across the site (-28%, about
  -2.2 kB per page), and nav edits stop rehashing every page bundle.

### 2.3 Do not inline handler bodies that are already declared

- Problem: each `Code` block emits its copy handler twice (as a local `var`
  and again inline in `kbindHandler`); the theme toggle appears 3 times. On
  `docs/reference/config`, 51 Code scopes are 45.9 kB of its 84 kB bundle.
- Code: `internal/irtree/builder_slots.go:4096-4102` (`extractHandlerBody`).
- Fix: reference the declared local instead of inlining the body. The larger
  fix (one function per component, called per instance) is 3.1.

### 2.4 Split CSS by source, and keep component CSS only when the component is used

- Problem: each page gets one stylesheet hashed from its combined CSS, so pages
  share a file only when their CSS is byte-identical. On the docs site the 5
  stylesheets are nested subsets of one another (58.6 kB gzip in total, 16.5 kB
  distinct). A visit across a tag page, a docs page, the compatibility guide
  and the home page downloads ~52 kB of CSS where ~18 kB would do. In the
  examples app, component CSS is copied into ~10 page files (117 kB gzip, 29.8 kB
  distinct).
- Code: `internal/build/build.go:1990` (`writePageCSS`), `:2063`, `:3007`.
- Fix: write one file per source in cascade order: global/Tailwind (already
  shared), layout/theme, syntax highlighting, each package's CSS, then the
  page's own CSS. Link them in that order so the cascade is unchanged. Then
  make the bundler keep a component's CSS only when the component's module is
  reachable from used exports (honour `sideEffects` in `package.json`, treat
  `.css` imports as side effects of their importing module only).
- Impact: about -30 kB gzip over a typical multi-page visit on the docs site,
  more in the examples app; theme CSS cached across all docs pages. Costs 1-2
  more stylesheet requests (fine on HTTP/2).
- Risk: cascade order between files, and the modern/legacy CSS variants and the
  build's size table need updating.

### 2.5 Islands: preload the whole graph and stop tiny chunks

- Problem: `/react-demo` loads 73 scripts, 171.6 kB gzip. The build table says
  78 kB because it counts only direct chunks. Only the 24 `island_*` entries
  get `modulepreload`; 45 shared `isl-*` chunks (import depth up to 6) and 4
  import-mapped Preact chunks are discovered one level at a time. 13 chunks are
  under 1 kB (one is 25 bytes). All islands mount at hydration.
- Code: `internal/build/htmlgen.go:24-43` (`dynamicChunkURLs`),
  `build.go:2239-2240`, `summary.go:77`, `islandgraph.go:264`.
- Fix: `modulepreload` the transitive closure from the esbuild metafile,
  including import-map targets; merge chunks below a size floor (for example
  2 kB) into their importer; add opt-in `client:visible` / `client:idle` mount
  strategies; make the summary count everything a page loads.

### 2.6 Smaller code-block markup

- Problem: `docs/reference/config` has 3,986 elements (Lighthouse warns at
  1,400). Chroma's per-line wrapper spans are 570 elements, and each copy
  button carries an SVG plus its own identical checkmark `<template>` (51 of
  them, 34 kB raw).
- Code: `packages/components/src/code/code.tsx:47-53`; templates from
  `internal/renderer/emitter.go:1258-1292` (one per branch instance, no dedupe).
- Fix: render both icons and switch with a data attribute and CSS; drop chroma's
  line wrappers when no lines are highlighted; dedupe identical branch templates
  per page.
- Impact: mostly DOM size and parse time (config: about -600 elements), small
  on the wire.

### 2.7 Image pipeline

- `hero.png` 1600x1000 is 7.1 kB; the generated 1600 px PNG variant is 32.4 kB,
  because `png.Encode` runs at default settings on RGBA. Use best compression,
  keep palette images paletted, and fall back to the source when it is smaller.
- The blur placeholder is a ~1 kB JPEG data URI, mostly headers; use a tiny
  PNG or WebP, or a CSS gradient of a few dominant colours.
- Code: `internal/imageproc/imageproc.go:106-110,219`.

### 2.8 Close the minifier gap

The CSS minifier is 1-3% larger than esbuild after gzip. The leftovers are
spaces between rules and before `{`, spaces after commas in selectors and
values, leading zeros (`0.85rem`), and spaces in at-rule preludes. Tighten
`minifyItems` and `minifyValue` (`internal/css/css_minify.go:176,204`,
`css_value.go:20`). About -1.6% gzip. Low priority, but cheap.

## Phase 3: architectural (large)

### 3.1 Compile client components once into shared chunks

Every docs page bundle inlines the same 18-25 kB of layout code
(`BaseDocsLayout`, `DocsSearch`, `TOCNav`, `SidebarNav`), with page constants
folded in and minified per page, so nothing is shared between pages and the
8 tag pages carry identical 26.4 kB bundles. Compile each client component
once into a content-hashed shared chunk, and keep only props and mount calls in
the page bundle. Expected page JS per navigation: 2-4 kB gzip instead of 15-35.
This builds on 2.2 and 2.3 and touches `internal/renderer/newhydrate.go:124-450`.

### 3.2 Tree-shake the runtime per site

Pages reference about 50 of the 150 core exports, and the runtime copies all of
them onto `window` and repeats esbuild's export helpers in each of its 8 IIFEs.
Bundling the runtime per site from ESM with only the referenced exports could
roughly halve core (49 kB raw). Needs 2.1's recorded API set first.

## Guardrails

Without tests these regress quietly. Add, alongside Phase 1:

- An output audit test that builds `examples/` and a small docs fixture and
  asserts:
  - no third-party script on pages that do not need it;
  - the runtime feature set per fixture;
  - no unknown lowercase elements in rendered markdown;
  - no duplicate ids;
  - every preload resolves to a file;
  - every hashed asset gets cache headers in each deploy adapter.
- Per-route size budgets. Snapshot gzip sizes for HTML, CSS, JS and the full
  transitive load (including prefetch and dynamic imports) and fail on growth
  beyond a tolerance. The existing informational benchmark job is the place to
  make this blocking.
- Make the build's size table report what a first visit actually transfers
  (transitive chunks, third-party, prefetch), so the number users see is honest.

## Checked and fine

- JS minification is full esbuild (whitespace, identifiers, syntax), no dev
  branches or comments, modern syntax kept. Scripts are at the end of `<body>`
  and preloaded from `<head>`.
- CSS: no empty rules, duplicate selectors, redundant prefixes, duplicate
  `@keyframes` or `@property`; `@layer` order declared first; scoped class
  names are short.
- HTML: charset first, `lang`, one `h1` per page with no skipped levels, skip
  link and `<main>`, canonical matches `og:url`, valid JSON-LD, feeds and
  sitemap; no broken internal links (apart from the slug case above); inline
  theme and platform scripts are tiny and prevent a flash of the wrong theme.
- Inter-tag whitespace is 74-316 B brotli per page; further HTML minification is
  not worth the risk.

## Open questions (not verified)

- Cloudflare Pages' default caching for files without `_headers`. If it
  revalidates every time, 1.5 matters more than estimated.
- `dist/.krate` (manifest with source paths; in examples also the server
  renderer and API handlers with source maps) is removed by `krate deploy`
  but not by `krate build`. Check which hosts serve dot-directories, and
  consider moving it out of `dist`.
- Asset hashes are FNV-32a truncated to 6 base-36 characters (~31 bits).
  Collisions are unlikely, but with immutable caching one would be permanent.
  A longer hash costs a few bytes per URL.
- Latin `unicode-range` subsets of Inter (48 kB) and JetBrains Mono (40 kB)
  probably save 30-50%; not measured.
- `local('Inter')` with a 100-900 weight range may synthesize bold on machines
  with a non-variable Inter installed.
- Opacity modifiers on colour utilities have no static fallback for browsers
  without `color-mix()` (noted in PR #44); below Tailwind v4's browser floor.

## Suggested order

1. 1.1 mermaid/KaTeX and 1.2 prefetch: the two items worth ~600 kB per view.
2. 1.3 to 1.7 and the guardrail tests, as small separate PRs.
3. 2.1 runtime features and 2.2 props, then 2.3; they shrink every page bundle
   and set up 3.1.
4. 2.4 CSS split, 2.5 islands, then 2.6 to 2.8.
5. Phase 3 once the guardrails are in place.

One PR at a time on this machine: each needs a docs-site build, the Chromium
browser conformance suite and, for renderer changes, the React differential.
