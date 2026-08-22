---
title: Welcome
order: 1
---

# Welcome

This is your new documentation site, powered by Krate. Everything under
`content/docs/` is rendered into the docs section automatically — Markdown and
MDX files become pages, and directories become sidebar sections.

## What you get

- **Sidebar navigation** — generated from the content tree.
- **Table of contents** — extracted from your headings.
- **Breadcrumbs** and **prev/next** navigation.
- **Light/dark theme** toggle.
- **Full-text search** — a WASM index embedded at build time.
- **Code highlighting** and GFM tables, task lists, and admonitions.

## A taste

```tsx
import { createSignal } from '@krate/runtime';

export default function Counter() {
  const [count, setCount] = createSignal(0);
  return (
    <div>
      <span>{count()}</span>
      <button onClick={() => setCount(c => c + 1)}>+</button>
    </div>
  );
}
```

## Where to go next

| Goal | Page |
|------|------|
| Add your first page | [Getting started](/docs/getting-started/) |
| Configure the site | [Configuration](/docs/configuration/) |
| Deploy it | [Deployment](/docs/guides/deployment/) |
