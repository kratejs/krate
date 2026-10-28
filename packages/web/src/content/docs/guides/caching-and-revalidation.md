---
title: Caching and Revalidation
order: 7
description: Cache server data with tags and revalidate windows, pick a page render mode with revalidate/dynamic exports, and invalidate ISR pages on demand with revalidatePath and revalidateTag.
---

# Caching and Revalidation

Krate caches request-time work in three places:

| Cache | Holds | Lives in |
|-------|-------|----------|
| ISR page cache | The rendered body of each ISR page variant (route + params + query) | SSR renderer (one per worker) |
| Region cache | Rendered dynamic regions (Suspense/runtime components, Go regions) | SSR renderer and Go host |
| Data cache | `unstable_cache` results and `fetch` responses requested with cache options | Each server process (renderer, Node API sidecar, Go host for QuickJS routes) |

`@krate/runtime/cache` (also re-exported from `@krate/runtime/server`) is the
API for all three. It mirrors `next/cache`.

```ts
import { revalidatePath, revalidateTag, unstable_cache, unstable_noStore } from "@krate/runtime/cache";
```

## Choosing a render mode

A page picks its mode with the `config` object or with Next.js-style segment
exports:

| Export | Mode |
|--------|------|
| `export const revalidate = 60` | ISR, regenerated every 60 seconds |
| `export const revalidate = 0` | SSR (rendered per request) |
| `export const revalidate = false` | Static (SSG), cached indefinitely |
| `export const dynamic = "force-dynamic"` | SSR (wins over `revalidate`) |
| `export const dynamic = "force-static"` or `"error"` | Static, or ISR when `revalidate = N` is also set |
| `export const dynamic = "auto"` | No effect |
| `export const config = { isr: true, revalidate: 60 }` | ISR (the `config` object wins over the segment exports) |

`revalidate` must be a constant number of seconds (`60 * 60` is folded) or
`false`. Any other value, and any other `dynamic` string, is reported as
warning [`KR8601`](/docs/reference/diagnostics/) and ignored.

When a page states no mode, Krate looks at how the page and the local modules
it imports read data:

- `fetch(url, { cache: "no-store" })`, `{ next: { revalidate: 0 } }` or
  `unstable_noStore()` make the page SSR.
- `{ next: { revalidate: N } }` or `unstable_cache(fn, keys, { revalidate: N })`
  make it ISR with the smallest `N`.
- Tags alone (`{ next: { tags: [...] } }`, `unstable_cache(..., { tags })`) make
  it ISR that stays cached until a tag or path is revalidated.

The build prints the inferred mode (`posts.tsx → isr (tagged data read)`). A
page that is pinned static (`force-static`, `revalidate = false`,
`output: "static"`) but reads tagged, timed or no-store data gets warning
`KR8602`: its HTML is baked once and nothing can refresh it.

## Caching data

### `fetch` options

Inside server code (page renders, API routes, server actions) `fetch` accepts
the Next.js cache options:

```tsx
export default async function Posts() {
  const res = await fetch("https://api.example.com/posts", {
    next: { tags: ["posts"], revalidate: 300 },
  });
  const posts = await res.json();
  return <ul>{posts.map((p) => <li>{p.title}</li>)}</ul>;
}
```

- `next.tags` are recorded on the page's ISR entry, so `revalidateTag("posts")`
  purges the page.
- `next.revalidate: N` caches the response for N seconds; `cache: "force-cache"`
  or `next.revalidate: false` cache it until invalidated.
- `cache: "no-store"` or `next.revalidate: 0` skip the cache and keep the
  render out of the ISR cache.
- Only successful `GET`/`HEAD` responses are cached, keyed by method, URL and
  request headers. Requests without these options are not cached.

### `unstable_cache`

```ts
const getPost = unstable_cache(
  async (id: string) => db.post.findUnique({ where: { id } }),
  ["post"],
  { tags: ["posts"], revalidate: 3600 },
);
```

The key is the function source, `keyParts` and the call arguments. Results must
be JSON-serializable (a hit returns a parsed copy). `revalidate` defaults to
`false` (kept until invalidated); `0` disables caching. Concurrent misses for
the same key share one call. An expired entry is recomputed on the next call
(there is no stale-while-revalidate for data entries).

### `unstable_noStore`

Called during a render, it keeps that render out of the ISR cache. Outside a
render it has no effect.

## On-demand revalidation

```ts
"use server";
import { revalidatePath, revalidateTag } from "@krate/runtime/cache";

export async function publish(id: string) {
  await db.post.update({ where: { id }, data: { published: true } });
  revalidateTag("posts");
  revalidatePath(`/blog/${id}`);
}
```

- `revalidateTag(tag)` purges every ISR entry and region whose render read the
  tag, plus every data-cache entry with the tag, in every process and worker.
- `revalidatePath(path)` purges a page:
  - a concrete URL (`/blog/hello`) purges that params variant (all query
    variants of it) of the matching dynamic route,
  - a route pattern (`/blog/[slug]`) or a static route purges every variant,
  - `revalidatePath("/blog", "layout")` purges every page at or below `/blog`.
  It also drops the data-cache entries those renders stored.

The next request for a purged page renders it fresh (a cache `MISS`).
Time-based ISR keeps its stale-while-revalidate behavior.

### Where you can call them

| Context | Behavior |
|---------|----------|
| Server action | Queued, then applied before the action responds. The JSON response carries `"revalidated": true` and `"revalidation": { "paths": [...], "tags": [...] }`, and the client dispatches a `krate:revalidate` window event with that detail so the router can refresh the current route. |
| Node/Bun API route | Queued, then applied before the response is sent. |
| Embedded (QuickJS) API route | Applied synchronously by the Go host. |
| Go API route | `krate.RevalidatePath(path, "page"\|"layout")` / `krate.RevalidateTag(tag)` from `goext/krate`; returns after the caches were invalidated. |
| Other server code under `krate serve` / `krate dev` (a timer, a queue consumer) | Sent immediately; failures are logged. |
| During a page render | Throws: revalidate after the data changed, from an action or API route. |
| Anywhere else (a build, a script) | Throws: `KRATE_HOST_URL is not set`. |

If the host cannot apply an invalidation (the renderer is unreachable), the
action or API route fails instead of answering with stale data.

### Static pages are skipped, not silently accepted

A static (SSG) page is an HTML file baked at build time; revalidation cannot
rebuild it. `revalidatePath("/about")` on a static page reports it under
`skipped` with the reason and logs a warning once per route. The build warns
early with [`KR8603`](/docs/reference/diagnostics/) when a `revalidatePath` call
with a literal path names a static page. Make such pages ISR
(`export const revalidate = N`) or SSR (`export const dynamic = "force-dynamic"`).

Unknown paths are reported as skipped too.

## How it works

`krate serve` and `krate dev` start a loopback-only internal endpoint,
`POST /__krate/internal/revalidate`, authenticated with a random per-process
secret. Every server runtime (SSR renderer, Node/Bun API and action sidecar,
QuickJS routes, Go API routes) receives `KRATE_HOST_URL` and
`KRATE_INTERNAL_SECRET`. The host resolves paths against the build manifest and
invalidates the renderer (fanned out to every `ssr.rendererWorkers` worker), the
Go region cache, the QuickJS data cache and the Node API sidecar's data cache
(fanned out to every API worker) before it answers:

```json
{ "revalidated": [{ "target": "/blog/hello", "kind": "path", "routes": ["/blog/[slug]"] }],
  "skipped": [{ "target": "/about", "reason": "route /about is static (SSG): ..." }] }
```

The renderer's mutating endpoints (`/__krate/ssr/revalidate`, `/refresh`,
`/invalidate`, `/cache-invalidate`) require the same secret; the public server
only forwards `/__krate/ssr/health` without it.

## Limits

- Caches are in memory per server process. Several `krate serve` instances
  behind a load balancer do not share them, and a CDN that holds an ISR
  response (`s-maxage`) keeps serving it until its own TTL expires.
- Go components cannot read tagged data, so `revalidateTag` does not target Go
  region renders; `revalidatePath` does.
- Region renders run synchronously today, so tags are only recorded for data
  read during the page render itself.
- `fetch` cache options in Node API routes and actions need the staged data
  cache runtime (`.krate/cache-runtime.mjs`, written by `krate serve`/`dev`); if
  `@krate/runtime` cannot be found the server prints a warning.
