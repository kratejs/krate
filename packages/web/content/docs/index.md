---
title: Welcome to Krate
order: 1
---

# Welcome to Krate

**Krate** is a Go-native static site generator with signal-based reactivity. It
compiles TSX/JSX pages into static HTML at build time and generates a tiny
hydration bundle that makes pages interactive on the client.

No React. No external bundler subprocess. No Node.js required for core
compilation. The compiler — lexer, parser, bundler, renderer, CSS pipeline and
Tailwind generator — is 100% custom Go, and builds run in milliseconds.

## Highlights

- **Go-native compiler** — a custom lexer, parser, bundler, and renderer. Builds run in milliseconds.
- **Signals, not React** — fine-grained reactivity with `createSignal` / `createEffect` / `createMemo`.
- **SSG-first** — every page is pre-rendered to static HTML. Hydration binds signals to the DOM via `data-k`/`data-kh` markers.
- **File-based routing** — `src/pages/` maps to URLs, with nested routes, dynamic segments (`[param]`), and `_layout.tsx` layouts.
- **Component tiers** — static, client, server (`@server`), and runtime (`@runtime`, via embedded QuickJS) components in one page.
- **Full CSS pipeline** — CSS Modules (FNV-32a scoping), Go-native Tailwind, minification, and `@import` inlining.
- **Streaming SSR** — Suspense-based streaming SSR with per-request data via runtime components.
- **SPA router** — client-side navigation with DOM tree reconciliation; state, focus, and scroll survive transitions.
- **Plugin system** — Go plugin hooks plus community plugins written in JavaScript, executed inside the embedded QuickJS runtime.
- **WASM docs search** — the docs plugin ships a search bar powered by Microsoft's docfind, with the index embedded into a WASM module at build time.

## How it works

```
Source (.tsx/.ts/.md/.mdx)
        │
        ▼
    Lexer (tokenize) ──► Parser (AST) ──► Bundler (imports, CSS modules, React rewrite)
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

## Where to go next

| Goal | Page |
|------|------|
| Install and scaffold | [Getting Started](/docs/getting-started/) |
| Configuration options | [Configuration](/docs/configuration/) |
| Every CLI command | [CLI Reference](/docs/cli/) |
| Signals and effects | [Reactivity](/docs/core-concepts/reactivity/) |
| Routing and layouts | [Routing & Layouts](/docs/core-concepts/routing/) |
