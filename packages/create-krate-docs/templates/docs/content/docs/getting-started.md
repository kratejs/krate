---
title: Getting Started
order: 2
---

# Getting Started

Add documentation by creating Markdown or MDX files under `content/docs/`.
Each file becomes a page at `/docs/<path>`.

## Create your first page

Create `content/docs/hello.md`:

```md
---
title: Hello World
order: 3
---

# Hello World

This is my first docs page.
```

That's it — the page appears in the sidebar automatically.

## Frontmatter

Frontmatter controls how a page is presented:

| Field   | Description                            |
| ------- | -------------------------------------- |
| `title` | The page and sidebar title             |
| `order` | Sort position within its section       |

## Markdown features

Krate supports GitHub-flavored Markdown out of the box:

- **Bold**, *italic*, `inline code`
- Fenced code blocks with syntax highlighting
- Tables, task lists, and block quotes
- [Links](/docs/configuration/) and images

### Admonitions

```md
> **Note** — this is a callout.
```

## Local development

```bash
npm run dev
```

The dev server runs at [http://localhost:3000](http://localhost:3000) with hot
reload.
