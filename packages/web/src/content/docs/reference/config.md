---
title: Config Reference
order: 4
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

## Minification

```typescript
minify: true,                 // Enable all minification (default: true)
minifyHTML: false,            // HTML minification (inherits minify)
minifyCSS: false,             // CSS minification (inherits minify)
minifyJS: false,              // JS minification (inherits minify)
```

## CSS

CSS is merged across pages (rule-level deduplication), `@import`s are inlined,
and the merged stylesheet is minified when `minifyCSS` (or `minify`) is on.
Tailwind generates CSS only for classes detected in the scanned sources, so
unused utility classes never ship. There are no per-feature CSS config toggles.

## Features

```typescript
sourcemap: false,             // Write per-page sourcemaps (index.<hash>.js.map)
```

React syntax is always transpiled to Krate signals and effects; the former
`emitReact` option is accepted but ignored.

## Dev server

```typescript
devServer: {
  port: 3000,
  open: true,
}
```

## Tailwind CSS

```typescript
tailwind: {
  enabled: false,
  scanDirs: ["src"],       // directories to scan (or use `content` globs)
  content: [],             // Tailwind-style content globs; wins over scanDirs
  preflight: false,        // emit the Tailwind base reset (changes styling)
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
  math: false,
}
```

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
}
```

- **`ssrRuntime`** — which runtime launches the SSR sidecar that renders
  SSR/ISR/streaming regions. `"node"` (default) runs the staged renderer driver
  with plain `node`; `"bun"` runs it with `bun run`; `"deno"` runs it with
  `deno run --allow-net --allow-read --allow-env --allow-sys`.
- **`streaming`** — forces all *static* (SSG) pages into streaming mode.
  Explicit per-page `isr`/`ssr` config still wins.
- **`middlewareRuntime`** and **`apiRuntime`** (`"quickjs"` | `"node"` |
  `"bun"` | `"deno"`) choose which runtime executes middleware and API routes.
  `"quickjs"` (default) uses the embedded QuickJS runtime with no Node.js
  dependency; `"node"`/`"bun"`/`"deno"` use a sidecar process.

Page-level rendering is opted into per page via
`export const config = { isr | ssr | streaming, revalidate }` — see
[Rendering](/docs/core-concepts/rendering/). SSR/ISR/streaming pages render in
the sidecar, which resolves only the page's dynamic regions against the baked
static shell; the embedded QuickJS runtime is used for middleware, API routes,
and community plugins.

## Plugins

```typescript
plugins: [
  { name: "sitemap", order: 10, options: { baseUrl: "https://..." } },
]
```

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
[Static output & dynamic params](/docs/features/typed-routes/#static-output--dynamic-params).

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
against the emitted output. See
[Quality Checks](/docs/features/quality-checks/).

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
