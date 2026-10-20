---
title: Config Reference
order: 4
description: Every krate.config.ts key, with types and defaults.
---

# Config Reference

The complete `krate.config.ts` surface, type-checked with `defineConfig`.

## Build

```typescript
entry: "src/index.tsx",       // Entry point (default: src/index.tsx)
outDir: "dist",               // Output directory (default: dist)
pagesDir: "src/pages",        // Pages directory (default: src/pages)
publicDir: "public",          // Static assets directory (default: public)
```

## Site metadata

`site` is the canonical site metadata; `seo` is a deprecated alias (a config
using both gets a warning and `site` wins per field). The sitemap/feed/llms/robots
plugins inherit these values.

```typescript
site: {
  url: "https://example.com",   // canonical origin (sitemap, canonical, OG)
  name: "My Site",
  description: "Default meta description",
  image: "https://example.com/og.png",
  titleTemplate: "%s | My Site",
  twitter: "mysite",            // twitter:site handle (@ optional)
  themeColor: "#0b1020",
  locale: "en",                 // <html lang> + og:locale
},
```

`defineConfig` also accepts a function invoked with the build context, and
`--mode <name>` selects the mode (and `.env.<mode>` files):

```typescript
export default defineConfig(({ command, mode, isDev }) => ({
  site: { url: isDev ? "http://localhost:3000" : "https://example.com" },
}));
```

Config files may be `krate.config.ts`, `.js`, `.mjs`, or `.json`. A `.json`
config cannot call the factory helpers (`docs()`, `defineContent`); write the
equivalent plain objects, or use a `.ts`/`.js` file.

`titleTemplate` applies only to bare page titles; a title that already carries a
site/section suffix (e.g. docs pages: `Guide - Site Docs`) is left unchanged so
the suffix is not duplicated.

Builds write a JSON Schema to `.krate/config.schema.json` for editor/tooling
completion (it is a local build artifact, not served with the site). A `.json`
config can reference it with `"$schema": "./.krate/config.schema.json"`.

## Minification

```typescript
minify: true,                 // Enable all minification (default: true)
minifyHTML: false,            // HTML minification (inherits minify)
minifyCSS: false,             // CSS minification (inherits minify)
minifyJS: false,              // JS minification (inherits minify)
```

Minification only controls whitespace/size optimizations. **Duplicate
declarations within a rule (including same-name CSS custom properties) are
always folded to the last value**, whether or not minification is enabled —
formatting is preserved when it is off.

## CSS

CSS is merged across pages (rule-level deduplication), `@import`s are inlined,
and the merged stylesheet is minified when `minifyCSS` (or `minify`) is on.
Tailwind generates CSS only for classes detected in the scanned sources, so
unused utility classes never ship. There are no per-feature CSS config toggles.

## Features

```typescript
sourcemap: false,             // Write per-page sourcemaps (index.<hash>.js.map)
```

React syntax is always transpiled to Krate signals and effects; no config is
required.

## Precompression

```typescript
compress: {                   // opt-in; also enabled by `krate build --precompress`
  brotli: true,               // write .br siblings
  gzip: true,                 // write .gz siblings
}
```

Build-time precompression writes `.br`/`.gz` siblings beside compressible assets
(HTML, CSS, JS, JSON, SVG, XML, ...). `krate serve` negotiates them per request
by `Accept-Encoding` (brotli > gzip > raw) and always sends
`Vary: Accept-Encoding`, falling back to on-the-fly gzip for dynamic responses.
Static hosts such as Cloudflare Pages and Netlify serve the siblings
automatically. Assets under 1 KB, already-compressed formats (images, fonts,
wasm), and internal `.krate/` build artifacts are skipped.

## Head & scripts

```typescript
head: {
  meta: [{ name: "theme-color", content: "#0b1020" }],
  links: [{ rel: "preconnect", href: "https://cdn.example" }],
  scripts: [
    { src: "/analytics.js", defer: true },
    // { content: "/* inline */", module: true },
  ],
  // raw: ['<meta ...>'],
},
// concise raw form:
// head: ['<link rel="preconnect" href="https://cdn.example">'],
```

`head` injects into every page's `<head>` before CSP hashing. `scripts` supports
`src`/`content`, `async`/`defer`, `type`/`module`, and `attrs`. Use it for
analytics, fonts, and other site-wide tags.

## Response headers & security

> These apply to **`krate serve`** only. Static hosts (Netlify, Cloudflare,
> Vercel) ignore them; configure headers there instead.

`headers` applies custom response headers; `security` overrides the defaults
(`"off"` disables a header).

```typescript
headers: [
  { source: "/assets/*", headers: { "Cache-Control": "public, max-age=31536000, immutable" } },
],
security: {
  frameOptions: "SAMEORIGIN",
  referrerPolicy: "strict-origin-when-cross-origin",
  permissionsPolicy: "camera=(), microphone=()",
  hsts: "max-age=31536000; includeSubDomains",
},
```

`krate deploy` writes explicit `headers` rules to `_headers` (Netlify /
Cloudflare Pages) and to `vercel.json` (Vercel). Only explicit rules are emitted;
`security` defaults stay serve-only so static-host behavior is not changed
implicitly.

## Images

```typescript
images: {
  breakpoints: [640, 1080, 1920],
  quality: 80,
  sizes: "(max-width: 768px) 100vw, 50vw",
  placeholder: true,
  formats: ["webp", "original"],
  remotePatterns: [{ hostname: "images.example.com" }],
  // unoptimized: true,
},
```

Controls the `<Image>` pipeline: generated widths, encode quality, default
`sizes`, blur placeholder, output formats, and allowed remote hosts. A remote
`src` not listed in `remotePatterns` is a build error; `unoptimized` passes the
original image through.

`remotePatterns` hostnames support `*.example.com` (one label) and
`**.example.com` (any depth); `pathname` supports `*` (one segment) and `**`
(any depth), e.g. `"/images/**"`. Output formats are WebP and the original
format; **AVIF is not supported** (it needs a separate encoder).

## Dev server

```typescript
devServer: {
  port: 3000,
  open: true,
  overlay: true,   // show the dev error overlay (default true)
  toolbar: true,   // show the dev toolbar (default true)
  editor: "code",  // editor command for "open in editor" (default "code" = VS Code)
  proxy: { "/api": "http://localhost:8080" }, // forward matching paths during dev
}
```

`editor` may include flags (for example `"code -g"`). It is used by the
overlay's location links and by `GET /__krate/open`. `proxy` forwards matching
path prefixes to a backend only in dev (longest prefix wins), so you can run a
separate backend without CORS. In development, generated JavaScript is served
unminified (with component comments) so devtools are readable; production output
stays minified.

## Tailwind CSS

```typescript
tailwind: {
  enabled: false,
  scanDirs: ["src"],       // directories to scan (or use `content` globs)
  content: [],             // Tailwind-style content globs; wins over scanDirs
  preflight: true,         // emit the Tailwind base reset (default; false to opt out)
  strict: false,           // warn about classes that produce no rule
  darkMode: "media",       // "media" | "class" | "selector"
  executeConfig: false,    // execute tailwind.config via npx tsx (else static parse)
}
```

## Content Security Policy

```typescript
csp: {
  enabled: false,
  directive: "",              // custom CSP string (empty = auto-generate)
}
```

## Markdown

```typescript
markdown: {
  gfm: true,
  headingAnchors: true,
  admonitions: true,
  codeHighlight: true,
  math: false,          // load KaTeX (CDN) and render $...$ / $$...$$
  plugins: ["./md/transform.ts"], // AfterMarkdownParse HTML transforms
}
```

Beyond GFM (tables, task lists, strikethrough, autolinks), the renderer supports
definition lists, footnotes (`[^1]`), emoji shortcodes (`:rocket:`), and
component directives — `:::card`, `:::steps` (`::step`), `:::tabs` (`::tab`),
and `:::code-group`. The `Tabs`/`Steps`/`Card`/`Code` components are auto-imported
for the docs plugin; elsewhere `:::component Name` wraps arbitrary components.

`markdown.plugins` lists JS/TS modules (relative to the project root) whose
default export transforms rendered HTML. Author them with `defineMarkdownPlugin`:

```ts
// md/transform.ts
import { defineMarkdownPlugin } from "@krate/plugin";
export default defineMarkdownPlugin((html) => html.replaceAll("TODO", "DONE"));
```

Each runs on the `AfterMarkdownParse` hook (async transforms are awaited) and
applies to rendered Markdown for both `.md`/`.mdx` page files and docs-plugin
pages.

## Runtime

```typescript
runtime: "node",              // "node" | "bun" | "deno"
```

## SSR / Streaming

```typescript
ssr: {
  streaming: false,           // force all *static* pages to streaming SSR
  ssrRuntime: "node",         // sidecar runtime: "node" | "bun" | "deno"
  rendererPort: 0,            // renderer sidecar port (0 = default)
  timeout: 5000,              // max render time (ms)
  maxCacheSize: 128,          // ISR in-memory cache size
  middlewareRuntime: "quickjs",  // middleware.ts runtime
  apiRuntime: "quickjs",        // API route runtime
  apiWorkers: 8,                // embedded QuickJS API worker pool (default min(NumCPU, 8))
  apiIsolation: false,          // true = fresh VM per request (no shared state; slower)
  nodeWorkers: 8,               // Node sidecar worker processes (cluster)
  rendererWorkers: 1,           // SSR renderer worker processes (cluster; default 1)
}
```

- **`ssrRuntime`** - which runtime launches the SSR sidecar that renders
  SSR/ISR/streaming regions. `"node"` (default) runs the staged renderer driver
  with plain `node`; `"bun"` runs it with `bun run`; `"deno"` runs it with
  `deno run --allow-net --allow-read --allow-env --allow-sys`.
- **`streaming`** - forces all *static* (SSG) pages into streaming mode.
  Explicit per-page `isr`/`ssr` config still wins.
- **`middlewareRuntime`** and **`apiRuntime`** (`"quickjs"` | `"node"` |
  `"bun"` | `"deno"`) choose which runtime executes middleware and API routes.
  `"quickjs"` (default) uses the embedded QuickJS runtime with no Node.js
  dependency; `"node"`/`"deno"` use the Node `http` sidecar; **`"bun"` uses a
  Bun-native sidecar built on `Bun.serve`** (no Node http stack, no cluster
  shim) — the fastest JS/TS sidecar option.
- **`nodeWorkers`** forks that many Node worker processes (`cluster`) for the
  API sidecar when `apiRuntime` (or `middlewareRuntime`) is
  `node`/`bun`/`deno`. (Ignored by the Bun sidecar, which is single-process.)
- **`rendererWorkers`** forks that many Node worker processes (`cluster`) for the
  SSR renderer, so page/region rendering uses more than one CPU. Default `1`
  (single process). Region and ISR caches are per-worker, and ISR disk
  persistence is disabled while clustered. Irrelevant when all request-time
  regions render on the Go host (Go components).
- **`apiWorkers`** sizes the embedded QuickJS API worker pool. Workers are
  long-lived VMs that each load a route once and reuse it, so module state
  persists across requests (Node semantics) and requests run in parallel.
- **`apiIsolation`** runs every API request in a fresh VM (no shared module
  state). Slower; dev uses it automatically so route edits are picked up.

Page-level rendering is opted into per page via
`export const config = { isr | ssr | streaming, revalidate }` — see
[Rendering](/docs/core-concepts/rendering/). SSR/ISR/streaming pages render in
the sidecar, which resolves only the page's dynamic regions against the baked
static shell; the embedded QuickJS runtime is used for middleware, API routes,
and community plugins.

## Partial prerendering (PPR)

```typescript
ppr: true,              // cache dynamic regions on non-ISR pages
ppr: { revalidate: 60 } // …with a custom window (seconds)
```

Dynamic regions (Suspense primaries + `@runtime` components) are already spliced
into the baked shell. PPR additionally caches each region independently with
stale-while-revalidate. Per-region directives win: add
`export const revalidate = 30` to a `*.runtime.tsx` component to give just that
region a 30-second window. Region caches are bounded, persisted with the ISR
cache, and invalidated per route.

:::warning
Region cache keys include the route, parameters, and query — **not cookies,
headers, or sessions**. Do not enable `ppr` for regions that render
per-user/per-session content, or one user's output can be served to another.
:::

## View Transitions

```typescript
viewTransitions: "auto", // "auto" (default) | "off" | true | false
```

When enabled, the SPA router wraps each navigation in the native View
Transitions API (`document.startViewTransition`), and every page emits the
cross-document `@view-transition { navigation: auto }` rule so full-page loads
morph too. Mark persistent chrome with `data-view-transition="name"` to morph it
instead of cross-fading. Reduced-motion preferences are respected.

## Plugins

```typescript
import { sitemap, feed, docs, robots, llms } from "@krate/core";

plugins: [
  sitemap({ baseUrl: "https://example.com" }),
  feed({ baseUrl: "https://example.com", type: "all", count: 20 }),
  robots({ disallow: "/admin" }),
  llms({ title: "Krate Docs" }),
  docs({ contentDir: "src/content/docs", title: "Docs" }),
]
```

The `feed` plugin emits `feed.xml` (RSS), `atom.xml`, and `feed.json` from the
docs content and adds `<link rel="alternate">` discovery plus JSON-LD
structured data to docs pages. `robots` and `llms` are typed factories like the
others; their options override the top-level `robots`/site metadata. The docs
plugin also renders tag/category index pages
(`/docs/tags/<slug>/`, `/docs/categories/<slug>/`) and fills each page's
"last updated" date from git history (disable with `docs({ lastUpdated: false })`).

Docs layout options:

```typescript
docs({
  // Sidebar: "auto" (default), "none", an explicit tree, or a JSON file.
  sidebar: "auto",
  // sidebar: [{ title: "Guides", children: [{ title: "Intro", url: "/docs/guides" }] }],
  // sidebar: { from: "file", file: "sidebar.json" },

  toc: true,          // table of contents (default true)
  breadcrumbs: true,  // breadcrumbs (default true)
  pagination: true,   // prev/next links (default true)
  editLink: { pattern: "https://github.com/org/repo/edit/main/:path" },
  search: { placeholder: "Search the docs", hotkey: "/" },
})
```

`editLinkBase` is deprecated; use `editLink.base` (base URL + source path) or
`editLink.pattern` (a URL template with `:path`). `search.hotkey` (e.g. `/` or
`mod+k`) focuses the search input, and `search.placeholder` sets its placeholder.

## Extends & presets

```typescript
extends: "./base.config.ts",
// extends: ["@acme/krate-preset", "./local.config.ts"],
```

Bases are merged underneath this config: objects deep-merge (this config wins),
and additive arrays (`plugins`, `redirects`, `rewrites`, `serverComponents`,
`runtimeComponents`, `serverDirs`, `runtimeDirs`) concatenate. Duplicate
`plugins` are de-duped by name, with the extending config winning. Paths resolve
relative to the extending config, then to `node_modules` (package presets —
resolved via `package.json` `krate`, `exports`, `module`, or `main`). Cycles are
rejected.

## Deploy

```typescript
deploy: {
  target: "vercel",                  // netlify | vercel | cloudflare | gh-pages
  options: { cleanUrls: "true", trailingSlash: "false" },
},
```

`krate deploy` uses this target unless `--target` is passed; the adapter options
customize the generated host files.

## i18n & versions

```typescript
i18n: { defaultLocale: "en", locales: ["en", "fr"], localeLabels: { fr: "Français" } },
versions: { current: "v2", versions: ["v2", "v1"], labels: { v1: "1.x (legacy)" } },
```

`localeLabels` / `labels` override the built-in display names in the locale and
version switchers (docs plugin).

`i18n` and `versions` are **docs-scoped**. For app pages, use `[locale]` dynamic
routes + content collections (Krate does not auto-prefix arbitrary page routes).

Docs pages emit `<html lang>` for their locale server-side. Any page can override
it with `export const lang = "fr"` (or `export const config = { lang: "fr" }`).

## Redirects & rewrites

```typescript
redirects: [
  { source: "/old-page", destination: "/new-page", permanent: true },
]
rewrites: [
  { source: "/docs/:path*", destination: "/documentation/:path*" },
]
```

## SEO & robots

`seo` is retained for compatibility; prefer `site` (see [Site metadata](#site-metadata)),
which also supports `titleTemplate`, `twitter`, `themeColor`, and `locale`.

```typescript
seo: {
  baseUrl: "https://example.com",
  siteName: "Krate",
  description: "A modern static site generator",
  image: "https://example.com/og.png",
}
robots: {
  allow: "/",
  disallow: "/admin",
  sitemap: "https://example.com/sitemap.xml",
}
```

## Component tiers

```typescript
serverComponents: ["DataTable"],   // names → @server
runtimeComponents: ["AuthCheck"],  // names → @runtime
serverDirs: ["src/components/server"],
runtimeDirs: ["src/components/runtime"],
```

## TypeScript path aliases

```typescript
pathAliases: {
  "@/*": ["./src/*"],
},
tsBaseDir: ".",
```

Path aliases are also read automatically from `tsconfig.json`
(`compilerOptions.paths` and `baseUrl`) — e.g. `@/components/Button` →
`src/components/Button`.

## Content collections

```typescript
content: {
  blog: {
    dir: "src/content/blog",              // default: src/content/<name>
    schema: {
      title: "string",
      order: { type: "number", required: true },
      tags: "string[]",
      status: { type: "string", enum: ["draft", "published"], default: "draft" },
    },
  },
},
```

`content` may be a plain object or `defineContent({...})` (an optional identity
helper for editor assistance). Entries under each `dir` are validated against
the schema; violations fail the build. Types are generated into
`.krate/types/content.d.ts`. See
[Typed Routes & Content](/docs/features/typed-routes/).

## Output mode

```typescript
output: "static",   // default: request-time rendering allowed
```

`"static"` makes the build fully static: dynamic routes only render the params
returned by `generateStaticParams` (unknown params 404), and SSR/ISR/streaming
are disabled. Individual pages can re-enable dynamic params with
`export const dynamicParams = true`. See
[Static output & dynamic params](/docs/features/typed-routes/#static-output-dynamic-params).

## Browser compatibility

```typescript
compat: {
  mode: "modern",     // "modern" (default) | "legacy" | "hybrid"
  userAgent: false,   // hybrid only: emit a UA-selectable legacy document
},
```

Controls how CSS Signals are emitted for browser support (see
[Browser Support](/docs/core-concepts/browser-support/)):

- **`modern`** (default) — `:has()` selectors, zero client JS for CSS-signal
  pages. Requires `:has()` (Chrome/Edge 105+, Safari 15.4+, Firefox 121+).
- **`legacy`** — emits class-based selectors plus a tiny scoped shim instead of
  `:has()`, so interactive CSS-signal pages work on browsers without it.
- **`hybrid`** — emits both, each wrapped in `@supports selector(:has(*))`, and
  feature-gates the shim. Modern browsers keep the zero-JS `:has()` path; legacy
  browsers get the shim. One document, no server involvement.

`userAgent: true` (hybrid only) additionally writes per-page sibling documents
(`index.modern.html` and `index.legacy.html`), each linking a **variant-only**
stylesheet (modern-only / legacy-only). `krate serve` sends the matching sibling
to User-Agent-classified visitors, preferring Chromium's `Sec-CH-UA` client hint
(requested via `Accept-CH`) over the `User-Agent` string, and sets
`Vary: User-Agent, Sec-CH-UA`. The default
document stays the full hybrid, so static hosts (which cannot select per
request) still serve a document that works everywhere.

## Quality checks

```typescript
checks: {
  a11y: "error",                  // "error" | "warning" | "off" | boolean
  seo: "warning",
  perf: "warning",
  rules: { "perf/js-budget": "error" },
  ignore: ["seo/og"],
  budget: { js: 150 },            // per-route client-JS budget, KB
  failOn: "error",
  custom: ["./checks/my-rule.ts"],
},
```

When a `checks` object is present, `krate build` runs the quality gates and
fails on findings at or above `failOn`. `krate check` runs the same rules
against the emitted output. Alongside the a11y/SEO/perf rules, Krate checks
`seo/broken-link` (internal links resolve to a known route),
`a11y/broken-anchor` (in-page `#id` targets exist), `seo/og-image` (absolute
Open Graph image), and `seo/duplicate-meta` (unique titles/descriptions across
pages). See [Quality Checks](/docs/features/quality-checks/).

## Dead-code elimination (DCE)

```typescript
dce: {
  css: true,          // prune unused CSS-module rules (default true)
  js: true,           // JS dead-code elimination (default true)
  aggressive: false,  // EXPERIMENTAL: also drop local helpers the scan can't prove unused
},
```

CSS DCE removes `.module.css` rules whose scoped class never appears in the
page's HTML (global, Tailwind, and component styles are never touched). JS DCE
is conservative by default. Disable both with `--no-dce`.

:::warning
**Experimental:** `dce.aggressive` additionally drops local helper functions the
reference scan cannot prove are used. This can remove a function that is only
referenced from a part of the component body the scan does not inspect, so it is
off by default and may change behavior. Leave it off unless you have verified the
output; use `--no-dce` to disable DCE entirely.
:::

## Fonts

```typescript
fonts: {
  preload: true,      // <link rel="preload"> for @font-face fonts (default true)
  display: "swap",    // default font-display injected when a face omits it ("off" to skip)
},
```

## Go (components & API routes)

```typescript
go: {
  enabled: true,                          // default true
  module: "krate-goapi",                  // module path when none is provided
  deps: [{ path: "github.com/x/y", version: "v1.2.3" }],
  replaces: [{ from: "example.com/x", to: "../local/x" }],
  exclude: [{ path: "github.com/a/b", version: "v9.9.9" }],
  tidy: false,                            // run `go mod tidy` before building
  componentDirs: ["src/components"],      // scanned for *.go components
  apiDirs: ["src/api"],                   // Go API routes (first dir is used)
  apiEnabled: true,                       // build Go API routes
},
```

`go` configures the unified Go host that compiles **Go API routes**
(`src/api/**/*.go`) and **Go components** (`componentDirs/**/*.go`, see
[Go Components](/docs/features/go-components/)) into one binary. It supersedes
the older `goApi` block (deprecated; its values are folded into `go`, and a
config that sets it gets a warning).

Go is **enabled by default** with `componentDirs: ["src/components"]` and
`apiDirs: ["src/api"]`, so no config is needed for the default layout. Run
`krate go:init` once to set up editor intellisense (a project `go.mod` +
`go.work`, the vendored helpers under `.krate/goext`, and `.krate/types` in your
`tsconfig.json`). Go requires the Go toolchain at build time.

Source trees are preserved: helper files keep their package, and each route file
compiles into its own generated package. A `src/api/go.mod` or
`src/components/go.mod` is used verbatim; otherwise a project-root `go.mod` is
merged (its `require`/`replace`/`exclude` directives are copied and relative
`replace` paths rebased), falling back to a generated module. The helper
packages (`krate`, `runtime`) are vendored under `.krate/goext` and imported via
their real paths (`github.com/kratejs/krate/packages/compiler/goext/...`). See
[API routes](/docs/features/api-routes/).

## Base path

```typescript
basePath: "/docs",   // serve the whole site under a sub-path (default: "")
```

When set, the build prefixes emitted asset URLs (page hydration scripts,
runtime chunks, stylesheets) with the base path, and `krate serve` mounts the
site under it (requests outside the prefix are redirected into it). Relative
dynamic imports resolve against the prefixed script URL. Author your own links
with the prefix (or use relative URLs).

## Server

```typescript
server: {
  host: "",             // bind address (default: all interfaces)
  port: 3000,           // falls back to devServer.port, then 3000
  maxBodySize: 1048576, // request body cap in bytes (0 = defaults)
},
```

`krate serve` uses `server.port` (or `devServer.port`). Request bodies are capped
(1 MiB for pages/middleware, 25 MiB for API routes by default). `GET /healthz`
is a liveness probe; `GET /readyz` is readiness (fails while an expected SSR
sidecar is down). Both stay reachable at the root even under a `basePath`.

## CORS

```typescript
cors: {
  enabled: false,                       // default: same-origin only
  origins: ["https://app.example"],     // ["*"] when omitted
  methods: ["GET", "POST"],
  headers: ["Content-Type"],
  credentials: false,
  maxAge: 600,
},
```

Preflight `OPTIONS` requests are answered automatically when enabled.

## API sidecar

Forward `/api/*` to your own HTTP service. Requests are forwarded first, and a
`404` falls through to Krate's built-in Go/TS/QuickJS routes, so the sidecar can
own exactly the routes it wants (including dynamic segments like
`/users/[id]`) with no route declarations.

```typescript
api: {
  sidecar: {
    // Supervised: Krate launches and stops the process.
    command: "node", args: ["server.js"], port: 8080,
    // ...or proxy-only for an already-running service:
    // target: "http://127.0.0.1:8080",
    prefix: "/api",   // path prefix the sidecar owns (default "/api")
  },
},
```

See [API Routes](/docs/features/api-routes/#custom-api-sidecar).

## Tailwind `@apply`

Plain utility `@apply` is expanded in your CSS (`p-4`, `font-bold`, ...).
Variant (`hover:*`, `md:*`), descendant-selector (`space-x-*`) and unknown
utilities cannot be expressed as plain declarations; they are reported as build
warnings and left unexpanded.

## Validation

```typescript
validate: (config) => { /* optional build-time validation */ }
```
