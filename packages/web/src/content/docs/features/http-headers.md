---
title: HTTP Headers
description: Set response headers and understand Krate's defaults.
order: 12
---

# HTTP Headers

Custom headers come from your [middleware](/docs/features/middleware/);
Krate also emits a small set of automatic headers on the pages and routes it
serves.

## Custom headers via middleware

Return a `Response` (with or without a body) from `middleware.ts` and its
headers are applied to the response:

```typescript
// middleware.ts
export function middleware(request: Request) {
  const url = new URL(request.url);

  // Set headers and continue to the page handler.
  if (url.pathname.startsWith("/docs")) {
    return new Response(null, {
      headers: {
        "X-Robots-Tag": "noindex",
        "Permissions-Policy": "camera=(), microphone=()",
      },
    });
  }
}
```

Middleware headers are applied whether the request continues to the page
handler or short-circuits (redirect/rewrite/custom response). See
[Middleware](/docs/features/middleware/) for the full contract.

## Automatic headers

For SSR, ISR, and streaming pages Krate sets `Content-Type` and
`X-Content-Type-Options: nosniff` automatically:

| Page type | Cache-Control |
|-----------|---------------|
| ISR | `public, s-maxage=<revalidate>, stale-while-revalidate=<2*revalidate>` |
| Other dynamic (SSR) | `no-store` |

ISR pages opt in with `export const config = { isr: true }` and an optional
`revalidate` (seconds). When `revalidate` is omitted it defaults to **60s**.

```typescript
// pages/blog/[slug].tsx
export const config = { isr: true, revalidate: 300 };
```

The `s-maxage` + `stale-while-revalidate` pair lets CDNs and browsers hold the
HTML while the renderer refreshes stale entries in the background.

### ISR cache status

Every ISR response includes an `X-Krate-Cache` header describing how the page
was served:

| Value | Meaning |
|-------|---------|
| `HIT` | Fresh from the cache |
| `STALE` | Served from cache while a background revalidate ran |
| `MISS` | Rendered on demand (first request / after invalidation) |

### Render failures

If a page-region render fails at serve time, the baked shell is served as a
graceful fallback and the response carries `X-Krate-Error: render`.

## Static assets

Static files under `public/` are served with Go's standard `http.FileServer`
headers and content types derived from the file extension. Asset files in the
build output (`_krate/images/...`, hashed bundles under `chunks/`, etc.) are
immutable per build, so they are safe to cache aggressively at the CDN layer.