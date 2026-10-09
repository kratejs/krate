---
title: LLMs.txt
description: Emit llms.txt and llms-full.txt for AI agents and crawlers.
order: 17
---

# LLMs.txt

`llms.txt` is a growing convention that makes a docs or content site legible to
AI agents: a curated index of the site's pages, plus an optional full-text
concatenation.

Enable the built-in plugin:

```typescript
// krate.config.ts
export default defineConfig({
  plugins: [
    {
      name: "llms",
      options: {
        baseUrl: "https://example.com",
        contentDir: "src/content/docs",
        title: "My Docs",
        description: "Everything about My Project",
        // full: true,   // also emit llms-full.txt (default true)
      },
    },
  ],
});
```

`krate build` then writes two files into `outDir`:

- **`llms.txt`** - a Markdown index: the site title and description, then each
  page grouped by section as `- [Title](url): description`.
- **`llms-full.txt`** - the same header followed by every page's raw content.

`baseUrl`, `title`, and `description` fall back to `seo.baseUrl`,
`seo.siteName`, and `seo.description` (and the docs plugin's options) when
omitted. Set `full: false` to skip `llms-full.txt`. Draft pages are excluded.
