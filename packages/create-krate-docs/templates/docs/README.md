# __PROJECT_DISPLAY_NAME__

A documentation site scaffolded with
[`create-krate-docs`](https://www.npmjs.com/package/create-krate-docs) and
powered by the Krate [`docs` plugin](https://krate.js.org/docs/features/plugins/).

It ships with a sidebar, table of contents, breadcrumbs, prev/next navigation,
light/dark theme, and WASM-powered full-text search — the same experience as the
official [krate.js.org](https://krate.js.org) docs.

## Getting started

Install dependencies and start the dev server:

```bash
npm install
npm run dev
```

Open [http://localhost:3000](http://localhost:3000) to see your docs.

## Writing docs

Add Markdown or MDX files under `src/content/docs/` — each file becomes a page
automatically. Directories become sections in the sidebar.

Use frontmatter to control the title and ordering:

```md
---
title: Getting Started
order: 1
---

# Getting Started
```

## Scripts

| Command          | Description                          |
| ---------------- | ------------------------------------ |
| `npm run dev`    | Start the dev server with hot reload |
| `npm run build`  | Build a production bundle into `dist/` |
| `npm run serve`  | Build and serve the production output |

## Project structure

```
.
├── krate.config.ts        # Krate config (docs plugin, search, SEO)
├── tsconfig.json          # TypeScript configuration
├── public/                # Styles, search script, favicon
└── src/
    ├── content/
    │   └── docs/          # Markdown/MDX documentation pages
    ├── pages/             # Home, layout, and 404 pages
    └── components/        # project components (docs shell comes from @krate/base-docs-theme)
```

## Configuration

Edit `krate.config.ts` to change the site title, add social links, point the
sitemap at your real domain, and tune search.

## Learn more

- [Krate documentation](https://krate.js.org/docs/)
- [Customizing the docs site](https://krate.js.org/docs/guides/customizing-docs/)
- [GitHub](https://github.com/kratejs/krate)
