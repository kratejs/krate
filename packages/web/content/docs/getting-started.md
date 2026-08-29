---
title: Getting Started
order: 2
---

# Getting Started

This guide walks you through installing the Krate CLI, scaffolding a project,
and building your first page.

## Prerequisites

- **Node.js 20+** — for the npm CLI wrapper and running `krate.config.ts`
- **Go 1.26+** — only needed when building the compiler from source

The published CLI ships pre-built binaries for macOS, Linux and Windows, so you
do not need Go to build a site.

## Install the CLI

```sh
npm install -g @krate/core
# or
pnpm add -g @krate/core
```

## Create a project

```sh
krate init my-app
# or, using the create-krate-app scaffold directly:
npx create-krate-app@latest my-app
```

`krate init` (alias `krate create`) scaffolds a complete project — including
`krate.config.ts`, `tsconfig.json`, `package.json`, a landing page, a layout, and
a 404 page. With no directory it prompts for a name.

Add a page:

```tsx
// src/pages/index.tsx
export default function Home() {
  return <h1>Hello, World!</h1>;
}
```

## Build and run

```sh
krate dev       # dev server with hot reload on http://localhost:3000
krate build     # production build → dist/
krate serve     # preview the production build
```

## Your first interactive page

Krate's reactivity is built on **signals**. A signal is a getter/setter pair;
reading a signal inside an effect subscribes that effect to it.

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

- `count()` **reads** the current value.
- `setCount(c => c + 1)` **writes** a new value and notifies subscribers.
- At build time the renderer extracts the initial value and emits static HTML.
- At runtime the hydration bundle wires the button to update the `<span>`.

## Data fetching

Krate provides data through component tiers rather than page-level data
functions:

```tsx
// @server — evaluated at build time, HTML baked in
export default function ServerTime() {
  return <time>{new Date().toUTCString()}</time>;
}
```

```tsx
// @runtime — evaluated per request, streamed via Suspense
export default function PriceTag({ price }) {
  return <span>{price}</span>;
}
```

Dynamic route pages receive their params as props via `generateStaticParams`.
See [Data Fetching](/docs/features/data-fetching/) for the full model.

## Layouts

A `_layout.tsx` file wraps every page in its directory (and subdirectories):

```tsx
// src/pages/_layout.tsx
export default function Layout({ children }) {
  return (
    <div>
      <nav><a href="/">Home</a></nav>
      <main>{children}</main>
    </div>
  );
}
```

## Dynamic routes

Files named `[param].tsx` become dynamic segments:

```tsx
// src/pages/video/[id].tsx → /video/abc123
export default function VideoPage({ params }) {
  return <h1>Video {params.id}</h1>;
}
```

## Built-in components

Built-in components (`Head`, `Link`, `Icon`, `Image`, `Script`, `Style`) are
recognized by the compiler by name — no import needed:

```tsx
export default function Page() {
  return (
    <>
      <Head>
        <title>My Page</title>
        <meta name="description" content="A cool page" />
      </Head>
      <Icon name="lucide:heart" />
      <Image src="/photo.jpg" width={800} alt="Photo" />
      <Link href="/about">About</Link>
    </>
  );
}
```

`<Image>` compiles to a WebP-first `<picture>`: lossy WebP variants generated at
build time with original-format fallback, responsive `srcset`/`sizes`,
lazy/eager loading, blur placeholders, and `aspect-ratio`-based CLS mitigation.
`<Link>` renders an SPA-enabled `<a>` with prefetching, `replace`, `scroll`,
hash scrolling, and `aria-current` for the active route.

## Next steps

- Explore [Configuration](/docs/configuration/) for the full config surface.
- Learn [Reactivity](/docs/core-concepts/reactivity/) in depth.
- See [Routing & Layouts](/docs/core-concepts/routing/) for routing features.
