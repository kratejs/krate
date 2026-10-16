# @krate/web - the Krate documentation website

The official documentation site for Krate, built with Krate itself. It
showcases the `docs` plugin (sidebar, edit links, feedback, full-text search),
the typed **content collections** system (the blog), sitemap/robots/feeds, and
the `base-docs-theme` shell.

## Layout

```
src/content/docs/ Documentation content (Markdown + MDX)
src/content/blog/ Blog posts (typed `blog` content collection)
src/pages/        Site pages (landing, blog, 404, roadmap, changelog, showcase)
src/components/   Page components (the landing-page demo)
public/           Static assets (site.css, fonts, favicons, og.png, webmanifest)
krate.config.ts   Framework config (docs plugin, content collections, SEO, feeds)
```

## Development

Requires the Krate CLI (`@krate/core`, a workspace dependency).

```sh
pnpm install        # link workspace packages
pnpm dev            # dev server with hot reload on http://localhost:3000
pnpm build          # production build -> dist/
pnpm serve          # preview the production build
```

## Docs vs blog

- **Docs** (`src/content/docs/`) are rendered by the `docs` plugin: sidebar,
  table of contents, prev/next, search, and `hreflang` for translations. Add a
  page by dropping a `.md`/`.mdx` file with frontmatter (`title`, `order`,
  optional `description`, `sidebar`, `keywords`).
- **Blog** (`src/content/blog/`) is a typed content collection declared in
  `krate.config.ts` and read with `getCollection('blog')` from `src/pages/blog/`.
  The post slugs map to `/blog/<slug>/` via `generateStaticParams`.

## Search

Docs search defaults to **Pagefind** in production (a post-build pass over the
built HTML) and **docfind** (WASM) during `krate dev`, with a JSON index as the
fallback. See `docs/features/search` for the full engine matrix.
