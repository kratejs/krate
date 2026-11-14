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

## Compiled React packages in server components

Components from npm packages built on React (lucide-react icons, Radix
primitives, shadcn-style wrappers around them) work in async pages, async
layouts and async components. Krate treats such a package like Next.js treats a
package with a `"use client"` directive: its components are client boundaries
in a server tree, rendered with Preact on the server and sent as HTML.

```tsx
import { Loader2 } from "lucide-react";
import { Dialog } from "radix-ui";
import { Checkbox } from "../components/ui/checkbox"; // wraps a Radix primitive

export default async function Settings() {
  const prefs = await loadPrefs();
  return (
    <main>
      <Loader2 className="spin" />
      <Checkbox defaultChecked={prefs.email} />
      <Dialog.Root>
        <Dialog.Trigger>Delete</Dialog.Trigger>
        <Dialog.Portal><Dialog.Content>...</Dialog.Content></Dialog.Portal>
      </Dialog.Root>
    </main>
  );
}
```

- A component without state or effects (an icon) renders to plain HTML and
  ships no JavaScript, in any tier.
- A component with state or effects (a checkbox, a dialog) becomes a
  `<krate-island>` that hydrates in the browser, like a `"use client"`
  component. Its chunk is built from just the exports the page uses, so a
  barrel such as lucide-react does not pull in every icon.
- Parts of one compound component keep their context: `Dialog.Trigger` and
  `Dialog.Content` written inside `Dialog.Root` render in the same Preact tree
  as the root, so they hydrate and talk to each other. Server components
  placed between the parts are rendered to HTML, so the parts on either side
  of one no longer share a tree.
- Props follow the `"use client"` rules: JSON-serializable values and server
  actions, no inline functions (`[KR7111]`). Put handlers in a `"use client"`
  wrapper, as in Next.js.
- A local module that renders such components needs no directive unless it
  uses hooks or event handlers. Calling a hook of an npm React library
  (zustand, react-hook-form, swr, ...) outside a `"use client"` module is
  `[KR8013]`.
- The global `<Link>` works in server components, async layouts and inside
  `"use client"` modules.

The bundle cost is the Preact runtime (shared across pages) plus the chunks of
the islands a page actually renders. A page with only icons and server
components ships none of it.

## Async layouts

A layout may be `async`. It renders per request in the SSR sidecar, so it can
await a session or flags and gate its subtree:

```tsx
// src/pages/admin/_layout.tsx
import { redirect } from "@krate/runtime/server";

export default async function AdminLayout({ children }) {
  const user = await getSession();
  if (user?.role !== "admin") redirect("/login");
  return <section class="admin">{children}</section>;
}
```

- `redirect()`, `notFound()`, `forbidden()`, `cookies()` and `headers()` work
  in a layout exactly as in an async page, and stop the whole subtree.
- The page and every layout between it and the outermost async layout render
  per request as one tree. Layouts above the outermost async layout stay baked
  into the static shell. Pages outside the async layout's directory stay static.
- Every page under an async layout therefore renders per request (SSR, or ISR
  when it sets `revalidate`), whether or not the page is itself async, and the
  build prints which pages that applies to. Pages and layouts inside it follow
  the server component rules: interactive parts go in `"use client"` modules
  (`[KR7110]`).
- An `error.tsx` between the page and an async layout is the error boundary of
  that page, as in Next.js. When the page (or a layout below the boundary)
  throws while rendering, the response is `200` with the error page rendered
  inside the async layout, and the error page hydrates so `reset` works
  (it reloads the page). The error prop is `{ name, message, digest }` with a
  generic message; the real error and its stack go to the server log with the
  same digest. `redirect()`, `notFound()`, `forbidden()` and `unauthorized()`
  are not caught. The async layout itself sits outside the boundary: if it
  throws, the error goes to the next `error.tsx` above it (or the `500` page).
- A `template.tsx` between the page and an async layout is `[KR7101]`: it
  hydrates in the browser. Put it above the async layout.
- Layouts below an async layout are not kept mounted across client-side
  navigation; the router replaces the whole request-time region.

## Server-only code

Inside a server tree, hooks (`useState`, `useEffect`, `useContext`, ...),
Krate signals and event handlers are build errors (`[KR7110]`): add
`"use client"` to the module that needs them. `import "server-only"` and
`import "client-only"` are accepted markers; a `"use client"` module that
reaches a `server-only` module (directly or through its imports) fails the
build (`[KR7112]`). In every server context (SSR bundles, route handlers,
server actions, middleware, and the build-time passes for metadata routes,
`generateMetadata` and `generateStaticParams`) `server-only` is a no-op, so the
npm package's "cannot be imported from a Client Component module" error never
fires there. `client-only` is a no-op everywhere: Krate does not report it when
a server context imports it.

## Error boundaries and streaming

A class error boundary around server components catches a failed child render
and renders its fallback on the server instead of answering 500. The boundary
receives the error through `getDerivedStateFromError` and may call
`componentDidCatch` for logging.

Sibling `<Suspense>` regions stream as they finish. The shell and fallbacks are
sent immediately, each region chunk inserts itself at its marker, and page
hydration scripts run after every chunk, so a slow region never blocks the rest
of the page. Nested boundaries and coarse SSR/ISR page regions stay on the
ordered path because they carry status or contain child markers. See
[head tags in request-time pages](#head-tags-in-request-time-pages) for head tags set
inside a streamed region.

## Head tags in request-time pages

`<title>`, `<meta>`, `<link>` and `<script>` inside `<Head>` of an async page or an
async component are in the `<head>` of the first bytes of the response (SSR, ISR,
streaming pages and async components alike, in `krate start` and on the serverless
targets), so crawlers and social scrapers that do not run scripts read them:

```tsx
export default async function Post({ params }) {
  const { slug } = await params;
  const post = await getPost(slug);
  return (
    <main>
      <Head>
        <title>{post.title}</title>
        <meta name="description" content={post.summary} />
        <meta property="og:image" content={post.image} />
        <link rel="canonical" href={`https://example.com/blog/${slug}`} />
        <script type="application/ld+json">{JSON.stringify(post.jsonLd)}</script>
      </Head>
      <h1>{post.title}</h1>
    </main>
  );
}
```

- A tag of the page **replaces** a baked tag with the same key: `meta name`,
  `meta property`, `meta http-equiv`, `meta charset`, `link rel="canonical"` and
  `base` (a layout's or `head` config's description loses to the page's). Several
  `property` tags of the page (`og:image`, `article:tag`) all stay; a repeated
  `name`, `canonical` or `charset` keeps the last one.
- A tag identical to one already in the head is not added twice. Only `meta`,
  `link`, `base`, `script`, `style` and `noscript` are head content; anything else
  inside `<Head>` is dropped. Attribute values are escaped, and the text of a
  `<script>` or `<style>` is written as is (so JSON-LD stays valid JSON), with
  `</script` neutralised.
- The title replaces the baked `<title>`, or is added when the shell has none.
- A `<Head>` inside a `<Suspense>` region that **streams** out of order runs after
  the head was sent. Its tags are applied by the region's script, which a crawler
  that does not run scripts never sees, and the server logs a warning once per
  region. Put SEO tags in the page component outside `<Suspense>`. A region that
  is not streamed out of order (ISR pages, nested boundaries, a bare async
  component) gets its tags into the initial head.

## Not supported

| Pattern | Result |
|---------|--------|
| `template.tsx` or `error.tsx` below an async layout | Build error `[KR7101]` |
| Server components with `output: "static"` | Build error `[KR7103]` |
| Module-level `await` in a page rendered at build time | Build error `[KR7102]` |
| `async` components in a `"use client"` module | Build error `[KR7113]` |
| `export * from` in a `"use client"` module | Build error `[KR7114]` |
| Client-side (SPA) navigation into a page with islands | Not covered: the island loader runs on full page loads |

See the [diagnostics reference](/docs/reference/diagnostics/) for every code.
