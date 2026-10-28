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
// src/pages/video/[id].tsx → /video/abc123
export default function VideoPage({ params }) {
  return <h1>Video {params.id}</h1>;
}
```

For a statically-built dynamic route, provide `generateStaticParams` to expand
the route into concrete pages. Each param value is passed to the component as
`params` (so `params.id` matches the `[id]` segment).

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
// src/pages/_layout.tsx — wraps every page in the directory
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
logs why) when the page hydrates through the class-component/error-boundary
owner tree, or when the layout renders a `lazy()` component, a `<Suspense>`
boundary, or an external island.

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
  return <div class="loading">Loading…</div>;
}
```

## Custom 404 page

Create `src/pages/404.tsx` to customize the not-found page. Error pages are
rendered with layout wrapping; during client navigation the router shows them
in place without reloading the app (see the
[SPA router reference](/docs/reference/runtime-api/#spa-router)).

## The SPA router

Internal links (via `<Link>` or anchors) trigger client-side navigation:

```tsx
import { Link } from '@krate/runtime';

<Link href="/about">About</Link>
```

SPA navigation is enabled by default. Set `spa: false` in the config to omit
the router from the shared runtime and use normal full-page navigation; links
and `navigate()` keep working.

The router handles:

- **Prefetching** — `data-prefetch` on hover/focus and viewport entry, cached
  for 30 seconds and used once.
- **Layout persistence** — shared layouts stay mounted (see above).
- **Scroll restoration** — forward navigation scrolls to top; back/forward
  restores the saved position of that history entry. Hash links scroll to the
  target element without a request.
- **Modified clicks** — ⌘/Ctrl/Shift/Alt-click and middle-click are never
  intercepted.
- **External links** — `http(s)://`, `mailto:`, `tel:`, `#`, `target="_blank"`
  and `download` anchors are left as plain links.
- **GET forms** — submitted client-side (opt out with `data-krate-reload`).

`<Link>` supports `prefetch={false}`, `replace`, `scroll={false}`, and
`target="_blank"` (which auto-adds `rel="noopener noreferrer"`). After
navigation the link matching the current path gets `aria-current="page"`.
Programmatic navigation, events and error handling are documented in the
[runtime API reference](/docs/reference/runtime-api/#spa-router).

## File conventions reference

| File | Purpose |
|------|---------|
| `src/pages/_layout.tsx` | Layout wrapper for directory + children (kept mounted across navigations) |
| `src/pages/_template.tsx` | Like a layout, but remounted on every navigation |
| `src/pages/(group)/` | Route group: organizes files, no URL segment |
| `src/pages/@slot/` | Parallel-route slot, rendered by the sibling layout as the `slot` prop |
| `src/pages/@slot/default.tsx` | Slot content when no slot page matches |
| `src/pages/@slot/(.)x/` | Intercepting route (soft navigation renders it into the slot) |
| `src/pages/404.tsx` | Custom 404 page |
| `src/pages/loading.tsx` | Loading component during SPA transitions |
| `middleware.ts` | Request middleware (runs before page rendering) |
| `src/api/*.ts` | JS API routes |
| `src/api/*.go` | Go API routes |
