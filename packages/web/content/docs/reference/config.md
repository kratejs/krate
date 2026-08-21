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
emitReact: false,             // React compatibility mode (rewrites React → krate)
```

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
  scanDirs: ["src"],
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
  streaming: false,           // force ALL pages to streaming SSR
  rendererPort: 0,            // Node renderer port (0 = default)
  timeout: 5000,              // max render time (ms)
  maxCacheSize: 128,          // ISR in-memory cache size
  middlewareRuntime: "quickjs",  // middleware.ts runtime
  apiRuntime: "quickjs",        // API route runtime
}
```

`middlewareRuntime` and `apiRuntime` (`"quickjs"` | `"node"` | `"bun"` |
`"deno"`) choose which runtime executes middleware and API routes.
`"quickjs"` (default) uses the embedded QuickJS runtime with no Node.js
dependency; `"node"`/`"bun"`/`"deno"` use a sidecar process. SSR/streaming
pages always render in the Node renderer sidecar, and runtime components
(`@runtime`) always render via the embedded QuickJS runtime.

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

## Validation

```typescript
validate: (config) => { /* optional build-time validation */ }
```
