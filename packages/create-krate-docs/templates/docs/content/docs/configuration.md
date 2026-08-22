---
title: Configuration
order: 3
---

# Configuration

The site is configured in `krate.config.ts` at the project root.

```ts
import { defineConfig, docs, sitemap } from '@krate/core';

export default defineConfig({
  outDir: "dist",
  plugins: [
    docs({
      contentDir: "content/docs",
      title: "My Docs",
      layout: "src/components/docs-layout.tsx",
      search: { enabled: true, engine: "docfind" },
    }),
  ],
});
```

## Site title

Set the `title` option to change the name shown in the docs navbar.

```ts
docs({
  title: "My Project Docs",
});
```

## Social links

Add links rendered in the navbar and sidebar:

```ts
docs({
  links: [
    { icon: "lucide:github", url: "https://github.com/you" },
  ],
});
```

Icons use the `lucide:` and `tabler:` icon sets.

## Search

Search is on by default and builds a WASM index at build time.

```ts
docs({
  search: {
    enabled: true,
    engine: "docfind",
    maxResults: 8,
  },
});
```

## Sitemap and SEO

The `sitemap` plugin generates `sitemap.xml`, and the top-level `seo` config
sets default metadata.

```ts
export default defineConfig({
  seo: {
    baseUrl: "https://example.com",
    siteName: "My Docs",
    description: "Documentation for my project.",
  },
  plugins: [
    sitemap({ baseUrl: "https://example.com" }),
  ],
});
```

See the [Krate configuration reference](/docs/configuration/) for every option.
