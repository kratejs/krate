---
title: Error Handling
order: 8
description: Handle build-time, render-time, and client-side errors in a Krate app.
---

# Error Handling

Krate fails loudly at build time and degrades gracefully at runtime. This guide
covers where errors surface and how to handle each kind.

## Build-time errors

Compile errors (lexer/parser/bundler/renderer) are reported with
`file:line:col`, a source line, a caret, and an actionable hint:

```
src/pages/about.tsx:20:0: expected ), got EOF
  <p>...</p>
       ^
  hint: check for an unclosed element or expression
```

`krate build` collects every error and exits non-zero. Fix them before
deploying - a failed build never writes a partial `dist/`.

To surface errors early in CI, run `krate check` (quality gates) and
`krate typecheck` (type errors) after `krate build`.

## Render-time errors (SSR/ISR/streaming)

Request-time pages are rendered by the Node SSR sidecar. If a page throws:

- The response is `500` with the nearest error page (`error.tsx` or `500.tsx`
  in the page's directory or above, see
  [Not-found and error pages](/docs/core-concepts/routing/#not-found-and-error-pages)),
  rendered inside the segment's layouts. `X-Krate-Error: render` stays on the
  response, and it is never cached (an ISR page does not store a failed render).
- The page's `error` prop is sanitized in production: the message is
  `"Internal Server Error"` and a `digest` identifies the failure. The real
  error and stack go to the server log with the same digest:

  ```
  [krate] Error rendering /blog/post (digest 3f9a1c20b4): Error: database is down
  ```

  `krate dev` passes the real message and stack instead.
- Without any error page the server answers `500` with a plain
  `Internal Server Error` document, or, for a page whose body can still be
  shown, the baked shell.

`notFound()` thrown while rendering answers `404` with the nearest not-found
page the same way.

## The dev overlay and toolbar

`krate dev` injects a dev-only overlay bundle (`chunks/krate-dev.js`). It is
never included in production HTML. It shows:

- **Build/compile errors** as structured diagnostics - `file:line:col`, the
  source line with a caret, and the hint - streamed over Server-Sent Events.
- **Runtime errors** (uncaught exceptions, unhandled rejections, hydration
  failures), forwarded to the dev server and logged to the terminal as
  `[browser] ...` so multi-tab sessions stay in sync.

Each diagnostic has an **Open in editor** button (and a clickable location
link); configure the command with `devServer.editor`. Press `Esc` to dismiss;
the overlay clears on the next successful build. A small toolbar (bottom of the
page) shows build status, the current route (updated on client-side navigation),
and a reload button. Toggle either with `devServer.overlay` /
`devServer.toolbar`.

If the **initial** build fails, the dev server still starts and the overlay
shows the error - fix the file and save to recover.

## Client-side errors

Hydration errors are caught and reported:

- **Dev:** the overlay shows the message and stack; the error is also sent to the
  dev server.
- **Prod:** set a global hook to forward errors to your telemetry:

```ts
// runs on the client after hydration
window.__krate_onHydrationError = (err) => {
  navigator.sendBeacon("/telemetry", JSON.stringify({ message: String(err) }));
};
```

If hydration throws, the router falls back to a full page load so users still
reach the page.

## Error pages

Create `404.tsx` / `not-found.tsx` and `error.tsx` / `500.tsx` in `src/pages`
or in any directory below it. They are ordinary components, wrapped in the
directory's layouts and written next to it (`dist/404.html`,
`dist/blog/404.html`, `dist/blog/500.html`):

```tsx
// src/pages/404.tsx
export default function NotFound() {
  return (
    <section class="not-found">
      <Head><title>404 - Not found</title></Head>
      <h1>Page not found</h1>
      <a href="/">Go home</a>
    </section>
  );
}
```

```tsx
// src/pages/dashboard/error.tsx
export default function DashboardError({ error, reset }) {
  return (
    <section>
      <h1>The dashboard failed</h1>
      <p>Reference: {error.digest}</p>
      <button onClick={reset}>Try again</button>
    </section>
  );
}
```

`error.tsx` also catches client-side errors below it: a component that throws
while rendering, in an effect, or in a reactive update replaces the segment's
content with this page while the layouts stay mounted, and `reset()` mounts the
content again. Event-handler errors are not caught; handle them where they
happen. Nested `error.tsx` files catch from the inside out, and an error page
that fails hands the error to the next one up.

Under an [async layout](/docs/features/server-components/#async-layouts) the
page renders per request, so `error.tsx` catches on the server instead: a page
that throws renders the error page inside the layout (status `200`, with a
generic message and a `digest`), and `reset()` reloads the page.

`forbidden.tsx` (403) and `unauthorized.tsx` (401) work the same way for the
`forbidden()` and `unauthorized()` calls; see
[Forbidden and unauthorized](/docs/core-concepts/routing/#forbidden-and-unauthorized).

A page that throws while it is **prerendered** still fails the build with
`KR3010`, because there is no meaningful page to ship. Catch the failure with a
class error boundary inside the page, or move the failing work to request time.

## API route errors

A handler that calls `notFound()`, `forbidden()` or `unauthorized()` answers
`404`, `403` or `401` with a JSON `{ "error": ... }` body. Any other handler that throws is logged and returns `500`. In production the JSON body
is the generic `{"error":"Internal Server Error"}`; in `krate dev` it also
carries the message. Validate input and return explicit statuses:

```ts
export async function POST(req: Request) {
  const body = await req.json().catch(() => null);
  if (!body) return new Response(JSON.stringify({ error: "bad json" }), { status: 400 });
  return new Response(JSON.stringify({ ok: true }), { status: 200 });
}
```

## See also

- [Troubleshooting](/docs/guides/troubleshooting/)
- [Rendering](/docs/core-concepts/rendering/)
- [Deployment](/docs/guides/deployment/)
