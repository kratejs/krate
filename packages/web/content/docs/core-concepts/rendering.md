---
title: Rendering
order: 5
---

# Rendering: SSG, SSR, ISR & Streaming

Krate is **SSG-first**: every page is pre-rendered to static HTML at build
time. On top of that base, pages can opt into server-side rendering (SSR),
incremental static regeneration (ISR), or Suspense-based streaming.

## SSG (default)

```tsx
export default function Page() {
  return <h1>Hello, World!</h1>;
}
```

The page is rendered once at build time. The output is a static HTML file plus
a hydration bundle. This is the fastest and most portable mode — it works on
any static host.

## Build-time props (`getStaticProps`)

```tsx
// Sync: evaluated by the compiler from the AST
export function getStaticProps() {
  return { props: { title: "Hello" } };
}

// Async: executed via an npx tsx bootstrap at build time
export async function getStaticProps() {
  const res = await fetch('https://api.example.com/data');
  return { props: { data: await res.json() } };
}
```

`getStaticProps` returns `{ props }`, which is passed to the page component.

## SSR (`getServerSideProps`)

```tsx
export async function getServerSideProps(ctx) {
  const data = await fetch('https://api.example.com/item/' + ctx.params.id);
  return { props: { item: await data.json() } };
}
```

Pages exporting `getServerSideProps` render **per request** in the Node
renderer sidecar. The HTML is served dynamically instead of from a static file.

## ISR (`getStaticProps` + `revalidate`)

```tsx
export async function getStaticProps() {
  return { props: { ... }, revalidate: 60 }; // seconds
}
```

The page is built statically, but the renderer revalidates it after the
specified interval, serving a cached copy in the meantime. The in-memory cache
size is configurable via `ssr.maxCacheSize`.

## Streaming SSR

Streaming uses Suspense-based two-phase rendering for pages that import runtime
components, or when `ssr.streaming` forces all pages into streaming mode:

1. **Phase 1 (fallback)** — renders the page with fallback content.
2. **Phase 2 (resolved)** — streams resolved content via
   `<!--suspense-resolved:N-->` markers with chunked transfer encoding.

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

## How SSR/ISR works

- Pages are built to static HTML where possible.
- The **Node renderer server** handles SSR/ISR/streaming requests and caches
  ISR pages in memory.
- `manifest.json` records each page's mode and metadata.
- Server bundles are compiled for pages that need dynamic rendering.

## Choosing a mode

| Need | Mode |
|------|------|
| Static content, fastest | SSG (default) |
| Data at build time | `getStaticProps` |
| Per-request data | `getServerSideProps` |
| Static + periodic refresh | ISR (`revalidate`) |
| Server-rendered components streamed in | Streaming SSR |
