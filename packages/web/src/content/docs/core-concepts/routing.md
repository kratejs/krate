---
title: Routing & Layouts
description: File-based routing, dynamic segments, and nested layouts.
order: 3
---

# Routing & Layouts

Krate uses **file-based routing**: every file in `src/pages/` becomes a route.

| Pattern | Route |
|---------|-------|
| `src/pages/index.tsx` | `/` |
| `src/pages/about.tsx` | `/about` |
| `src/pages/blog/index.tsx` | `/blog` |
| `src/pages/blog/post.tsx` | `/blog/post` |
| `src/pages/video/[id].tsx` | `/video/abc123` |
| `src/pages/docs/[...slug].tsx` | `/docs/a/b/c` (catch-all, one or more segments) |
| `src/pages/shop/[[...filter]].tsx` | `/shop`, `/shop/red/xl` (optional catch-all) |
| `src/pages/(marketing)/pricing.tsx` | `/pricing` (route group) |

When several patterns match a URL the most specific wins, segment by segment:
a static segment beats `[param]`, which beats `[...catchAll]`, which beats
`[[...optional]]` (`/docs/intro.tsx` serves `/docs/intro` even with
`/docs/[...slug].tsx` present). The order is the same in `krate dev`,
`krate serve` and SSR.

## Dynamic routes

Files named `[param].tsx` map to dynamic segments. The segment value is passed
to the component as `params`:

```tsx
// src/pages/video/[id].tsx -> /video/abc123
export function generateStaticParams() {
  return [{ id: "abc123" }];
}

export default function VideoPage({ params }) {
  return <h1>Video {params.id}</h1>;
}
```

A statically-built dynamic route provides `generateStaticParams` (as above) to
expand the route into concrete pages. Each param value is passed to the
component as `params` (so `params.id` matches the `[id]` segment).

A catch-all param is the matched segments joined with `/`
(`/docs/a/b` gives `params.slug === "a/b"`); an empty optional catch-all is
`""`.

## Route groups

A directory named `(name)` organizes files without adding a URL segment:
`src/pages/(marketing)/about.tsx` serves `/about`. A `_layout.tsx` inside the
group wraps only the pages in that group, so groups give different sections
different shells. Two pages that resolve to the same URL from different groups
are a build error.

## Layouts

A `_layout.tsx` file wraps every page in its directory and subdirectories:

```tsx
// src/pages/_layout.tsx - wraps every page in the directory
export default function Layout({ children }) {
  return (
    <div>
      <nav><a href="/">Home</a></nav>
      <main>{children}</main>
    </div>
  );
}
```

Layouts nest: a `src/pages/blog/_layout.tsx` wraps blog pages inside the root
layout. Layouts receive their page content through `{children}`, which must be
rendered exactly once. A layout that fails to build (or has no default export)
fails the build; one that never renders `{children}` is reported as a warning.

### Layout persistence

On a client-side navigation the router compares the layouts of the current
and the next page and keeps every shared layout mounted: only the content of
the deepest layout that differs is replaced. State, effects, focus, scroll
containers and open menus in a kept layout survive the navigation, and its
hydration code is not run again.

A layout under a dynamic segment (`blog/[slug]/_layout.tsx`) is kept only while
that segment's value is unchanged. A layout is remounted (and `krate dev`
logs why) when it renders a `lazy()` component, a `<Suspense>` boundary, or an
external island. Class components and error boundaries on the page do not stop
layouts from persisting.

### Templates

`_template.tsx` (or `template.tsx`) follows the layout convention but remounts
on **every** navigation that passes through it, including between sibling
pages and between params of the same dynamic route: its DOM is replaced and its
state and effects start fresh. In one directory the order is
layout > template > page:

```tsx
// src/pages/blog/_template.tsx
export default function Template({ children }) {
  return <div class="fade-in">{children}</div>;
}
```

## Parallel routes

A directory named `@name` next to a `_layout.tsx` is a **slot**. The layout
receives each slot as a prop of the same name and renders it like
`{children}`:

```tsx
// src/pages/_layout.tsx
export default function Layout({ children, modal }) {
  return (
    <>
      <main>{children}</main>
      {modal}
    </>
  );
}
```

For a page under the layout, a slot renders the slot page with the same path
(`@modal/settings.tsx` for `/settings`), otherwise `@modal/default.tsx`. A
slot without a match and without `default.tsx` is a build error. On a client
navigation a slot keeps its current content when the new URL has no matching
slot page (it only resets on a full load), so add a catch-all slot page if a
navigation should clear it. Slot pages with dynamic segments are only
supported as intercepting routes (below).

## Intercepting routes

Inside a slot, a directory prefixed with `(.)`, `(..)`, `(..)(..)` or `(...)`
intercepts a URL: `(.)` matches the slot's own level, `(..)` one segment up,
`(...)` the app root. A photo gallery:

```
src/pages/
  _layout.tsx              renders {children} and {modal}
  index.tsx                the feed, links to /photo/1
  photo/[id].tsx           the full photo page
  @modal/default.tsx       renders nothing
  @modal/(.)photo/[id].tsx the photo as a modal
```

Clicking `/photo/1` from the feed renders `@modal/(.)photo/[id].tsx` into the
`modal` slot while the feed stays on screen and the address bar shows
`/photo/1`. Back closes the modal (the slot's previous content is restored
without a request) and forward reopens it. A full load or reload of
`/photo/1` renders the real `photo/[id].tsx` page. Interception only applies
while the layout that owns the slot is mounted.

Intercepting pages are built as fragments under `/_krate/i/<slot>/...`; the
router fetches them during the soft navigation. Dynamic fragments need
`krate serve` (or `krate dev`) like any dynamic route. Intercepting directories
outside a slot are a build error.

## Loading states

A `loading.tsx` next to (or above) a page is overlaid during SPA transitions
while the next page is fetched. It is not a route of its own:

```tsx
// src/pages/loading.tsx
export default function Loading() {
  return <div class="loading">Loading...</div>;
}
```

## Not-found and error pages

Any directory under `src/pages` can hold its own not-found and error page. They
follow the same rules as `_layout`, `_template` and `loading`: a file applies to
its directory and everything below it.

| File | Role |
|------|------|
| `404.tsx` or `not-found.tsx` | The segment's not-found page (status 404) |
| `error.tsx` or `500.tsx` | The segment's error page (status 500): a component that receives `{ error, reset }` |
| `forbidden.tsx` or `403.tsx` | The page for `forbidden()` (status 403) |
| `unauthorized.tsx` or `401.tsx` | The page for `unauthorized()` (status 401) |

None of them is a route: `blog/404.tsx` does not serve `/blog/404`. Two files
for one role in a directory (`404.tsx` and `not-found.tsx`, `error.tsx` and
`500.tsx`, `forbidden.tsx` and `403.tsx`) are a build error (`KR9001`), and so is a segment page inside a
`@slot` or an intercepting `(.)x` directory (`KR9002`). The root `404.tsx` and
`500.tsx` keep working as the global fallback and are still written to
`dist/404.html` and `dist/500.html`.

```tsx
// src/pages/blog/not-found.tsx
export default function BlogNotFound() {
  return <p>That post does not exist. <Link href="/blog/">All posts</Link></p>;
}
```

```tsx
// src/pages/blog/error.tsx
export default function BlogError({ error, reset }) {
  return (
    <div>
      <p>Something went wrong ({error.digest})</p>
      <button onClick={reset}>Try again</button>
    </div>
  );
}
```

### Which page answers

The page nearest to the failure is used, walking up from the requested URL (or
from the route's source directory), and it renders inside that segment's
layouts, so shared navigation stays in place:

- **Not found** (status 404): a URL that matches no route; a dynamic route with
  `dynamicParams = false` and an unknown param; `notFound()` thrown by a page or
  a server component (the page nearest to the route that threw).
- **Error** (status 500): an uncaught error while an SSR, ISR or async page is
  rendered. The response keeps `X-Krate-Error: render` and is never cached.
- **Forbidden** (status 403) and **unauthorized** (status 401): `forbidden()` or
  `unauthorized()` called while a page renders, in an async server component, in a
  server action or in an API route. The nearest `forbidden.tsx` / `unauthorized.tsx`
  answers, inside its layouts, like a not-found page. See
  [Forbidden and unauthorized](#forbidden-and-unauthorized).

Route groups scope segment pages like they scope layouts. `(shop)/cart/404.tsx`
answers `notFound()` and unknown params of routes inside `(shop)`, but not a URL
that matches no route at all (no route says which group the URL belongs to);
that URL gets the nearest page outside any group. Two groups cannot both define
the page of the same URL directory.

Segment pages are prerendered once (`blog/404.html`, `blog/500.html`) and picked
per request from the `segments` map in the build manifest; request-time
rendering modes on them are ignored (`KR9003`). They are `noindex` by default
(a `robots` meta you set yourself wins), carry no canonical link, and are left
out of the sitemap, the route list and `krate types`. `krate inspect` and the
MCP `explain` tool list each route's nearest not-found and error page.

The Krate server (`krate start`, `krate dev`) picks the nearest page per
request. A static host that only knows `404.html` serves the root page for every
missing URL; the per-directory files are in the output for hosts and handlers
that look them up with the manifest's `segments` map. The serverless handler
(`krate deploy --target node|vercel|netlify|cloudflare`) does the same lookup, so
segment pages work there too (see the deployment guide).

### Forbidden and unauthorized

`forbidden()` and `unauthorized()` stop a render the way `notFound()` does and
answer with 403 or 401. They come from `@krate/runtime/server` (from
`@krate/runtime` in a client file) and are what `next/navigation` exports under
the same names.

```tsx
// src/pages/admin/index.tsx
import { cookies, forbidden, unauthorized } from "@krate/runtime/server";

export default async function Admin() {
  const session = (await cookies()).get("session")?.value;
  if (!session) unauthorized();          // 401, nearest unauthorized.tsx
  if (session !== "admin") forbidden();  // 403, nearest forbidden.tsx
  return <h1>Admin</h1>;
}
```

```tsx
// src/pages/forbidden.tsx (or admin/forbidden.tsx for /admin only)
export default function Forbidden() {
  return <p>You are not allowed to see this. <Link href="/">Home</Link></p>;
}
```

| Where it is called | Result |
|--------------------|--------|
| An SSR, ISR or streaming page, or an async server component it renders | The nearest `forbidden.tsx` / `unauthorized.tsx` with that status; without one, a plain-text `403 Forbidden` / `401 Unauthorized` |
| A server action | Fetch actions get JSON (`{ "ok": false, "error": { "message": "Forbidden" } }`) with status 403 / 401. A no-JS form post gets the nearest page for the page the form was posted from |
| An API route | JSON `{ "error": "Forbidden" }` or `{ "error": "Unauthorized" }` with that status; `notFound()` answers `{ "error": "Not Found" }` with 404 |

The response is never cached: ISR does not store it, it carries
`Cache-Control: no-store`, and a CDN never keeps it. A client navigation
(`<Link>`, `router.push`) to a URL that answers 403 or 401 swaps the page in
place like a 404 does: layout state is kept and back and forward work.

Everything above holds in the Go server (`krate start`, `krate dev`) and in the
portable handler (`krate deploy --target node|vercel|netlify|cloudflare`).

Two limits follow from how Krate renders:

- **Static pages and layouts never run per request.** A page that is built
  statically, and every layout (layouts are baked into the shell), cannot call
  `forbidden()` usefully: the call never runs and the page is served to
  everyone. The build warns (`KR8604`). Make the page request-time
  (`export const dynamic = "force-dynamic"`), or call the function from an async
  server component that the page renders. Where a whole directory must be gated,
  use [middleware](/docs/features/middleware/).
- **Streaming has already sent the status.** In an out-of-order streamed page the
  shell is on the wire before an async child runs, so a `forbidden()` raised by
  such a child cannot change the status. Gate the page itself, not a streamed
  fragment.

### The error prop

On the server, `error` is `{ name, message, digest }`. In production the
message is always `"Internal Server Error"`: the real error is written to the
server log together with the same `digest`, so a visitor can quote the digest
and you can find the log line. `krate dev` shows the real message and `stack`.
The `message` and `digest` are filled into the HTML the server sends; text you
derive from them while rendering (`error.message.toLowerCase()`) appears once
the page hydrates.

### Error boundary around the segment

An `error.tsx` is also a client error boundary around the segment's pages, in
Next.js order: the directory's layout, then the error boundary, then its
template and the page. A render error, an error in an effect, or a reactive
update that throws below it shows the error page in place, with the layouts
still mounted. Event handlers are not caught (the same rule as React's
boundaries).

- `reset()` mounts the segment's children again with fresh state.
- Soft navigation clears the error page. A boundary that has not caught anything
  stays mounted while the user moves between the pages it covers, so layouts
  below it keep their state; one that caught an error is replaced on the next
  navigation.
- The nearest boundary catches first. If an error page throws itself, the error
  goes to the next boundary up.
- Pages without an `error.tsx` above them ship no boundary code.
- `500.tsx` is the server error page only; it does not add a client boundary.

During soft navigation the router shows a 404 or 500 page the server returns in
place, keeps the layouts the two routes share (the boundary of an `error.tsx` is
swapped with the page), and leaves the requested URL in the address bar.

## The SPA router

Internal links written as `<Link>` (or an anchor carrying `data-krate-link`)
trigger client-side navigation; a plain `<a href>` is an ordinary full-page
load:

```tsx
<Link href="/about">About</Link>
```

`Link` is built in, so it needs no import. Under a
[base path](/docs/reference/config/#base-path), `<Link>`, `navigate()` and
`krateRouter` add the prefix, so write hrefs from the site root. A path read from
`usePathname()` already has the prefix removed; `location.pathname` does not, so
do not pass it to them.

SPA navigation is enabled by default. Set `spa: false` in the config to omit
the router from the shared runtime and use normal full-page navigation; links
and `navigate()` keep working. The router is part of the shared runtime, which
a page loads only when it has something to hydrate: a page with no state or
handlers ships no JavaScript, so a link on it is a full-page load (the next page
then starts the router if it needs one).

The router handles:

- **Prefetching** - `data-prefetch` on hover/focus and viewport entry, cached
  for 30 seconds and used once.
- **Layout persistence** - shared layouts stay mounted (see above).
- **Scroll restoration** - forward navigation scrolls to top; back/forward
  restores the saved position of that history entry. Hash links scroll to the
  target element without a request.
- **Modified clicks** - Cmd/Ctrl/Shift/Alt-click and middle-click are never
  intercepted.
- **External links** - `http(s)://`, `mailto:`, `tel:`, `#`, `target="_blank"`
  and `download` anchors are left as plain links.
- **GET forms** - submitted client-side (opt out with `data-krate-reload`).

`<Link>` supports `prefetch={false}`, `replace`, `scroll={false}`, and
`target="_blank"` (which auto-adds `rel="noopener noreferrer"`). After
navigation the link matching the current path gets `aria-current="page"`.
Programmatic navigation, events and error handling are documented in the
[runtime API reference](/docs/reference/runtime-api/#spa-router).

## Prerendered endpoints

A `.ts` or `.js` file under `src/pages` whose name ends in a data extension
(`json`, `xml`, `txt`, `csv`, `rss`, `atom`, `webmanifest`, `ics`, `yml`, `yaml`)
is not a page. Its `GET` export runs once at build time, on Node (or the
configured runtime), and the response body is written to the output as a file:

```ts
// src/pages/data.json.ts  ->  dist/data.json
export async function GET() {
  return Response.json({ items: [1, 2, 3] });
}
```

`src/pages/blog/feed.xml.ts` becomes `dist/blog/feed.xml`. The build fails
(`KR7121`) when there is no `GET`, it throws, or it answers a non-2xx status.
The path cannot have a dynamic segment; for a response that depends on the
request, use an [API route](/docs/features/api-routes/). A `.ts` or `.js` file
under `src/pages/api` is not an API route and warns with `KR7120`: API routes
live in `src/api`.

## File conventions reference

| File | Purpose |
|------|---------|
| `src/pages/_layout.tsx` | Layout wrapper for directory + children (kept mounted across navigations) |
| `src/pages/_template.tsx` | Like a layout, but remounted on every navigation |
| `src/pages/(group)/` | Route group: organizes files, no URL segment |
| `src/pages/@slot/` | Parallel-route slot, rendered by the sibling layout as the `slot` prop |
| `src/pages/@slot/default.tsx` | Slot content when no slot page matches |
| `src/pages/@slot/(.)x/` | Intercepting route (soft navigation renders it into the slot) |
| `src/pages/404.tsx`, `not-found.tsx` | Not-found page of the directory (the root one is the global fallback) |
| `src/pages/error.tsx`, `500.tsx` | Error page of the directory; `error.tsx` is also a client error boundary |
| `src/pages/forbidden.tsx`, `403.tsx` | Page for `forbidden()` in the directory (status 403) |
| `src/pages/unauthorized.tsx`, `401.tsx` | Page for `unauthorized()` in the directory (status 401) |
| `src/pages/loading.tsx` | Loading component during SPA transitions |
| `middleware.ts` | Request middleware (runs before page rendering) |
| `src/pages/name.json.ts` | Prerendered endpoint: `GET` runs at build time and writes `name.json` |
| `src/api/*.ts` | JS API routes |
| `src/api/*.go` | Go API routes |
