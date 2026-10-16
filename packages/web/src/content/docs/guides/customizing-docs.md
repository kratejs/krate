---
title: Customizing the Docs Site
description: Theme and extend the docs plugin.
order: 2
---

# Customizing the Docs Site

The `docs` plugin turns a `src/content/docs/` directory into a full documentation
site. This guide walks through the moving parts.

## Enable the plugin

```ts
import { defineConfig, docs } from '@krate/core';
import { baseDocsTheme } from '@krate/base-docs-theme';

export default defineConfig({
  plugins: [
    docs({
      contentDir: "src/content/docs",
      title: "My Docs",
      theme: baseDocsTheme(),
      search: { enabled: true, engine: "pagefind" }, // recommended; "docfind" | "json"
      links: [{ icon: "lucide:github", url: "https://github.com/me/project" }],
    }),
  ],
});
```

| Option | Description |
|--------|-------------|
| `contentDir` | Markdown/MDX docs directory (default `src/content/docs`) |
| `title` | Title shown in the docs navbar |
| `layout` | Path to your layout component |
| `theme` | Alias for `layout` (path, installed npm package, or factory — see [Theming](#theming)) |
| `sidebar` | Custom sidebar override |
| `links` | Social links rendered in the navbar |
| `search` | Search bar options (see [Search](/docs/features/search/)) |

## What the plugin generates

For every markdown page, the plugin generates a TSX page into `.krate/gen/docs/`
that renders your layout component with a single typed props object
(`DocsLayoutProps` from `@krate/plugin`) carrying:

- The **sidebar tree** (from the directory structure + frontmatter `order`)
- The **table of contents** (from headings)
- **Breadcrumbs**
- **Prev/Next** navigation links
- A **SearchBar** (with the WASM search index)

The plugin does not inject any chrome itself: the theme (layout component) owns
the entire page shell — its JSX is the only markup around the rendered content.
The generated page passes render props only, so a theme can lay out the sidebar,
breadcrumbs, search, and footer however it likes.

It also writes `docs/data/sidebar.json` and `docs/data/search-index.json` plus
the WASM search assets under `docs/search/`.

## File conventions

```
src/content/docs/
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

The layout is a normal Krate TSX component receiving a single `DocsLayoutProps`
object (`title`, `pageTitle`, `sidebarItems`, `tocItems`, `breadcrumbs`,
`prev`/`next`, `socialLinks`, `currentPath`, and optional `options`). It renders
`{children}` for the content and owns all surrounding chrome.

## Theming

The default `@krate/base-docs-theme` package ships the stock shell — navbar,
sidebar, table of contents, breadcrumbs, prev/next, and light/dark mode — as a
single composable component. Use its `baseDocsTheme()` factory:

```ts
import { baseDocsTheme } from '@krate/base-docs-theme';

docs({
  contentDir: "src/content/docs",
  theme: baseDocsTheme(),
});
```

You can swap the whole docs shell with the `theme` option instead of `layout`.
A theme is one of:

- **A path** (`"./src/components/my-layout.tsx"` or an absolute path) — a plain
  layout component, exactly like `layout`. Setting both `layout` and a
  `theme` that points elsewhere is an error.
- **An installed npm package** — a bare specifier like `"@krate/base-docs-theme"`
  is emitted as-is, so the theme's own CSS and sub-components flow through the
  bundler graph. Resolution walks up from the project root through
  `node_modules`.
- **A factory descriptor** via `defineDocsTheme` from `@krate/plugin`:

```ts
import { defineDocsTheme } from '@krate/plugin';

export default defineDocsTheme({
  name: "my-docs-theme",
  // layout?: string                     // override component path
  // module: string                      // default: this file (import.meta.url)
  options: { primaryColor: "teal" },     // forwarded as docsProps.options
});
```

Theme options are forwarded to the component as `docsProps.options`, so one
published theme can be configured per-site without forking it.

The theme's layout component is whatever the module **default-exports**; pages
import it and pass every docs data field in as props. It can render that data
itself or hand it to imported sub-components — nothing about the layout is
hardcoded by the docs plugin.

### Styling

Docs pages are built exactly like normal pages, so a layout's CSS is imported
the normal way and bundled into the site's global stylesheet:

```tsx
// In your docs layout component:
import "./docs-theme.css";
```

Docs pages link that global stylesheet automatically — no manual
`<link>` and no special `docs-styles.css` file. This works for relative CSS
imports in the project (`./theme.css`), from an installed theme package, or for
site-wide styles in a page/layout module.

Notes:

- Search UI styles live in `docs/search/search.css` (generated) — override the
  `.krate-search-*` classes from your own imported stylesheet.
- Interactive behavior (sidebar, TOC tracking, theme toggle) lives in the theme
  component itself (`@krate/base-docs-theme`), not a separate script.

Use CSS custom properties to retheme: `--color-primary`, `--color-bg`,
`--color-fg`, `--color-border`, `--radius`, etc.

## Writing docs content

See [Markdown & MDX](/docs/core-concepts/markdown/) for frontmatter and
content features, and [Search](/docs/features/search/) for tuning search
relevance.
