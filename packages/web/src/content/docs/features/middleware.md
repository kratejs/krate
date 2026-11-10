---
title: Middleware
description: Intercept requests before a page renders.
order: 10
---

# Middleware

Middleware runs **before page rendering** and can short-circuit requests with a
custom `Response` - useful for auth, geolocation, A/B testing, redirects, and
bot detection.

## File convention

Create `middleware.ts` at the project root (or in `src/`):

```typescript
// middleware.ts
export function middleware(request: Request) {
  // Return a Response to short-circuit (redirect / rewrite / custom response).
  // Return undefined / null to continue to the page handler.
  const url = new URL(request.url);
  if (url.pathname.startsWith("/admin") && !isAuthed(request)) {
    return Response.redirect(new URL("/login", request.url));
  }
}
```

The handler can be the default export instead. A file with neither is a build
error (`KR7143`), and a server running such a module answers `503` rather than
letting requests through.

| Return value | Behavior |
|--------------|----------|
| `Response` | Short-circuits - the response is served instead of the page (any status, including `200`) |
| `Response` with an `x-middleware-next: 1` header | Continues to the page handler; the response's other headers are added to the final response |
| `Response` with an `x-middleware-rewrite: /other` header | Continues, serving the page at `/other` (the browser URL does not change) |
| `undefined` / `null` | Continues to the page handler |

`async` middleware is supported. A thrown error (or a rejected promise) fails
closed with `503 middleware failed` and is logged with its message; in dev the
message is included in the response body.

To add headers to every response without short-circuiting, return a
pass-through response:

```typescript
export function middleware(request: Request) {
  return new Response(null, {
    headers: { "x-middleware-next": "1", "x-frame-options": "DENY" },
  });
}
```

Repeated `Set-Cookie` headers are preserved individually (use
`headers.append("set-cookie", ...)`).

## The request

`request` is a standard Web `Request`: `request.url` is the absolute URL the
client used (built from the `Host` header, so
`new URL("/login", request.url)` redirects on the right host),
`request.headers.get("cookie")` is case-insensitive, and
`request.cookies.get(name)` returns `{ name, value }` like Next.js. Behind a
reverse proxy that rewrites `Host`, set
[`server.trustProxy`](/docs/reference/config/#server) so the original
`X-Forwarded-Host` / `X-Forwarded-Proto` are used.

## Scoping with `matcher`

By default middleware runs for every page request, API route
(`/api/*`) and server action, but skips static assets (requests whose path ends
in a file extension such as `.css`, `.js`, `.png`, `.woff2`). Export a
Next-compatible `config.matcher` to choose exactly which paths run it:

```typescript
export const config = {
  matcher: [
    "/dashboard/:path*",          // /dashboard and everything below it
    "/account",
    "/((?!_next|favicon.ico).*)", // regex group with a negative lookahead
  ],
};
```

Supported syntax: literal paths, `:name`, `:name*`, `:name+`, `:name?`,
parenthesised regular expressions, and `{ source: "/path" }` objects. When a
matcher is declared it applies to every path, including assets. `has`,
`missing` and `locale` conditions are not supported by the embedded runtime and
fail the middleware with an explanatory error.

## How it's wired

- `middleware.ts` is compiled to `.krate/middleware.js` during the build.
- By default it runs in the **embedded QuickJS runtime**, from a pool of VMs
  (one request does not wait for another). Set `ssr.middlewareRuntime: "node"`
  (or `"bun"` / `"deno"`) to run it in the sidecar instead. The embedded
  runtime has the same Web APIs as [API routes](/docs/reference/api-runtime-compat/),
  and the build warns with [`KR7160`](/docs/reference/diagnostics/#kr7160) when
  middleware uses one it lacks.
- In `krate dev`, the module is reloaded whenever `.krate/middleware.js`
  changes (and picked up if you add it while the server runs).
- Combined with [redirects & rewrites](/docs/features/redirects-rewrites/),
  middleware gives you request-level control over routing.

## Use cases

- **Auth** - redirect unauthenticated users.
- **Bot detection** - serve a custom response to known bots.
- **A/B testing** - rewrite requests to different variants.
- **Header injection** - set security headers per route.
