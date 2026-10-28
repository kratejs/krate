---
title: Async Server Components
description: Async components that fetch data per request, and "use client" boundaries inside them.
order: 18
---

# Async Server Components

An `async` function component is a **server component**. It can `await`
anything (`fetch`, a database client, `cookies()`), renders per request in the
SSR sidecar (Node, bun or deno), and never ships JavaScript to the browser.
No directive is needed: `async` is the marker.

```tsx
// src/pages/post/[slug].tsx
import { cookies, notFound } from "@krate/runtime/server";

export default async function Post({ params, searchParams }) {
  const { slug } = await params; // `params.slug` works too
  const post = await fetch(`https://api.example.com/posts/${slug}`).then((r) => r.json());
  if (!post) notFound();
  const theme = (await cookies()).get("theme")?.value;
  return (
    <article class={theme}>
      <h1>{post.title}</h1>
      <Comments id={post.id} />
    </article>
  );
}

async function Comments({ id }) {
  const list = await fetch(`https://api.example.com/comments/${id}`).then((r) => r.json());
  return <ul>{list.map((c) => <li>{c.body}</li>)}</ul>;
}
```

## Async pages

When the page component itself is async, the whole page body renders per
request:

- The page is **SSR**, or **ISR** when it sets `export const revalidate = 60`
  (or `export const config = { isr: true, revalidate: 60 }`).
- The static shell contains the layout around an empty page region; there is
  no build-time body and no hydration script.
- Async children render at any depth. Siblings resolve in parallel and
  `<Suspense>` renders its resolved children (the response is sent when the
  page has rendered).

## Async children of a static page

An async component used from a synchronous page becomes its own request-time
region. The rest of the page is baked into the static shell at build time, and
the page is served in streaming mode (`[KR7104]` says so in the build output).
Inside `<Suspense fallback>`, the fallback is baked into the shell and
replaced by the rendered component.

Because the shell is built once, the props of such a component must be known at
build time (literals or module constants), and it cannot receive children
(`[KR7105]`). Make the page `async` when the props come from request data.

## Request APIs

These come from `@krate/runtime/server` and work while a server component
renders (and inside server actions):

| API | Behavior |
|-----|----------|
| `params`, `searchParams` props | Plain objects that can also be awaited (`await params`), as in Next.js 15 |
| `headers()` | Request `Headers`; also awaitable |
| `cookies()` | Read-only view: `get(name)` returns `{ name, value }`, plus `getAll()` / `has()`; `set()` throws during a render |
| `notFound()` | Stops rendering and answers 404 (the custom 404 page when present) |
| `redirect(url)` | Stops rendering and redirects |
| `fetch` | The runtime's `fetch` |

## Errors

An error thrown by a server component (or a failed `fetch`) answers **500**
with the `X-Krate-Error: render` header and the custom `500.html` when the
project has one. A server component never renders as an empty string, and the
synchronous `renderToString` throws if it meets a Promise.

## Client components inside server components

A module that starts with `"use client"` is a client boundary. When a server
component renders one of its exports, the server renders its initial HTML
into a `<krate-island>` element with the serialized props, and a small
per-module chunk hydrates it in the browser:

```tsx
// src/components/Counter.tsx
"use client";
import { useState } from "react";

export default function Counter({ start, children }) {
  const [n, setN] = useState(start);
  return <div><button onClick={() => setN(n + 1)}>{n}</button>{children}</div>;
}
```

```tsx
// src/pages/index.tsx
import Counter from "../components/Counter";

export default async function Home() {
  const start = await getStartValue();
  return <Counter start={start}><ServerNote /></Counter>;
}
```

- Client components run with React semantics through Preact (the same engine
  as [external React islands](/docs/guides/react-compatibility/)), so the
  project needs `preact` installed (`pnpm add preact`).
- Props must be JSON-serializable: strings, numbers, booleans, `null`, plain
  objects and arrays. Server actions may be passed and stay callable. A
  function, class instance (including `Date`), symbol or JSX prop fails the
  build when it is written inline (`[KR7111]`) and fails the request with the
  prop name otherwise.
- `children` passed from a server component are rendered on the server and
  inserted as HTML inside the client component.
- Pages without server components are unaffected: there, `"use client"`
  changes nothing and components compile through the normal Krate pipeline.

## Server-only code

Inside a server tree, hooks (`useState`, `useEffect`, `useContext`, ...),
Krate signals and event handlers are build errors (`[KR7110]`): add
`"use client"` to the module that needs them. `import "server-only"` and
`import "client-only"` are accepted markers; a `"use client"` module that
reaches a `server-only` module (directly or through its imports) fails the
build (`[KR7112]`).

## Not supported

| Pattern | Result |
|---------|--------|
| `async` layouts | Build error `[KR7101]`: move the fetching into an async child |
| Server components with `output: "static"` | Build error `[KR7103]` |
| Module-level `await` in a page rendered at build time | Build error `[KR7102]` |
| `async` components in a `"use client"` module | Build error `[KR7113]` |
| `export * from` in a `"use client"` module | Build error `[KR7114]` |
| Error boundaries around server components | Not applied on the server: the request answers 500 |
| Out-of-order streaming of `<Suspense>` fallbacks | The response is sent once every region has rendered |
| Client-side (SPA) navigation into a page with islands | Not covered: the island loader runs on full page loads |

See the [diagnostics reference](/docs/reference/diagnostics/) for every code.
