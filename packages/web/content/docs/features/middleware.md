---
title: Middleware
order: 8
---

# Middleware

Middleware runs **before page rendering** and can short-circuit requests with a
custom `Response` — useful for auth, geolocation, A/B testing, redirects, and
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

| Return value | Behavior |
|--------------|----------|
| `Response` | Short-circuits — the response is served instead of the page |
| `undefined` / `null` | Continues to the page handler |

## How it's wired

- `middleware.ts` is compiled to `.krate/middleware.js` during the build.
- It's executed via the Node.js sidecar before page rendering.
- Combined with [redirects & rewrites](/docs/features/redirects-rewrites/),
  middleware gives you request-level control over routing.

## Use cases

- **Auth** — redirect unauthenticated users.
- **Bot detection** — serve a custom response to known bots.
- **A/B testing** — rewrite requests to different variants.
- **Header injection** — set security headers per route.
