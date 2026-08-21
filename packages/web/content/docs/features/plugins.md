---
title: Plugin System
order: 5
---

# Plugin System

Krate's plugin system is a single, unified interface with **7 lifecycle
hooks**. Built-in plugins are written in Go; community plugins are JavaScript
modules executed inside the embedded QuickJS runtime — no subprocess, no
stdin/stdout protocol.

## The plugin interface

```go
type Plugin interface {
    Name() string
    Order() int                    // Lower runs first (default: 50)
    Hooks() PluginHooks
}

type PluginHooks struct {
    BeforeBuild        func(ctx *BuildHookCtx) error
    AfterParse         func(ctx *ParseHookCtx) error
    AfterMarkdownParse func(ctx *MarkdownHookCtx) error
    AfterRender        func(ctx *RenderHookCtx) error
    GenerateRoutes     func(ctx *BuildHookCtx) ([]Route, error)
    AfterPage          func(ctx *PageHookCtx) error
    AfterBuild         func(ctx *BuildResultHookCtx) error
}
```

## The 7 lifecycle hooks

| Hook | When | Mutable Context |
|------|------|-----------------|
| `BeforeBuild` | Before any pages are built | Root, Config |
| `AfterParse` | After a page is parsed (AST available) | AST, Source |
| `AfterMarkdownParse` | After markdown/MDX is parsed | HTML, Frontmatter |
| `AfterRender` | After SSR rendering | HTML, HeadHTML, Signals, Handlers |
| `GenerateRoutes` | Generate virtual pages | Routes |
| `AfterPage` | After a page is fully built | HTML, Route |
| `AfterBuild` | After all pages are built | Results, Manifest |

## Config usage (typed)

```ts
import { defineConfig, sitemap, docs } from '@krate/core';
import demoPlugin from './plugins/krate-plugin-demo';

export default defineConfig({
  plugins: [
    sitemap({ baseUrl: "https://example.com" }),
    docs({ contentDir: "content/docs", title: "Docs" }),
    demoPlugin({ greeting: "Hello!" }),
  ],
});
```

Each factory returns a **serializable descriptor**: `{ name, order, options }`
(built-ins) or `{ name, order, module, options }` (community plugins).

## Community plugin protocol

Community plugins are **JavaScript modules** executed inside the embedded
QuickJS runtime (`modernc.org/quickjs`). The module is bundled with esbuild
into a self-contained IIFE and its hooks are called directly from Go.

```javascript
// plugins/my-plugin/index.js
export const hooks = {
  BeforeBuild(ctx, options, krate) {
    return { files: [{ path: "note.txt", content: "hi" }] };
  },
  AfterRender(ctx, options, krate) {
    return {
      html: "<b>" + ctx.html,
      headHTML: "<meta ...>",
      rawCSS: ".x{}",
    };
  },
};

export default function myPlugin(options) {
  return {
    name: "my-plugin",
    order: 20,
    module: typeof import.meta !== "undefined" && import.meta.url ? import.meta.url : "",
    options: options || {},
  };
}
```

- **Hook signature** — every hook receives `(ctx, options, krate)` where `ctx`
  is the JSON-serialized hook context (lowercase fields like `ctx.html`,
  `ctx.page`, `ctx.outName`, `ctx.headHTML`, `ctx.rawCSS`) and `krate` is
  `{ root, outDir, version }`.
- **Return value** — hooks return `{ files, routes, generatedPages, html,
  headHTML, rawCSS }` (all optional; may be a Promise). `files` are written into
  the output directory (path traversal is rejected), `routes` become static
  HTML pages, `generatedPages` feed the page pipeline, and `html`/`headHTML`/
  `rawCSS` mutate the hook context.
- **Runtime capabilities** — bundled plugins can use `import fs from 'fs'` /
  `import path from 'path'` (polyfilled) plus Web API polyfills (`fetch`, `URL`,
  `Headers`, `Response`, `TextEncoder`, timers, `process.env`). Non-relative
  third-party imports are left external and unavailable.

## Built-in plugins

| Plugin | Purpose |
|--------|---------|
| `sitemap` | Generates `sitemap.xml` |
| `icons` | `<Icon>` → Iconify SVG with disk cache |
| `imageprocessing` | `<Image>` → responsive `<picture>` with srcset |
| `markdown` | Markdown/MDX compilation |
| `csp` | Content Security Policy meta tag |
| `docs` | Documentation site generator with WASM search |

See [Guides: Create a Plugin](/docs/guides/customizing-docs/) and the demo
plugin in the examples for a full walkthrough.
