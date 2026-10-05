---
title: Configuration
order: 3
description: Every krate.config.ts option, type-checked with defineConfig.
toc:
  minLevel: 2
  maxLevel: 3
  label: Config sections
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
    docs({ contentDir: "src/content/docs", title: "Docs" }),
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

React syntax (`useState`, `useEffect`, `useRef`, JSX) is always transpiled to
Krate signals and effects; no config is required. The former `emitReact` option
is accepted but ignored.

## Runtime

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `runtime` | `"node" \| "bun" \| "deno"` | `node` | Runtime for the API routes sidecar |

## Dev server

```typescript
devServer: {
  port: 3000,
  open: true,
  overlay: true,   // dev error overlay (default true)
  toolbar: true,   // dev toolbar (default true)
  editor: "code",  // editor command for "open in editor"
}
```

See [Error Handling](/docs/guides/error-handling/) for what the overlay shows.

## View transitions & partial prerendering

```typescript
viewTransitions: "auto", // native View Transitions for SPA + full-page navs
ppr: { revalidate: 60 }, // cache dynamic regions on non-ISR pages
```

See the [config reference](/docs/reference/config/#view-transitions) for
details, and [Rendering](/docs/core-concepts/rendering/) for the region model.

## Tailwind CSS

```typescript
tailwind: {
  enabled: false,
  scanDirs: ["src"],       // or content: ["./src/**/*.{tsx,mdx}"]
  preflight: false,        // opt-in base reset
  strict: false,           // warn on classes that produce no rule
  darkMode: "media",       // "media" | "class" | "selector"
  executeConfig: false,    // run tailwind.config via npx tsx instead of static parse
}
```

Krate's Tailwind is **Go-native** (no PostCSS, no Node at build time). A
candidate scanner extracts Tailwind tokens from source files and maps them to
rules from a built-in rule set. It supports variants (`hover:`, `focus:`,
`group-*`, responsive breakpoints, `dark:`, arbitrary variants), arbitrary
values (`w-[100px]`, `bg-[#ff0000]`), negatives, and color opacity modifiers
(`bg-blue-500/50`). Output is deterministic. Configuration lives in
`tailwind.config.ts` and is **statically parsed** by default; set
`executeConfig: true` to execute it via `npx tsx`. `theme.extend` merges onto
the defaults; top-level `theme` keys replace them.

This is a documented subset of Tailwind: JS plugins and `@apply` are not
supported, and unknown classes produce no rule (enable `strict` to surface
them).

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

## SSR / ISR / Streaming

```typescript
ssr: {
  streaming: false,          // force all *static* pages to streaming SSR
  ssrRuntime: "node",        // sidecar runtime: "node" | "bun" | "deno"
  rendererPort: 0,           // renderer sidecar port (0 = default)
  timeout: 5000,             // max render time (ms)
  maxCacheSize: 128,         // ISR in-memory cache size
  middlewareRuntime: "quickjs", // middleware.ts runtime
  apiRuntime: "quickjs",        // API route runtime
}
```

`ssrRuntime` picks which runtime launches the SSR sidecar that renders
SSR/ISR/streaming regions: `"node"` (default) runs the staged driver with plain
node, `"bun"` with `bun run`, and `"deno"` with the needed `--allow-*` flags.

`middlewareRuntime` and `apiRuntime` select the runtime that executes
middleware and API routes: `"quickjs"` (default) uses the embedded QuickJS
runtime (no Node.js needed), while `"node"`, `"bun"`, or `"deno"` uses a
sidecar process.

Rendering modes themselves are opted into **per page** via
`export const config = { isr | ssr | streaming, revalidate }` — see
[Rendering](/docs/core-concepts/rendering/). `ssr.streaming: true` forces all
static (SSG) pages into streaming mode; explicit per-page `isr`/`ssr` still win.

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
  docs({ contentDir: "src/content/docs", title: "Docs" }),
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
