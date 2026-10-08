---
title: Writing docs
order: 4
description: Authoring conventions for the Krate documentation site.
---

# Writing docs

This site's content lives under `packages/web/src/content/docs/`. Pages are
Markdown (`.md`) or MDX (`.mdx`) and compile to static HTML. To change them,
edit the files and run `krate build` from `packages/web` (or `pnpm dev`).

After editing content, regenerate the copy embedded in the compiler binary:

```sh
node scripts/sync-krate-docs.mjs   # or: pnpm sync:docs
node scripts/sync-krate-docs.mjs --check   # CI check
```

## Frontmatter

Every page starts with YAML frontmatter:

```md
---
title: My Page
description: One sentence describing the page (used for meta/OG tags).
order: 2
---
```

| Key | Purpose |
|-----|---------|
| `title` | Page title (sidebar and `<title>`) |
| `description` | One-line summary for meta/OG tags — **required** on every page |
| `order` | Sort order within a directory (default `999`; must be unique per directory) |
| `sidebar` | Section override on an `index.md` (`label`, `order`, `collapsible`, `defaultOpen`) |
| `next` | Override the bottom "next" link (`text`, `link`) |
| `template` | Layout, e.g. `hero` |
| `hero` | Hero content on `template: hero` pages (`title`, `tagline`, `actions`) |
| `toc` | Table-of-contents options (`minLevel`, `maxLevel`, `label`) |
| `tags` / `categories` | Taxonomy terms (generates `/docs/tags/…` and `/docs/categories/…`) |
| `badge` | Small sidebar badge next to the title |
| `head` | Extra `<head>` markup |

Directories become sidebar sections via their `index.md`'s `sidebar` frontmatter.
Add each new page to its section index table.

## Style

- Write for the reader's task, not the implementation. Explain **what a feature
  does and how to use it**; leave architecture and rationale to this
  [Contributing](/docs/contributing/) section.
- Keep one idea per paragraph and prefer short, imperative sentences.
- Every page needs a `description`.
- Prefer tables for option/prop references and fenced code for examples.
- Use admonitions sparingly:

  ```md
  :::note
  A neutral, supplementary note.
  :::

  :::tip
  A helpful suggestion.
  :::

  :::warning
  Something that can bite you.
  :::
  ```

- Link between pages with root-relative URLs (e.g. `/docs/core-concepts/routing/`).
  Moving a page means updating its links and adding a redirect in
  `packages/web/krate.config.ts`.

## Adding a page

1. Create `<section>/my-page.md` with `title`, `description`, and a unique
   `order`.
2. Add a row to the section's `index.md` table.
3. Run `krate build` from `packages/web` to verify, then
   `node scripts/sync-krate-docs.mjs`.
