# create-krate-docs

Scaffold a new [Krate](https://github.com/kratejs/krate) documentation site in
seconds. Ships with the same look and feel as the official
[krate.js.org](https://krate.js.org) docs — sidebar, table of contents,
breadcrumbs, prev/next navigation, dark/light theme, and WASM search.

```bash
npm create krate-docs@latest my-docs
# or
npx create-krate-docs@latest my-docs
```

Also works with `pnpm`, `yarn`, and `bun`:

```bash
pnpm create krate-docs my-docs
bun create krate-docs my-docs
```

## Usage

```
create-krate-docs [project-directory] [options]
```

| Option          | Description                              |
| --------------- | ---------------------------------------- |
| `--use-npm`     | Install dependencies with npm            |
| `--use-pnpm`    | Install dependencies with pnpm           |
| `--use-yarn`    | Install dependencies with yarn           |
| `--use-bun`     | Install dependencies with bun            |
| `--skip-install`| Skip dependency installation             |
| `--no-git`      | Do not initialize a git repository       |
| `-h, --help`    | Show help                                |
| `-v, --version` | Show the CLI version                     |

Run without a project directory to be prompted for a name.

## What you get

A ready-to-run Krate docs site, powered by the `docs` plugin:

- `krate.config.ts` — `docs()` plugin with search, social links, and SEO
- `tsconfig.json` — TypeScript configuration
- `src/components/docs-layout.tsx` — full docs layout (sidebar, TOC, prev/next)
- `src/pages/` — home, `_layout`, and 404 pages
- `content/docs/` — markdown/mdx content (add files here to create pages)
- `public/` — styles, search script, favicon

## Writing docs

Add a Markdown file under `content/docs/` and it becomes a page automatically.
Frontmatter `title` and `order` control the sidebar label and ordering:

```md
---
title: My Page
order: 2
---

# My Page
```

## License

Apache-2.0
