---
title: Core Concepts
order: 1
description: How Krate builds sites: signals, static-first rendering, and a single Go compiler.
sidebar:
  label: Concepts
  order: 1
  collapsible: true
  defaultOpen: true
---

# Core Concepts

Three ideas shape everything else in Krate:

1. **Signals, not hooks.** State is a getter/setter pair; effects subscribe to
   the exact signals they read. There is no virtual DOM diffing on the client.
2. **Static-first.** Every page is evaluated at build time to static HTML. The
   runtime hydration bundle only binds the dynamic parts.
3. **One Go binary.** The lexer, parser, bundler, renderer, CSS pipeline, and
   Tailwind are all built into the compiler.

:::tip
Coming from React? Krate transpiles React hooks and JSX automatically and
auto-calls bare reads like `{count}`. See
[React Compatibility](/docs/guides/react-compatibility/).
:::

## Reactivity model

Signals are the unit of state. `createSignal` returns a `[getter, setter]` pair:

```tsx
const [count, setCount] = createSignal(0);
count();             // read
setCount(1);         // write
setCount(c => c + 1) // functional update
```

Effects re-run when the signals they read change. Memos cache derived values.
Context provides dependency injection. Resources handle async data. See
[Reactivity](/docs/core-concepts/reactivity/) for the full API.

## Rendering modes

Every page is pre-rendered at build time into a static shell. Depending on the
page's exports and config, the final HTML can be:

- **SSG** (default) — fully static HTML, no runtime data.
- **ISR** — static shell; the page body is cached per URL variant and
  revalidated in the background.
- **SSR** — static shell; the page body is rendered fresh per request.
- **Streaming** — static shell with dynamic regions (runtime components /
  Suspense) that the sidecar renders and splices in, streaming as they resolve.

All four modes are static-first: only the page's dynamic regions (or its whole
body, for ISR/SSR) are rendered at request time. See
[Rendering](/docs/core-concepts/rendering/) for details.

## Component tiers

Components are classified into four tiers — static, client, server, and
runtime — that determine how and where they render. See
[Component Tiers](/docs/core-concepts/component-tiers/).

## Guides

| Guide | What it covers |
|-------|----------------|
| [Reactivity](/docs/core-concepts/reactivity/) | Signals, effects, memos, context, resources |
| [Routing & Layouts](/docs/core-concepts/routing/) | File-based routing, dynamic routes, layouts |
| [Component Tiers](/docs/core-concepts/component-tiers/) | Static / client / server / runtime |
| [Rendering](/docs/core-concepts/rendering/) | SSG, ISR, SSR & streaming |
| [Styling](/docs/core-concepts/styling/) | CSS Modules, Tailwind, the CSS pipeline |

Want the internals (lexer → parser → bundler → renderer)? See
[Contributing → Architecture](/docs/contributing/architecture/).
