---
title: Redirects & Rewrites
description: Redirect and rewrite URLs from your config.
order: 9
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
| `source` | The path to match (supports `*` splats and `:param`, `:param*`, `:param+`, `:param?` segments) |
| `destination` | Where to send the visitor |
| `permanent` | `true` -> `301`, `false` (default) -> `302` |

`:param*` matches zero or more segments, `:param+` one or more, `:param?` one
optional segment; reuse the name (with or without the modifier) in the
destination. Matched values are percent-encoded when they are put in the
destination, and a path destination can never become an off-site URL: values
like `/\evil.com` or an encoded `//evil.com` are escaped or collapsed so the
`Location` always starts with a single `/`.

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

- **Redirects** - the browser is sent to a new URL (`301`/`302`).
- **Rewrites** - the request is served from a different internal path; the URL
  in the address bar is unchanged.

Both support pattern matching with wildcard (`*`) and parameterized
(`:param`) segments, resolved at request time.

## On hosted targets

`krate deploy` translates these patterns for Netlify, Cloudflare Pages and
Vercel (see [Deployment](/docs/guides/deployment/)). Write the splat explicitly
in the destination (`/docs/:splat`): that form is portable, while the dev
server's implicit "append the rest of the path" when a wildcard source has a
destination without `:splat` is not carried to hosts.
