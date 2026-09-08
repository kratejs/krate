---
title: Plugin System
order: 5
---

# Plugin System

Krate's plugin system is a single, unified interface with **9 hooks** across
two runtimes. Built-in plugins are written in Go; community plugins are either
JavaScript modules executed inside the embedded QuickJS runtime — no subprocess,
no stdin/stdout protocol — **or** Go plugins run as a subprocess over HashiCorp
`go-plugin`.

## Community plugin protocol

Community JS plugins are JavaScript **or TypeScript** modules executed inside
the embedded QuickJS runtime (`modernc.org/quickjs`). The module is bundled with
esbuild (which transpiles `.ts`/`.tsx`) into a self-contained IIFE and its hooks
are called directly from Go. Plugin modules may be plain `.js`, `.mjs`, `.cjs`
or TypeScript `.ts`/`.tsx`; a plugin *directory* resolves its entry point in the
same order (`index.js`, `index.ts`, `plugin.tsx`, ...).

Plugins can be authored with full type-safety via the
**`@krate/plugin`** package, which ships typed hook contexts, outputs, and
descriptors plus compile-time-only helpers (`definePlugin`, `definePluginHooks`,
`defineGoPlugin`). The helpers are erased when the module is bundled, so they
add no runtime cost inside QuickJS.

```ts
// plugins/my-plugin/index.ts
import { definePlugin, definePluginHooks } from "@krate/plugin";
import type { Krate, PluginOutput } from "@krate/plugin";

interface MyPluginOptions {
  greeting?: string;
}

export const hooks = definePluginHooks({
  BeforeBuild(ctx, options, krate: Krate): PluginOutput {
    return { files: [{ path: "note.txt", content: "hi" }] };
  },
  AfterRender(ctx, options, krate: Krate): PluginOutput {
    return {
      html: "<b>" + ctx.html,
      headHTML: "<meta ...>",
      rawCSS: ".x{}",
    };
  },
});

export default function myPlugin(options: MyPluginOptions = {}) {
  return definePlugin({
    name: "my-plugin",
    order: 20,
    module: typeof import.meta !== "undefined" && import.meta.url ? import.meta.url : "",
    options,
  });
}
```

- **Hook signature** — every hook receives `(ctx, options, krate)` where `ctx`
  is the JSON-serialized hook context (lowercase fields like `ctx.html`,
  `ctx.page`, `ctx.outName`, `ctx.headHTML`, `ctx.rawCSS`) and `krate` is
  `{ root, outDir, version }`. The `@krate/plugin` types name these
  `BuildContext`, `ParseContext`, `MarkdownContext`, `RenderContext`,
  `PageContext`, `BuildResultContext`, `ServeRequestContext`, and
  `ServeResponseContext`.
- **Return value** — hooks return `{ files, routes, generatedPages, html,
  headHTML, rawCSS, scripts, metaTags, ast }` (all optional; may be a Promise;
  `PluginOutput` in `@krate/plugin`). `files` are written into the output
  directory (path traversal is rejected), `routes` become static HTML pages,
  `generatedPages` feed the page pipeline, and
  `html`/`headHTML`/`rawCSS`/`scripts`/`metaTags` mutate the hook context.
  At `AfterParse`, `ctx.program` is the kind-tagged AST document; mutate it and
  return it as `ast` to rewrite the tree (same capability as Go plugins).
- **Runtime capabilities** — bundled plugins can use `import fs from 'fs'` /
  `import path from 'path'` (polyfilled) plus Web API polyfills (`fetch`, `URL`,
  `Headers`, `Response`, `TextEncoder`, timers, `process.env`). Non-relative
  third-party imports are left external and unavailable.

## Go plugins

Community plugins can also be **Go programs** run as a subprocess via HashiCorp
`go-plugin`. They use the public SDK
(`github.com/kratejs/krate/packages/compiler/pluginsdk`, package
`plug`) and are distributed as an npm package whose descriptor reports
`runtime: 'go'` and per-platform binary paths:

```javascript
module.exports = function () {
  return {
    name: "my-plugin",
    module: (typeof import.meta !== 'undefined' && import.meta.url) ? import.meta.url : '',
    runtime: "go",
    binaries: { "linux-amd64": "bin/my-plugin-linux-amd64" },
  };
};
```

`module` points at the descriptor module itself so the `binaries` paths resolve
against the package root.

Go plugins are trusted dependencies and run with full subprocess access. They
can edit the parsed AST (`ctx.Program` is a live `*ast.Program`), and their
serve hooks handle request-time transforms.

## Serve hooks

Two additional request-time hooks run in the dev server: `ServeRequest`
(mutate or short-circuit an incoming request before it is served) and
`ServeResponse` (rewrite a non-streaming response after it has been buffered).
Streaming responses pass through unchanged.

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
(built-ins), `{ name, order, module, options }` (JS community plugins), or
`{ name, order, module, runtime: "go", hooks, binaries }` (Go plugins). The
`@krate/plugin` helpers type each shape: `definePlugin` for JS community
plugins, `defineGoPlugin` for Go-plugin descriptors.

## Built-in plugins

| Plugin | Purpose |
|--------|---------|
| `sitemap` | Generates `sitemap.xml` |
| `icons` | `<Icon>` → Iconify SVG with disk cache |
| `imageprocessing` | `<Image>` → responsive `<picture>` with srcset |
| `markdown` | Markdown/MDX compilation |
| `csp` | Content Security Policy meta tag |
| `docs` | Documentation site generator with WASM search |

See [Guides: Creating Plugins](/docs/guides/creating-plugins/) for an authoring
walkthrough (JS and Go), [Guides: Customizing Docs](/docs/guides/customizing-docs/),
and the demo plugins in the examples (`examples/plugins/`).
