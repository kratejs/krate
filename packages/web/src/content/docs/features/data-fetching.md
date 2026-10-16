---
title: Data Fetching
description: Fetch data at build time, per request, or on the client.
order: 4
---

# Data Fetching

Krate offers complementary data-fetching models: build-time and serve-time data
via component tiers, route params via `generateStaticParams`, and client-side
resources.

## Build-time data (server components)

Mark a component with `// @server` (or a `*.server.tsx` file). It is evaluated
at **build time** and its HTML is baked into the page with zero client
JavaScript:

```tsx
// @server
export default function ServerTime() {
  return <time>{new Date().toUTCString()}</time>;
}
```

Because server components run at build time, they can `fetch` external data and
render the result — ideal for data that doesn't change per request.

## Per-request data (runtime components)

Mark a component with `// @runtime` (or a `*.runtime.tsx` file). It is rendered
at **request time** by the SSR sidecar (Node/bun/deno) and spliced into the
static shell as its own region — streamed to the client without a full-page
re-render:

```tsx
// @runtime
export default function PriceTag({ price }) {
  return <span>{formatCurrency(price)}</span>;
}
```

Runtime components cover the "changes per request" case that previously used
page-level server-side data functions. A page that imports them is treated as
**streaming**: its shell is baked (layout, server components, suspense
fallbacks) and each runtime component resolves independently.

## ISR / SSR page data

For whole pages whose body is request-time (e.g. a dynamic route without known
params), opt the page into ISR or SSR and read `params`/`query` from the page
props:

```tsx
// src/pages/video/[id].tsx
export const config = { isr: true, revalidate: 60 };

export default function VideoPage({ params }) {
  return <h1>Video {params.id}</h1>;
}
```

- **ISR** caches the rendered page body per URL variant and revalidates it in
  the background after `revalidate` seconds.
- **SSR** renders the page body on every request with the live params/query.

Both bake everything else (layout, head, static content) into a shell at build
time; only the page body is dynamic.

## Dynamic route params (`generateStaticParams`)

For statically-built dynamic routes, `generateStaticParams` pre-generates the
concrete URLs, and each param value is passed to the page component as `params`:

```tsx
// src/pages/video/[id].tsx
export default function VideoPage({ params }) {
  return <h1>Video {params.id}</h1>;
}

export function generateStaticParams() {
  return [
    { id: "abc-123" },
    { id: "def-456" },
  ];
}
```

Each `{ id: ... }` set is expanded into a static page (`/video/abc-123`,
`/video/def-456`) whose component renders with `params.id` set to the matching
value. Directly destructured params (`{ id }`) work the same way.

## Client-side data (`createResource`)

```typescript
import { createResource } from '@krate/runtime';

const [data, actions] = createResource(source, fetcher);
```

- **Reactive** — auto-refetches when the source signal changes.
- **Abort** — in-flight requests are aborted when the source changes.
- **Optimistic updates** — `mutate(prev => …)`.
- **Manual refetch** — `refetch()`.

```tsx
export default function User({ id }) {
  const [user, { refetch }] = createResource(
    () => id,
    async (id) => (await fetch(`/api/user/${id}`)).json(),
  );

  return (
    <div>
      {user.loading && <p>Loading…</p>}
      {user() && <p>Hello, {user().name}</p>}
      <button onClick={refetch}>Refresh</button>
    </div>
  );
}
```

## Other request-time concerns

Per-request redirects and headers are handled by `middleware.ts`, which runs
before page rendering. For your own JSON endpoints, see
[API Routes](/docs/features/api-routes/).
