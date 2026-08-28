---
title: Configuration
order: 3
---

# Configuration

Krate is configured with a `krate.config.ts` file at the project root, typed
with `defineConfig` from `@krate/core`. Every supported key is type-checked —
unknown or misspelled options become compile errors.

```typescript
import { defineConfig, sitemap, docs } from '@krate/core';

export default defineConfig({
  entry: "src/index.tsx",
  outDir: "dist",
  pagesDir: "src/pages",
  minify: true,
  tailwind: { enabled: false, scanDirs: ["src"] },
  redirects: [{ source: "/old", destination: "/new", permanent: true }],
  plugins: [
    sitemap({ baseUrl: "https://example.com" }),
    docs({ contentDir: "content/docs", title: "Docs" }),
  ],
});
```

## Build options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `entry` | `string` | `src/index.tsx` | Entry point |
| `outDir` | `string` | `dist` | Output directory |
| `pagesDir` | `string` | `src/pages` | Pages directory |
| `publicDir` | `string` | `public` | Static assets directory |
| `minify` | `boolean` | `true` | Enable all minification |
| `sourcemap` | `boolean` | `false` | Write per-page sourcemaps (`index.<hash>.js.map`) |
| `emitReact` | `boolean` | `false` | React compatibility mode (rewrites React → krate) |

## Runtime

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `runtime` | `"node" \| "bun" \| "deno"` | `node` | Runtime for the API routes sidecar |

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

Krate's Tailwind is **Go-native** (no PostCSS). The class scanner extracts class
names from source files and maps them to rules from a built-in rule set. It
supports variants (`hover:`, `focus:`, responsive breakpoints, `dark:`) and
arbitrary values (`w-[100px]`, `bg-[#ff0000]`). Configuration lives in
`tailwind.config.ts` (executed via `npx tsx`).

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

## Content Security Policy

```typescript
csp: {
  enabled: false,
  directive: "",   // custom CSP string (empty = auto-generate)
}
```

When enabled, krate computes SHA-256 hashes of inline scripts and styles and
emits them in the CSP meta tag.

## SSR / Streaming

```typescript
ssr: {
  streaming: false,          // force ALL pages to streaming SSR
  rendererPort: 0,           // Node renderer port (0 = default)
  timeout: 5000,             // max render time (ms)
  maxCacheSize: 128,         // ISR in-memory cache size
  middlewareRuntime: "quickjs", // middleware.ts runtime
  apiRuntime: "quickjs",        // API route runtime
}
```

`middlewareRuntime` and `apiRuntime` select the runtime that executes
middleware and API routes: `"quickjs"` (default) uses the embedded QuickJS
runtime (no Node.js needed), while `"node"`, `"bun"`, or `"deno"` uses a
sidecar process. The top-level `runtime` option controls which sidecar runtime
API routes use.

## Redirects & rewrites

```typescript
redirects: [
  { source: "/old-page", destination: "/new-page", permanent: true },
],

rewrites: [
  { source: "/docs/:path*", destination: "/documentation/:path*" },
],
```

Redirects produce `301`/`302` responses; rewrites map URLs to internal paths.

## Component tiers

```typescript
serverComponents: ["DataTable"],  // names to treat as @server
runtimeComponents: ["AuthCheck"], // names to treat as @runtime
serverDirs: ["src/components/server"],
runtimeDirs: ["src/components/runtime"],
```

## Plugins

```typescript
plugins: [
  sitemap({ baseUrl: "https://example.com" }),
  docs({ contentDir: "content/docs", title: "Docs" }),
  demoPlugin({ greeting: "Hello!" }),
]
```

Built-in plugins use factory functions (`sitemap`, `docs`); community plugins
are imported from local modules and take their own options object. Plugins run
in `order` sequence (lower first).

## SEO & robots

```typescript
seo: {
  baseUrl: "https://example.com",
  siteName: "Krate",
  description: "A modern static site generator",
},
robots: {
  allow: "/",
}
```

See the [Config Reference](/docs/reference/config/) for every supported key.
