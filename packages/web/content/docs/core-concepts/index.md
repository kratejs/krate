---
title: Core Concepts
order: 1
---

# Core Concepts

This section explains how Krate thinks about building sites. The mental model
is different from React, Next.js, or Astro in three important ways:

1. **Signals, not hooks.** State is a getter/setter pair; effects subscribe to
   the exact signals they read. There is no virtual DOM diffing on the client.
2. **SSG-first.** Every page is evaluated at build time to static HTML. The
   runtime hydration bundle only binds the dynamic parts.
3. **A real compiler.** Lexer, parser, bundler, renderer, CSS pipeline and
   Tailwind are all custom Go. Pages are not bundled by a JS bundler at runtime.

## The pipeline

```
Source (.tsx/.ts/.md/.mdx)
        │
        ▼
    Lexer (tokenize)
        │
        ▼
    Parser (AST)
        │
        ▼
    Bundler (resolve imports, CSS modules, React rewrite)
        │
        ▼
    Renderer (SSR: AST → HTML + signal/handler detection)
        │
        ▼
    Hydration Codegen (signals → JS bundle with data-k/data-kh bindings)
        │
        ▼
    Build Output (HTML + hashed JS + hashed CSS + manifest.json)
```

## Reactivity model

Krate is **not React**. `createSignal` returns a `[getter, setter]` pair:

```tsx
const [count, setCount] = createSignal(0);
count();             // read
setCount(1);         // write
setCount(c => c + 1) // functional update
```

Effects re-run when the signals they read change. Memos cache derived values.
Context provides dependency injection. Resources handle async data.

React compatibility exists through `emitReact: true`, which rewrites React
imports/primitives to Krate equivalents for gradual migration.

## Rendering modes

Every page is pre-rendered at build time. Depending on the page's exports and
config, the final HTML can be:

- **SSG** (default) — static HTML, no runtime data.
- **SSR** — rendered per request by a Node sidecar (`getServerSideProps`).
- **ISR** — static with revalidation (`getStaticProps` + `revalidate`).
- **Streaming** — Suspense-based two-phase rendering (runtime components).

See [Rendering](/docs/core-concepts/rendering/) for details.

## Component tiers

Components are classified into four tiers — static, client, server, and
runtime — that determine how and where they render. See
[Component Tiers](/docs/core-concepts/component-tiers/).

| Guide | What it covers |
|-------|----------------|
| [Reactivity](/docs/core-concepts/reactivity/) | Signals, effects, memos, context, resources |
| [Routing & Layouts](/docs/core-concepts/routing/) | File-based routing, dynamic routes, layouts |
| [Component Tiers](/docs/core-concepts/component-tiers/) | Static / client / server / runtime |
| [Rendering](/docs/core-concepts/rendering/) | SSG, SSR, ISR, streaming |
| [Styling](/docs/core-concepts/styling/) | CSS Modules, Tailwind, the CSS pipeline |
| [Markdown & MDX](/docs/core-concepts/markdown/) | Content authoring |
