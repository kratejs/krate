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

## The dev overlay

`krate dev` injects a dev-only overlay bundle (`chunks/krate-dev.js`). It is
never included in production HTML, and it lives in its own shadow root so page
styles do not reach it.

### The dock

A small dock sits in a corner of the page (bottom left by default). It shows
the route pattern of the page (`/blog/[slug]`), how it renders (`SSG`, `SSR`,
`ISR`, `STREAMING`) and how long the last rebuild took. While the watcher
builds, it says what it is rebuilding; with errors it shows their count, and
when the dev server goes away it says it is reconnecting. A stylesheet-only
edit swaps the CSS in place and a short toast confirms it. Drag the dock to
another corner, or click the Krate mark to collapse it (a red dot then means
there are issues).

Click the route to open the panel:

- **Route**: the page's source file, params, render mode, layouts, what it
  ships (HTML, gzipped JS against `checks.budget.js`, CSS, hydration time), its
  islands with their signals and hydration time, a head check (title,
  description, `og:image`, canonical, `<html lang>`), and actions: open the
  page source, view the built HTML, rebuild the page, copy the URL.
- **Inspect**: the component inspector (below).
- **Activity**: every rebuild with the changed files, what it rebuilt, its
  duration and a breakdown of where the time went.
- **Logs**: the dev server's output (renderer, API and Go sidecars) merged with
  this tab's `console.error`/`console.warn` and runtime errors, with source
  filters, a text filter and an errors-only switch. It is fetched only while
  the tab is open.
- **Settings**: dock corner, theme (System, Dark, Light), the editor used by
  **Open in editor** (from the editors found on your `PATH`), whether runtime
  errors open the overlay, whether `console.error` counts as an issue, and
  whether the dock starts collapsed. Settings are stored in the browser.

In Krate Native, the panel also has the **Host** and **Memory** tabs (see
[Native dev](/docs/native/dev/#host-inspector)).

### Errors

The error overlay pages through the current issues (a rail on the left, the
arrow keys or the pager):

- **Build errors** show the diagnostic code with a link to its
  [reference entry](/docs/reference/diagnostics/), the message, a code frame with
  line numbers and the column underlined, and the hint. When `krate fix` knows
  the rewrite, a **Safe fix** box previews the change; **Apply fix** (`F`)
  applies it to that file and **Fix all** applies every safe fix, exactly as
  [`krate fix --apply`](/docs/guides/safe-fixes/) does. The overlay closes when
  the next build passes.
- **Runtime errors** (uncaught exceptions, unhandled rejections, failed
  resources, and `console.error` unless turned off) show the source frame, the
  stack split into your frames and folded framework frames, and the component
  that was rendering when it is known. They are forwarded to the dev server and
  logged to the terminal as `[browser] ...`. **Ignore this error for the
  session** hides one that you expect.
- **Server crashes** (the SSR renderer, the Node or Go API sidecar, the
  middleware) show what failed, its exit code, the last console lines with
  their source, and a **Restart sidecar** button.

Every issue has **Open in editor** (`O`) and **Copy for AI**, which copies a
Markdown summary (message, code, location, code frame, hint and stack) to paste
into an assistant or a bug report. `Esc` dismisses the overlay; the dock count
reopens it.

If the **initial** build fails, the dev server still starts and the overlay
shows the error - fix the file and save to recover.

### Component inspector

Press `Alt+I` (Option+I on macOS) to outline every component on the page,
colored by [tier](/docs/core-concepts/component-tiers/): static, server, client,
runtime, and Preact or React islands. Hover one for its source file, live signal
values, shipped size and hydration time; click to open it in the editor,
Shift-click to copy its path, `Esc` to stop.

### Command menu and shortcuts

`Alt+K` opens the command menu: jump to any route (dynamic routes list their
prebuilt paths), or run a dev action (toggle the inspector, rebuild the page,
rebuild everything, restart the dev server, clear the `.krate` cache and
rebuild). `Enter` runs the selected item; `Ctrl+Enter` (Cmd+Enter on macOS)
opens its source file.

| Shortcut | Action |
|----------|--------|
| `Alt+K` | Command menu |
| `Alt+I` | Component inspector |
| `Alt+E` | Show issues |
| `Alt+.` | Hide everything (again to show) |

The shortcuts match the physical key, so they work on every keyboard layout,
and they do nothing while you type in the page's inputs.

`devServer.overlay: false` keeps errors from opening the overlay (the dock still
counts them) and `devServer.toolbar: false` removes the dock and its panel.

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
that throws renders the error page inside the layout (status `500` with
`Cache-Control: no-store`, a generic message and a `digest`), and `reset()` reloads the page.
`retry` is the same function under another name. When the async layout itself
fails, the error page is the prerendered one, built without that layout, so it
never depends on the renderer.

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
