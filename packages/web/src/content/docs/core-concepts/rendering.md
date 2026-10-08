---
title: Rendering
order: 5
description: Choose between static (SSG), incremental (ISR), per-request (SSR), and streamed rendering.
---

# Rendering Modes

Krate is **SSG-first**: every page is pre-rendered to static HTML at build
time. On top of that base, pages can opt into ISR, SSR, or Streaming via a small
per-page config or by using the components that imply them.

The architecture is **static-first**: the Go compiler bakes as much as possible
into a static shell (layout, `<head>`, server components, resolved suspense
content). Only genuinely dynamic pieces — the "regions" of a page — are
rendered at request time by a small sidecar and spliced into the shell. Krate
never re-renders an entire page just to update one dynamic part, and it never
renders a page twice.

## Modes at a glance

| Mode | Config | Output |
|------|--------|--------|
| **SSG** (default) | — | Fully static HTML, no request-time work |
| **ISR** | `export const config = { isr: true, revalidate: 60 }` | Static shell; page body cached per URL variant and revalidated in the background |
| **SSR** | `export const config = { ssr: true }` | Static shell; page body rendered fresh on every request |
| **Streaming** | `export const config = { streaming: true }`, `<Suspense>`, or runtime components | Static shell; each dynamic region streamed in as it resolves |

Precedence when several apply: `isr` > `ssr` > `streaming` > `ssg`.

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

## ISR (Incremental Static Regeneration)

Opt a page in with the `isr` flag. `revalidate` (seconds) controls how often the
page body is regenerated in the background:

```tsx
export const config = { isr: true, revalidate: 60 };

export default function PricesPage() {
  return <p>Latest prices</p>;
}
```

ISR responses are cacheable: Krate emits
`Cache-Control: public, s-maxage=…, stale-while-revalidate=…` so CDNs hold
fresh HTML while the sidecar revalidates stale entries in the background.
`revalidate` defaults to 60 seconds when omitted.

For **dynamic routes** with `generateStaticParams`
([Data Fetching](/docs/features/data-fetching/)), known variants are baked as
static files at build time. Unknown variants render on demand through the page
region and are cached per variant (URL params and query string are part of the
cache key).

## SSR

```tsx
export const config = { ssr: true };
```

The page shell is baked, and the page body is rendered by the sidecar on every
request with the live URL params/query. Responses are `no-store`.

## Streaming

Streaming pages use Suspense boundaries and runtime components. The shell is
baked with each dynamic boundary's **fallback** in place; as each region
resolves it is streamed to the client and spliced in — one render per region,
no two-phase full-page render.

```tsx
// @runtime
export default function PriceTag({ price }) {
  return <span>{price}</span>;
}
```

```tsx
export const config = { streaming: true };
```

Pages that render a runtime component or use `<Suspense>` are automatically
treated as streaming even without the config:

```tsx
export default function LivePage() {
  return (
    <Suspense fallback={<p>Loading…</p>}>
      <LiveStatus />
    </Suspense>
  );
}
```

## Choosing an approach

| Need | Approach |
|------|----------|
| Static content, fastest | SSG (default) |
| Data at build time | Server component (`@server`) |
| Per-request data | Runtime component (`@runtime`) + Streaming |
| Content that updates on a schedule | ISR (`isr` + `revalidate`) |
| Fully per-request page | SSR (`ssr`) |
| Static + dynamic route URLs | `generateStaticParams` |

See [Component Tiers](/docs/core-concepts/component-tiers/) for how server and
runtime components work, and [Data Fetching](/docs/features/data-fetching/) for
the full data model. For how the static shell and sidecar are wired, see
[Contributing → Rendering internals](/docs/contributing/rendering/).
