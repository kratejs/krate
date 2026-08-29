---
title: Rendering
order: 5
---

# Rendering: SSG & Streaming

Krate is **SSG-first**: every page is pre-rendered to static HTML at build time.
On top of that base, pages can opt into Streaming SSR — including per-request
data via runtime components.

## SSG (default)

```tsx
export default function Page() {
  return <h1>Hello, World!</h1>;
}
```

The page is rendered once at build time. The output is a static HTML file plus
a hydration bundle. This is the fastest and most portable mode — it works on
any static host.

Server components (`// @server`) are evaluated at build time and their output is
baked into the static HTML, so build-time data needs no special page-level
function. See [Data Fetching](/docs/features/data-fetching/).

## Streaming SSR

Streaming uses Suspense-based two-phase rendering for pages that import runtime
components, or when `ssr.streaming` forces all pages into streaming mode:

1. **Phase 1 (fallback)** — renders the page with fallback content.
2. **Phase 2 (resolved)** — streams resolved content via
   `<!--suspense-resolved:N-->` markers with chunked transfer encoding.

This is the recommended path for per-request data, which lives in runtime
components (`// @runtime`):

```tsx
// @runtime
export default function PriceTag({ price }) {
  return <span>{price}</span>;
}
```

```tsx
// Force ALL pages to streaming SSR
export default defineConfig({
  ssr: { streaming: true },
});
```

Or opt in per page:

```tsx
export const config = { streaming: true };
```

## How streaming works

- Pages are built to static HTML where possible.
- Pages with runtime components are compiled into server bundles and rendered
  at request time by the Node renderer server.
- The renderer resolves runtime component props and streams resolved content
  through Suspense boundaries.
- `manifest.json` records each page's mode and metadata.

## Choosing an approach

| Need | Approach |
|------|----------|
| Static content, fastest | SSG (default) |
| Data at build time | Server component (`@server`) |
| Per-request data | Runtime component (`@runtime`) + Streaming SSR |
| Static + dynamic route URLs | `generateStaticParams` |

See [Component Tiers](/docs/core-concepts/component-tiers/) for how server and
runtime components work, and [Data Fetching](/docs/features/data-fetching/) for
the full data model.
