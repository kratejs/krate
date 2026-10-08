---
title: Configuration
order: 3
description: The krate.config.ts entry point and a map of every option group.
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

React syntax (`useState`, `useEffect`, `useRef`, JSX) is transpiled to Krate
signals and effects automatically — no config is required. See
[React Compatibility](/docs/guides/react-compatibility/).

## Option groups

This page is a map; the [Config Reference](/docs/reference/config/) documents
every key and its default.

| Group | Key(s) | Reference |
|-------|--------|-----------|
| Build | `entry`, `outDir`, `pagesDir`, `publicDir`, `minify`, `sourcemap` | [Build](/docs/reference/config/#build) |
| Dev server | `devServer` | [Dev server](/docs/reference/config/#dev-server) |
| Rendering | `viewTransitions`, `ppr`, `ssr`, `output` | [SSR / Streaming](/docs/reference/config/#ssr-streaming) |
| CSS & Tailwind | `tailwind` | [Tailwind CSS](/docs/reference/config/#tailwind-css) |
| Markdown | `markdown` | [Markdown](/docs/reference/config/#markdown) |
| Content | `content`, `pathAliases`, `tsBaseDir` | [Content collections](/docs/reference/config/#content-collections) |
| Server | `server`, `basePath`, `redirects`, `rewrites` | [Server](/docs/reference/config/#server) |
| Security | `csp`, `cors` | [Content Security Policy](/docs/reference/config/#content-security-policy) |
| SEO | `seo`, `robots` | [SEO & robots](/docs/reference/config/#seo-amp-robots) |
| Checks | `checks` | [Quality checks](/docs/reference/config/#quality-checks) |
| Plugins | `plugins` | [Plugins](/docs/reference/config/#plugins) |
