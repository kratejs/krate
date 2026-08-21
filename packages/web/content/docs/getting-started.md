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
mkdir my-app && cd my-app
krate init
```

`krate init` scaffolds a `krate.config.ts` in the current directory.

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

Pages can declare build-time props with `getStaticProps`:

```tsx
export async function getStaticProps() {
  const res = await fetch('https://api.example.com/data');
  return { props: { data: await res.json() } };
}

export default function Page({ data }) {
  return <pre>{JSON.stringify(data, null, 2)}</pre>;
}
```

Synchronous `getStaticProps` (object literal returns) is evaluated by the
compiler directly; asynchronous ones run through an `npx tsx` bootstrap at
build time.

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
