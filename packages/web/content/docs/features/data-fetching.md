---
title: Data Fetching
order: 3
---

# Data Fetching

Krate offers two complementary data-fetching models: build-time props and
client-side resources.

## Build-time data (`getStaticProps`)

```typescript
// Sync — object-literal returns are extracted from the AST
export function getStaticProps() {
  return { props: { title: "Hello" } };
}

// Async — detected via an await heuristic, executed via `npx tsx`
export async function getStaticProps() {
  const res = await fetch('https://api.example.com/data');
  return { props: { data: await res.json() } };
}
```

Props are passed to the page component:

```tsx
export default function Page({ data }) {
  return <pre>{JSON.stringify(data, null, 2)}</pre>;
}
```

Combine with `revalidate` for ISR (see [Rendering](/docs/core-concepts/rendering/)).

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

## Server data (`getServerSideProps`)

For per-request data, use `getServerSideProps` — rendered by the Node sidecar.
See [Rendering](/docs/core-concepts/rendering/).

## API routes

For your own JSON endpoints, see [API Routes](/docs/features/api-routes/).
