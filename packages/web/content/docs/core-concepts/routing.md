---
title: Routing & Layouts
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
| `src/pages/docs/[...slug].tsx` | `/docs/a/b/c` (catch-all) |

## Dynamic routes

Files named `[param].tsx` map to dynamic segments. The segment value is passed
to the component as `params`:

```tsx
// src/pages/video/[id].tsx → /video/abc123
export default function VideoPage({ params }) {
  return <h1>Video {params.id}</h1>;
}
```

For a statically-built dynamic route you can provide
`generateStaticParams`, or use `getServerSideProps` to render per-request.

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
layout. Layouts receive their page content through `{children}`.

## Loading states

A `src/loading.tsx` component is shown during SPA page transitions:

```tsx
// src/loading.tsx
export default function Loading() {
  return <div class="loading">Loading…</div>;
}
```

## Custom 404 page

Create `src/pages/404.tsx` to customize the not-found page. Error pages are
rendered with layout wrapping and use a full page replacement.

## The SPA router

Internal links (via `<Link>` or anchors) trigger client-side navigation:

```tsx
import { Link } from '@krate/runtime';

<Link href="/about">About</Link>
```

`initRouter()` powers:

- **Prefetching** — `data-prefetch` on hover/focus and viewport entry.
- **Tree reconciliation** — the router diffs the live content root against the
  parsed new page instead of wiping `innerHTML`. Nodes keyed by `<!--k:-->`
  markers, `data-k` attributes, and `id` anchors are kept so their state
  (focus, scroll, media, CSS animations) survives.
- **Scroll restoration** — forward navigation scrolls to top; back/forward
  restores the saved position. Hash links scroll to the target element.
- **Modified clicks** — ⌘/Ctrl/Shift/Alt-click and middle-click are never
  intercepted.
- **External links** — `http(s)://`, `mailto:`, `tel:`, `#`, `target="_blank"`
  and `download` anchors are left as plain links.

`<Link>` supports `prefetch={false}`, `replace`, `scroll={false}`, and
`target="_blank"` (which auto-adds `rel="noopener noreferrer"`). After
navigation the link matching the current path gets `aria-current="page"`.

## File conventions reference

| File | Purpose |
|------|---------|
| `src/pages/_layout.tsx` | Layout wrapper for directory + children |
| `src/pages/404.tsx` | Custom 404 page |
| `src/loading.tsx` | Loading component during SPA transitions |
| `middleware.ts` | Request middleware (runs before page rendering) |
| `src/api/*.ts` | JS API routes |
| `src/api/*.go` | Go API routes |
