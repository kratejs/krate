---
title: Redirects & Rewrites
order: 7
---

# Redirects & Rewrites

Krate supports config-based URL manipulation via `redirects` and `rewrites` in
`krate.config.ts`.

## Redirects

```typescript
redirects: [
  { source: "/old-page", destination: "/new-page", permanent: true },
  { source: "/legacy/*", destination: "/docs/:splat", permanent: false },
]
```

| Option | Description |
|--------|-------------|
| `source` | The path to match (supports `*` splats and `:param` segments) |
| `destination` | Where to send the visitor |
| `permanent` | `true` → `301`, `false` (default) → `302` |

## Rewrites

```typescript
rewrites: [
  { source: "/docs/:path*", destination: "/documentation/:path*" },
]
```

Rewrites map an incoming URL to an internal path **without changing the URL
the visitor sees**. Use them to serve content from a different route while
keeping clean public URLs.

## Difference

- **Redirects** — the browser is sent to a new URL (`301`/`302`).
- **Rewrites** — the request is served from a different internal path; the URL
  in the address bar is unchanged.

Both support pattern matching with wildcard (`*`) and parameterized
(`:param`) segments, resolved at request time.
