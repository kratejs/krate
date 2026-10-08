---
title: Welcome to Krate
order: 1
description: Krate is a Go-native static site generator with signal-based reactivity. Compiles TSX/JSX to static HTML in milliseconds with a tiny client hydration bundle.
sidebar:
  label: Overview
  order: 1
template: hero
hero:
  title: Krate
  tagline: A Go-native static site generator with signal-based reactivity. Compiles TSX/JSX to static HTML in milliseconds, with no external bundler or Node build step.
  actions:
    - link: /docs/getting-started/
      text: Get started
    - link: https://github.com/kratejs/krate
      text: GitHub
      variant: secondary
tags:
  - homepage
  - overview
categories:
  - general
---

**Krate** compiles TSX/JSX pages into static HTML at build time and generates a
tiny hydration bundle that makes pages interactive on the client. Builds run in
milliseconds on a single Go binary — no bundler subprocess and no Node.js
required to build.

## Highlights

- **Static by default** — every page is pre-rendered to static HTML; hydration binds signals to the DOM via `data-k`/`data-kh` markers only where needed.
- **Fine-grained signals** — `createSignal` / `createEffect` / `createMemo`, with no virtual DOM.
- **File-based routing** — `src/pages/` maps to URLs, with nested routes, dynamic segments (`[param]`), and `_layout.tsx` layouts.
- **Component tiers** — static, client, server (`@server`), and runtime (`@runtime`, rendered at request time by the sidecar) components in one page.
- **Full CSS pipeline** — CSS Modules (FNV-32a scoping), Go-native Tailwind, minification, and `@import` inlining.
- **SSR, ISR & streaming** — every page is a static shell; ISR revalidates cached page bodies and streaming resolves dynamic regions per request.
- **SPA router** — client-side navigation with DOM tree reconciliation; state, focus, and scroll survive transitions.
- **Plugin system** — Go plugin hooks plus community plugins written in JavaScript or TypeScript, executed inside the embedded QuickJS runtime and typed with `@krate/plugin`.
- **Docs search** — the docs plugin ships a search bar that uses [Pagefind](https://pagefind.app) by default for a chunked, streamed index, with the docfind WASM engine used during development builds.

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

The `<span>{count()}</span>` becomes server-rendered HTML at build time, and the
hydration bundle registers an effect that updates just that text node when
`setCount` is called.

Curious how the compiler turns that source into HTML? See
[Contributing → Architecture](/docs/contributing/architecture/).

## Where to go next

| Goal | Page |
|------|------|
| Install and scaffold | [Getting Started](/docs/getting-started/) |
| Configuration options | [Configuration](/docs/configuration/) |
| Every CLI command | [CLI Reference](/docs/cli/) |
| Signals and effects | [Reactivity](/docs/core-concepts/reactivity/) |
| Routing and layouts | [Routing & Layouts](/docs/core-concepts/routing/) |
| Coming from React | [React Compatibility](/docs/guides/react-compatibility/) |
