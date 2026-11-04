# Changelog

Krate uses [GitHub Releases](https://github.com/kratejs/krate/releases) for the
detailed changelog. This file records the release train at a glance.

## Unreleased

- `krate version` reports the native binary instead of the npm wrapper, and warns
  when the two differ.
- Backslashes in tsconfig `baseUrl` and `paths` are normalized, so a config
  written on Windows resolves on Linux and macOS.
- `krate deploy` runs server features on more hosts. A portable server handler
  (`@krate/runtime/server-handler`) is bundled with the build data into one
  module for `--target node` (new: `dist/.krate/server/server.mjs`, no Go
  binary), and for `vercel` (Build Output API v3), `netlify` (Functions) and
  `cloudflare` (Workers with static assets) instead of refusing projects with
  SSR/ISR/API routes/actions/middleware. A request matrix checks the node target
  against `krate start`. Go API routes, Go components and sidecars are refused
  with a hint to use Docker. Migration note: caches are per instance on the
  serverless targets, so `revalidatePath`/`revalidateTag` reach only the
  instance that handled the request (reported under `skipped`).
- Per-segment not-found and error pages: `404.tsx`/`not-found.tsx` and
  `error.tsx`/`500.tsx` in any pages directory. The nearest not-found page
  answers unmatched URLs, closed dynamic params and `notFound()` (404, inside
  the segment's layouts); the nearest error page answers uncaught server errors
  (500, sanitized message plus a digest, never cached) and is a client error
  boundary around the segment with `reset()`. Two files for one role, or a
  segment page in a `@slot`, are build errors (`KR9001`, `KR9002`). The Next.js
  migrator maps nested `not-found.tsx`, `error.tsx` and `global-error.tsx` to
  native files instead of manual items or a generated class-boundary layout.
  Behavior change: a page that renders inside an error boundary no longer
  disables layout persistence (the runtime now tears down class-component owners
  by route scope).
- Tailwind output is layered like v4 (`@layer theme, base, components,
  utilities`). Migration note: unlayered CSS beats Tailwind utilities, so a
  global reset such as `* { margin: 0; padding: 0 }` must move into
  `@layer base { ... }` or it will override `p-*`/`m-*` utilities.

## 0.5.0-alpha.4

- Next.js migration maps App Router routing, server components, client
  boundaries, cache/revalidation, and framework features.
- Docs and marketing site updated to the new SVG logo and alpha.4 packages.
- Server, MCP, migration, and runtime hardening fixes.
