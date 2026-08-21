---
title: Customizing the Docs Site
order: 2
---

# Customizing the Docs Site

The `docs` plugin turns a `content/docs/` directory into a full documentation
site. This guide walks through the moving parts.

## Enable the plugin

```ts
import { defineConfig, docs } from '@krate/core';

export default defineConfig({
  plugins: [
    docs({
      contentDir: "content/docs",
      title: "My Docs",
      layout: "src/components/docs-layout.tsx",
      search: { enabled: true, engine: "docfind" },
      links: [{ icon: "lucide:github", url: "https://github.com/me/project" }],
    }),
  ],
});
```

| Option | Description |
|--------|-------------|
| `contentDir` | Markdown/MDX docs directory (default `content/docs`) |
| `title` | Title shown in the docs navbar |
| `layout` | Path to your layout component |
| `sidebar` | Custom sidebar override |
| `links` | Social links rendered in the navbar |
| `search` | Search bar options (see [Search](/docs/features/search/)) |

## What the plugin generates

For every markdown page, the plugin generates a TSX page into
`.krate/gen/docs/` that wraps the rendered HTML in your layout with:

- The **sidebar tree** (from the directory structure + frontmatter `order`)
- The **table of contents** (from headings)
- **Breadcrumbs**
- **Prev/Next** navigation links
- A **SearchBar** (with the WASM search index)

It also writes `docs/data/sidebar.json` and `docs/data/search-index.json` plus
the WASM search assets under `docs/search/`.

## File conventions

```
content/docs/
  index.md               → /docs/
  getting-started.md     → /docs/getting-started/
  guides/
    index.md             → /docs/guides/
    advanced.md          → /docs/guides/advanced/
```

Sidebar sections come from directories; page order within a directory comes
from the frontmatter `order` field. Each directory's `index.md` becomes the
section landing page.

## The layout component

The layout is a normal Krate TSX component receiving props for page title,
site title, sidebar items, TOC items, breadcrumbs, prev/next links, social
links, and the current path. It renders `{children}` for the content. See
`src/components/docs-layout.tsx` in this site for a reference implementation.

The docs plugin expects a `src/components/docs/` directory with `SidebarNav`,
`TOCNav`, `Breadcrumbs`, `PrevNext`, and `SocialLinks` components.

## Theming

Override the docs UI with your own CSS:

- Global docs styles: `public/docs-styles.css` (linked by the layout).
- Search UI styles: `docs/search/search.css` (generated) — override the
  `.krate-search-*` classes in your own stylesheet.
- Interactive behavior (sidebar, TOC tracking, theme toggle) lives in
  `public/docs-script.js`.

Use CSS custom properties to retheme: `--color-primary`, `--color-bg`,
`--color-fg`, `--color-border`, `--radius`, etc.

## Writing docs content

See [Markdown & MDX](/docs/core-concepts/markdown/) for frontmatter and
content features, and [Search](/docs/features/search/) for tuning search
relevance.
