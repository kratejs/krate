# @krate/web — the Krate documentation website

The official documentation site for Krate, built with Krate itself. It
showcases the `docs` plugin, including the WASM-powered search bar backed by
Microsoft's [docfind](https://github.com/microsoft/docfind).

## Layout

```
content/docs/   Documentation content (Markdown + MDX)
src/pages/      Site pages (landing page, 404)
src/components/ docs-layout.tsx + the docs UI components
public/         Static assets (site + docs styles, scripts)
krate.config.ts Framework config (docs plugin + search)
```

## Development

Requires the Krate CLI (`@krate/core`, a workspace dependency).

```sh
pnpm install        # link workspace packages
pnpm dev            # dev server with hot reload on http://localhost:3000
pnpm build          # production build → dist/
pnpm serve          # preview the production build
```

## Writing docs

Add Markdown or MDX files under `content/docs/`. Frontmatter supports `title`,
`order`, `sidebar`, and optional `keywords`. See the search feature docs for how
indexing works.
