---
title: Caching and Revalidation
order: 7
description: Cache server data with tags and revalidate windows, choose a render mode, and invalidate ISR pages on demand with revalidatePath and revalidateTag.
---

# Caching and Revalidation

Krate caches request-time work in three places:

| Cache | Holds | Lives in |
|-------|-------|----------|
| ISR page cache | The rendered body of each ISR page variant (route + params + query) | SSR renderer (one per worker), or a [shared cache](#sharing-caches-between-instances) |
| Region cache | Rendered dynamic regions (Suspense/runtime components, Go regions) | SSR renderer and Go host |
| Data cache | `unstable_cache` results and `fetch` responses requested with cache options | Each server process (renderer, Node API sidecar, Go host for QuickJS routes), or a shared cache |

`@krate/runtime/cache` (also re-exported from `@krate/runtime/server`) is the
API for all three. It mirrors `next/cache`.

```ts
import { revalidatePath, revalidateTag, unstable_cache, unstable_noStore } from "@krate/runtime/cache";
```

## What keys a cached entry

An ISR page is cached per route + params, plus only the query keys its render read
(`searchParams`, `useSearchParams`-style accessors): `/blog?utm_source=x` and a
random `?zz=1` share the entry of `/blog` unless the page looks at them, and a
page that enumerates the query is keyed by all of it. Regions work the same way,
for async server components and for runtime component bundles alike: the render
sees `props.query` through a view that records the keys it touches. Until a render
has shown what a page or region reads, a request that carries a query renders
instead of being answered from the cache (a restart, or an evicted record, costs
one render), so an entry is never matched to a query it was not made for.

The caches are bounded (`ssr.maxCacheSize`, 512 by default) with a segmented LRU:
a new entry starts in a probation segment and is promoted by its first hit, so a
client requesting thousands of one-off variants (any slug renders on an open
`[param]` route) only churns probation and cannot flush the pages and regions that
were requested twice. Unknown params still render; nothing 404s because of the
cache.

A Go region (`*.runtime.go` with `// krate:revalidate`) is keyed on what the
component **read** of its `kr.Request`: `r.Header(name)`, `r.Cookie(name)`,
`r.QueryValue(name)` and `r.Param(name)` record the names they were asked for (like
the `Vary` header of a response), and the entry is keyed on the values of those
names only. A cookie or header the component never reads cannot split the cache,
and one it reads always does, so two visitors with different cookies never share a
render. A component that reads the `Headers`, `Query`, `Params`, `Method` or `URL`
fields, or passes the `Request` to another function, cannot be recorded: its
renders are keyed on the whole request (every header included, so only an
identical request hits). A component without a `kr.Request` parameter shares one
entry.

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

Only code that runs on the server counts. A `fetch` in a `"use client"` module
(or a module only client components import), in a `useEffect`/`onMount`
callback, or in an `onClick`-style event handler runs in the browser and never
changes how the page renders.

The build prints the inferred mode (`posts.tsx -> isr (tagged data read)`). A
page that is pinned static (`force-static`, `revalidate = false`,
`output: "static"`) but reads tagged, timed or no-store data gets warning
`KR8602`: its HTML is baked once and nothing can refresh it.

### The `Cache-Control` header of a page

A page rendered per request answers with `Cache-Control: no-store` unless
something set the header first. A `Cache-Control` that middleware sets
(`NextResponse.next({ headers })` or `response.headers.set(...)`) or that a
`headers` rule in `krate.config` matches is kept, so an app can let a CDN cache
a request-time page:

```ts
export function middleware() {
  const res = NextResponse.next();
  res.headers.set('Cache-Control', 'public, s-maxage=60');
  return res;
}
```

An ISR page gets `public, s-maxage=<revalidate>` by default and the same rule
applies. Krate keeps `no-store` where caching would be wrong: error, 404 and
403 pages, a 503 while the renderer is down, an ISR render that read request
state such as cookies or headers (`private, no-store`), and server action
responses. The rule is the same in `krate serve` and on the Cloudflare, Vercel,
Netlify and node targets.

On Cloudflare, `deploy.cloudflare.cache: true` stores such responses in
`caches.default`, and a `fetch` wrapper in `src/cloudflare.ts` can do your own
caching. See [Edge cache for SSR pages](/docs/guides/deployment/#edge-cache-for-ssr-pages).

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

## Sharing caches between instances

By default each `krate start` process (and each serverless instance) keeps its
own caches. Several instances behind a load balancer then render every page once
each, and `revalidateTag` on one leaves the others serving the old page. A shared
cache fixes both:

```ts
// krate.config.ts
export default {
  cache: { adapter: "redis", url: process.env.REDIS_URL },
};
```

| Cache | With a shared adapter |
|-------|-----------------------|
| ISR page cache | Entries live in the store. An instance serves a page another one rendered; stale-while-revalidate and the tags a render read work across instances. |
| Data cache (`fetch` with `next` options, `unstable_cache`) | Entries live in the store, tagged. Concurrent identical misses in one process still share one upstream call. |
| Region caches (Suspense/runtime component and Go regions) and the embedded QuickJS data cache | Stay in each instance's memory (they hold request-keyed fragments), but a revalidation clears them on every instance. |
| `revalidatePath` / `revalidateTag` | The originating instance purges the store, then publishes the revalidation on the store's channel; every other instance clears what it keeps in memory. A missed message (a dropped connection) makes the instance clear everything. |

Redis is built in (a small RESP client with no dependency; `rediss://` for TLS,
`AUTH` and a database number from the URL). `krate start` joins the invalidation
channel itself, so the Go host's caches are cleared too.

**A broken cache never breaks a page.** Every call is bounded by `timeoutMs`
(250 ms), a failure is logged once, and after three in a row the store is skipped
for five seconds. Meanwhile pages render and are cached in the instance's own
memory; when the store answers again, sharing resumes without a restart. A
revalidation while the store is down purges the instance's own cache and the
response stays 200, but other instances keep the shared copy until the store is
back and its entries expire or are revalidated again.

**Per-user responses are never shared.** Only renders that are cacheable are
stored, the same rule as the in-process cache: a render that read cookies or
headers, called `unstable_noStore()` or fetched with `cache: "no-store"` is not
written, and neither is a page answering 4xx or 5xx.

### Writing an adapter

An adapter is a small key-value store with tag deletion and pub/sub:

```ts
import type { CacheAdapter } from "@krate/runtime/cache";

export default function createAdapter(options: { url?: string; prefix?: string }): CacheAdapter {
  return {
    name: "my-store",
    async get(key) { /* the string stored under key, or null */ },
    async set(key, value, { ttlSeconds, tags } = {}) { /* store; remember the tags */ },
    async delete(key) { /* remove one key */ },
    async keysByTag(tag) { /* keys stored with tag */ },
    async deleteByTags(tags) { /* remove every key stored with one of tags; return how many */ },
    async publish(channel, message) { /* deliver to every subscriber, this process included */ },
    async subscribe(channel, handler) { /* call handler(message); handler("*") after missed messages; return an unsubscribe function */ },
    async close() {},
  };
}
```

Point `cache.adapter` at the module (`"./cache-adapter.ts"` or a package name) and
Krate imports its default export (a factory called with `{ url, prefix, timeoutMs,
...options }`, or an adapter object). Methods may reject: Krate bounds and
tolerates them. The Go host joins the invalidation channel for `redis` only; with
a custom adapter the shared ISR and data caches and revalidation work, but the
Go-host regions and QuickJS data cache of other instances are not cleared.

### Serverless targets

`krate deploy` writes the cache settings (without the URL) into the bundle. The
`node`, `vercel` and `netlify` targets read the URL from `KRATE_CACHE_URL`, or
from the variable `cache.urlEnv` names, at run time; the Redis adapter needs
outbound TCP. A custom `cache.adapter` module is imported into the bundle at
deploy time, so an HTTP-based store (Upstash REST, Vercel KV, Cloudflare KV)
works wherever `fetch` does. On Cloudflare Workers use such an adapter: the
deploy warns when `cache.adapter` is `"redis"` there.

Without `krate deploy`, pass the adapter to the handler yourself:

```ts
import { createKrateHandler } from "@krate/runtime/server-handler";
import { createRedisAdapter } from "@krate/runtime/cache-redis";

const handler = createKrateHandler({ target: "node", data, modules, cache: createRedisAdapter({ url: process.env.REDIS_URL! }) });
```

With a shared adapter configured, `revalidatePath` and `revalidateTag` no longer
report instances as `skipped`, and `krate deploy` stops warning about them. In
the serverless handler, instances that live long enough (the node target) also
subscribe to the invalidation channel to clear their region caches; short-lived
functions simply read the store on every request.

## Limits

- Without a shared cache the caches are in memory per server process. Several
  `krate serve` instances behind a load balancer do not share them, and a CDN that
  holds an ISR response (`s-maxage`) keeps serving it until its own TTL expires.
  See [Sharing caches between instances](#sharing-caches-between-instances).
- Go components cannot read tagged data, so `revalidateTag` does not target Go
  region renders; `revalidatePath` does.
- Region renders run synchronously today, so tags are only recorded for data
  read during the page render itself.
- `fetch` cache options in Node API routes and actions need the staged data
  cache runtime (`.krate/cache-runtime.mjs`, written by `krate serve`/`dev`); if
  `@krate/runtime` cannot be found the server prints a warning.
