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

When the page component itself is async:

- A dynamic async page with `generateStaticParams` is **prerendered at build
  time** for every param set returned by `generateStaticParams`, including its
  layouts and `generateMetadata`/`metadata` head tags, and marked **SSG** in
  the route manifest. With `output: "static"`, non-dynamic async pages are
  prerendered the same way and no SSR server is emitted.
- Pages that opt out (`export const dynamic = "force-dynamic"`,
  `export const config = { ssr: true }`, `revalidate = 0`, or uncached
  `no-store` fetches) or read request-only APIs (`headers()`, `cookies()`,
  `searchParams`) keep **SSR** per-request rendering (or fail with `[KR7103]`
  when `output: "static"` is set). If a page throws a dynamic-usage bailout
  during prerendering in non-static mode, Krate falls back to request-time SSR
  with a note instead of failing the build.
- Pages with `export const revalidate = 60` (or
  `export const config = { isr: true, revalidate: 60 }`) keep **ISR**
  revalidation at request time, and `generateStaticParams` entries seed their
  initial HTML with the prerendered page instead of an empty shell.
- Other async pages (dynamic routes without `generateStaticParams`, or
  non-dynamic async pages without `output: "static"` / `dynamic = "force-static"`)
  render per request (**SSR**): the static shell contains the layout around an
  empty page region, with no build-time body and no hydration script.
- Async children render at any depth. Siblings resolve in parallel and
  `<Suspense>` renders its resolved children (in prerendered pages, every
  Suspense boundary is resolved in the baked HTML).

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
| `headers()` | Request `Headers`, including `host`; also awaitable. `x-forwarded-host` is there only if the client or proxy sent it |
| `cookies()` | Read-only view: `get(name)` returns `{ name, value }`, plus `getAll()` / `has()`; `set()` throws during a render |
| `notFound()` | Stops rendering and answers 404 (the custom 404 page when present) |
| `redirect(url)` | Stops rendering and redirects with `307` (a server action navigates the client). `permanentRedirect(url)` does the same with `308`, as in Next.js |
| `fetch` | The runtime's `fetch` |

## Errors

An error thrown by a server component (or a failed `fetch`) answers **500**
with the `X-Krate-Error: render` header and the custom `500.html` when the
project has one. A server component never renders as an empty string, and the
synchronous `renderToString` throws if it meets a Promise.

## Client components inside server components

A module that starts with `"use client"` is a client boundary. When a server
component renders one of its exports, the server renders its initial HTML
between two comments, `<!--krate-island:{...}-->` (the module, the export and
the serialized props) and `<!--/krate-island-->`, and a small per-module chunk
hydrates those nodes in place in the browser. The island adds no element of its
own, so the component's root elements are children of the server element
around it, as in React: child combinators and sibling selectors such as
Tailwind's `space-y-*`, `divide-*` and `*:` style them as they would in Next.js.
`window.__krateIslands` lists the islands of the page with their status
(`loading`, `hydrated`, `error`):

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
- Props must be serializable, as in React Server Components: strings, numbers
  (including `NaN`, `Infinity` and `-0`), booleans, `null`, `undefined`,
  `bigint`, `Date`, `Map`, `Set`, typed arrays, `ArrayBuffer`, `DataView`,
  `Symbol.for` symbols, plain objects and arrays. They arrive in the client
  component as the same types, so a database row can be passed as it is.
  Server actions may be passed and stay callable. A function, class instance
  or unregistered symbol fails the build when it is written inline (`[KR7111]`)
  and fails the request with the prop name otherwise.
- `children` passed from a server component are rendered on the server, in
  place, inside the client component's HTML. So is any other prop that holds
  JSX, such as `userMenu={<UserMenu user={user} />}` or an array of elements
  (a slot), and a client component inside a slot becomes its own island. The
  props carry only a reference to each slot: when the client component
  hydrates it adopts the server-rendered nodes where it renders the slot, so
  the children are in the page once, re-renders leave them in place, and a
  slot the component hides and shows again gets the same nodes back (with
  any islands inside them still running). The nodes sit directly inside the
  element the component renders them in, with no wrapper element (comment
  markers bound a slot in the server HTML), so child-combinator CSS such as
  Tailwind's `has-[>svg]:`, `*:` or `space-y-*` matches them as in React. A
  real-React island still puts the HTML parts of its children in a
  `display: contents` element. A slot the server did not render,
  such as the content of a closed dialog, is sent as HTML in the props, and
  so are the server parts of a real-React island's children, since nothing
  renders them on the server. Each island's props are in the document once,
  however deep it is nested. JSX nested inside a plain object cannot be sent.
- The child of a client component with `asChild` (a Radix `Slot`, as in a
  shadcn `<DropdownMenuTrigger asChild><Button>...</Button></DropdownMenuTrigger>`)
  arrives as an element it can clone, as in Next.js, so the trigger's
  handlers, ref and ARIA props land on the button. A server component at the
  top of that child (a directive-less `Button`) is run on the server to the
  element it renders, and client components in its output (an `Avatar`)
  render inside the same island instead of as islands of their own. Server
  components deeper inside it are still sent as HTML. A child that can only
  be sent as HTML (raw HTML) cannot receive props; the server logs a warning
  naming the component.
- Context from a client component reaches the client components inside its
  server children, as in Next.js. A root layout that renders
  `<Providers>{children}</Providers>` (a `"use client"` module with a theme,
  a Radix `TooltipProvider` or a query client) gives every client component
  of the page that context, also across server components in between, and a
  provider whose value is state updates them when it changes. The server
  renders them inside the same context. Each nested component is still its
  own island: one on Preact shares the provider's instances, and a real-React
  island (one that reaches a reconciler library such as `@react-three/fiber`)
  gets its own copy of each context with the provider's current value. Not
  carried: context from a provider that is itself a real-React island, and,
  into real-React islands, contexts created while rendering.
- Only what a client component imports and uses goes to the browser, as in
  Next.js. A server helper (database client, `node:crypto`, a native
  package) that sits in the same barrel as the component, or in a module the
  component never calls, stays out of the client chunks. If client code does
  use Node built-ins or a `.node` addon, the build stops with
  [KR8009](/docs/reference/diagnostics/#kr8009).
- `lazy(() => import("./heavy"))` in a server component renders the module on
  the server, in place; no browser chunk is made for it. `process.env` reads
  in modules only the server tree uses are real, so [KR8310](/docs/reference/diagnostics/#kr8310)
  only fires for modules a client component bundles.
- Pages without server components are unaffected: there, `"use client"`
  changes nothing and components compile through the normal Krate pipeline.

### Islands across client navigations

A client navigation (a `<Link>`, `router.push`/`replace`, `router.refresh()`,
or the refresh after a server action that revalidated) swaps in the server
HTML of the new route, but it does not tear down the client components that
are already mounted, as in Next.js:

- An island that is in the new HTML too stays mounted: its DOM nodes, state
  and focus are kept, and it re-renders in place when its server props
  changed. `useSearchParams`, `usePathname` and `useParams` inside it follow
  the new URL. Server HTML outside islands is replaced.
- Islands are matched by component (module and export), in document order:
  the n-th island of a component in the old page is the n-th one in the new
  page. On a navigation to another pathname an island is kept only when its
  server props are unchanged too (an island of a shared layout), so a page's
  own islands start fresh on another page, as React remounts a page.
- Server children of a kept island follow the new server HTML in place (the
  islands inside them are kept the same way). When their top-level shape
  changed (a provider in a layout around pages with other islands), they are
  rendered anew inside the kept island: it keeps its state, the islands of
  the old children unmount and those of the new ones mount inside it.
- An island the new HTML no longer has (or has with another component in its
  place) is unmounted before its markup is removed, so its effects clean up
  and its portals (dialogs, popovers, toasts) leave the page.
- A focused control in a kept island keeps the focus. When the focused element
  is replaced, focus moves to the matching element once the new islands
  mounted.

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
- A component with state or effects (a checkbox, a dialog) becomes an
  island that hydrates in the browser, like a `"use client"`
  component. Its chunk is built from just the exports the page uses, so a
  barrel such as lucide-react does not pull in every icon.
- Parts of one compound component keep their context: `Dialog.Trigger` and
  `Dialog.Content` written inside `Dialog.Root` render in the same Preact tree
  as the root, so they hydrate and talk to each other. Server components
  placed between the parts are rendered to HTML, so the parts on either side
  of one become separate islands, which still see the root's context.
- Props follow the `"use client"` rules: serializable values (see above) and server
  actions, no inline functions (`[KR7111]`). Put handlers in a `"use client"`
  wrapper, as in Next.js.
- A local module that renders such components needs no directive unless it
  uses hooks or event handlers. Calling a hook of an npm React library
  (zustand, react-hook-form, swr, ...) outside a `"use client"` module is
  `[KR8013]`.
- The global `<Link>` works in server components, async layouts and inside
  `"use client"` modules.
- **A client module's exports are references, not functions you can call.**
  As in Next.js, a server component can render an export of a `"use client"`
  module (`<Shell />`) or pass it to a client component, but calling it
  (`parseTheme(cookie)`) fails: with `[KR7118]` at build time when the call is
  visible in the component, and otherwise when the page renders, with
  `Attempted to call parseTheme() from the server but parseTheme is on the client`.
  Only that request fails with a 500; the renderer keeps serving other pages.
  Move a plain function into a module without `"use client"` and import it from
  both the server and the client side. Exports that are not functions
  (constants, plain objects) are not references in Krate: a server component
  reads `LIMIT` from a client module as the value (Next.js hands it a client
  reference it cannot read).

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
  throws while rendering, the response is `500` with `Cache-Control: no-store`
  (so no CDN keeps it or crawler indexes it) and the error page rendered
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
| Server components that require request-time rendering with `output: "static"` | Build error `[KR7103]` (prerenderable async pages with `generateStaticParams` or without dynamic segments succeed) |
| Module-level `await` in a page rendered at build time | Build error `[KR7102]` |
| `async` components in a `"use client"` module | Build error `[KR7113]` |
| `export * from` in a `"use client"` module | Build error `[KR7114]` |
| Calling a function exported by a `"use client"` module from a server component | Build error `[KR7118]`, or a 500 for that request when the call is not visible to the build |
| Client-side (SPA) navigation into a page with islands | Not covered: the island loader runs on full page loads |

See the [diagnostics reference](/docs/reference/diagnostics/) for every code.
