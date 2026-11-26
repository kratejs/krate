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

A synchronous server component sees what the build can compute, including the
time of the build. A server component that is `async` (it `await`s a `fetch`, a
database call) renders via the SSR renderer; an async child of a synchronous
page streams per request (`KR7104`, informational) and `output: "static"`
rejects it (`KR7103`), while an async page with `generateStaticParams` (or a
non-dynamic async page under `output: "static"`) is prerendered at build time
unless it uses request-only APIs or opts into `force-dynamic`.

### Dates and times at build time

The build evaluates dates in a fixed environment, so a build gives the same
output on every machine:

- **Now.** `new Date()`, `Date.now()`, `Date()` and `Math.random()` are the
  value at build time, one instant per build. In a static or server component
  that is the final value. In a client component the static HTML shows the build
  value and the page computes the expression again when it hydrates; the text is
  replaced, so there is no hydration error, but it can change on load.
  Set `SOURCE_DATE_EPOCH` (integer seconds since the Unix epoch) for reproducible
  builds: `new Date()` is that instant, `Math.random()` becomes a function of it
  and of the call site, and two builds of the same sources are byte-identical.
- **Deterministic dates fold everywhere.** `new Date(ms)`, `new Date("2020-01-01")`,
  an ISO string with `Z` or an offset, `Date.UTC(...)`, `getTime()`, the
  `getUTC*` and `setUTC*` methods, `toISOString()`, `toJSON()` and `toUTCString()`
  do not depend on the machine, so they are computed at build time in any
  component and need no client code.
- **Time zone and locale.** Anything that reads the local time zone or the
  locale depends on the environment: `getHours()`, `getDate()`, `toString()`,
  `toLocaleDateString()`, `new Date(2020, 0, 1)`, `new Date("2020-01-01T10:00")`
  and the like. At build time the zone is **UTC** and the locale **en-US**
  (`toLocaleDateString()` is `1/2/2020`, `getTimezoneOffset()` is `0`), whatever
  the build machine uses. Only these en-US options are modelled: `weekday`,
  `year`, `month`, `day`, `hour`, `minute`, `second`, `hour12`, `hourCycle`
  (`h12`/`h23`), `timeZoneName`, `dateStyle` and `timeStyle`, with `timeZone`
  `"UTC"`; another locale, a time zone other than UTC or any other option is
  left unresolved.
- **Client components.** An expression that depends on the environment is not
  folded into a client component's code: the build writes the UTC/en-US text
  into the HTML and the browser recomputes it for the visitor's own time zone and
  locale when the page loads. To avoid the change on load, format with
  `toISOString()`, `getUTC*` or `toLocaleDateString("en-US", { timeZone: "UTC" })`,
  which give the same text in both places.
- `Map`, `Set` and iterators work at build time like in Node: `new Map(entries)`,
  `new Set(values)`, `arr.entries()`, `arr.keys()`, `map.values()` and the like
  return real iterators (`.next()`, `.toArray()`, `.map()`) that also work with
  `for...of`, spread, `Array.from` and destructuring.
- Regular expressions work at build time with JavaScript semantics (via the
  embedded QuickJS engine): regex literals (`/pattern/flags`) and
  `new RegExp(...)` can be stored in module constants and objects, passed to
  `exec`, `test`, `String.prototype.match`, `matchAll`, `replace`, `replaceAll`
  (with string or function replacers), `search` and `split`, and honour
  `lastIndex`, `g`/`y`/`d` flags, lookbehind, backreferences and named capture
  groups (`match.groups`).
- A local such as `const now = new Date()` is a build value in a server or
  static component. In a client component it is evaluated by the browser when
  the page loads, so write the expression in the JSX
  (`{new Date().getFullYear()}`) to have a build value in the HTML.

### Values built by statements at build time

A component body can build up a value with statements, not only with one
expression, and the build still evaluates it:

```tsx
function Nav({ pages }) {
  const groups = [];
  for (const p of pages) {
    let g = groups.find((x) => x.dir === p.dir);
    if (!g) { g = { dir: p.dir, items: [] }; groups.push(g); }
    g.items.push({ title: p.title, href: p.href });
  }
  return <nav>{groups.map((g) => <section><h2>{g.dir}</h2>{/* ... */}</section>)}</nav>;
}
```

- **Statements.** `for`, `for...of`, `for...in`, `while`, `do...while`,
  `switch`, `try`/`catch`/`finally`, `if`, `break`, `continue` (labelled ones too),
  `let` reassignment, `+=` on strings and numbers, and nested loops.
- **Changing values in place.** `push`, `pop`, `shift`, `unshift`, `splice`,
  `sort`, `reverse`, index and property assignment, `delete`, `Map.set` and
  `Set.add` work on arrays, objects, Maps and Sets the component creates, and
  push objects, arrays and JSX. An object reached from an array is the same
  object as the variable that holds it (`g` above is an element of `groups`), so
  a change through one shows through the other.
- **Module values are copied when they change.** A module constant, or a prop
  that came from one, is shared by every component and page. Build-time
  evaluation never changes it: the first statement that changes such a value
  works on a copy that belongs to that component's render. The component sees its
  own change (including through another name for the same object); other
  components and pages, and the module constant itself, keep the original.
  This differs from JavaScript, where the change would be visible to all.
- **Limits.** A component may run 100000 loop iterations in total at build time. A
  `return` inside a loop is evaluated in a component without state; in one with
  state the loop is left to the browser.
- **When the build gives up.** A statement the build cannot run because it uses an
  unsupported construct, or passes the iteration limit, is reported as a
  `KR8356` warning that names the statement and the reason; the value it builds is
  then computed in the browser (or, in a component without state, reported as
  `KR8352`). A statement that depends on state, such as a loop over a signal, is
  computed in the browser as before and does not warn.

## Per-request data (runtime components)

Mark a component with `// @runtime` (or a `*.runtime.tsx` file). It is rendered
at **request time** by the SSR sidecar (Node/bun/deno) and spliced into the
static shell as its own region - streamed to the client without a full-page
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

## Async server components

An `async` component fetches data per request with plain `await`, in the page
itself or in any child, and can render `"use client"` components that hydrate
in the browser. See [Async Server Components](/docs/features/server-components/).

```tsx
export default async function Post({ params }) {
  const { slug } = await params;
  const post = await fetch(`https://api.example.com/posts/${slug}`).then((r) => r.json());
  return <h1>{post.title}</h1>;
}
```

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

`export const revalidate = 60` and `export const dynamic = "force-dynamic"` are
accepted as well. Server-side `fetch` honors `{ next: { revalidate, tags } }`
and `cache: "no-store"`, and `unstable_cache` caches any async function; both
are invalidated with `revalidateTag` / `revalidatePath`. See
[Caching and Revalidation](/docs/guides/caching-and-revalidation/).

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
value. Directly destructured params (`{ id }`) work the same way. Async pages
with `generateStaticParams` (and `generateMetadata`) are also prerendered at
build time for every returned param set (with `params` readable both as
`params.id` and `await params`) and marked **SSG**, including with
`output: "static"`. Pages that export `dynamic = "force-dynamic"` or read
`headers()`, `cookies()`, or `searchParams` stay request-time (or report
`KR7103` under `output: "static"`).

## Client-side data (`createResource`)

```typescript
import { createResource } from '@krate/runtime';

const [data, actions] = createResource(source, fetcher);
```

- **Reactive** - auto-refetches when the source signal changes.
- **Abort** - in-flight requests are aborted when the source changes.
- **Optimistic updates** - `mutate(prev => ...)`.
- **Manual refetch** - `refetch()`.

```tsx
export default function User({ id }) {
  const [user, { refetch }] = createResource(
    () => id,
    async (id) => (await fetch(`/api/user/${id}`)).json(),
  );

  return (
    <div>
      {user.loading && <p>Loading...</p>}
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
