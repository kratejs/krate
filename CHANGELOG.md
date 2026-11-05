# Changelog

Krate uses [GitHub Releases](https://github.com/kratejs/krate/releases) for the
detailed changelog. This file records the release train at a glance.

## Unreleased

- Tier decorators: `@server`, `@static`, `@runtime` and `@client` can go directly
  on a function, class or const. On a file's default export or only exported
  component they classify the whole file, like a `// @server` comment; elsewhere
  they classify one component. Misplaced ones fail with `KR7115`. `tsc` reports
  them, see the component tiers guide.
- Decorators work in `.js` and `.jsx` files, not only TypeScript.
- A decorated class component now always warns (`KR3011`), including member
  decorators and legacy decorators, which used to report an unresolved component
  or drop the decorator silently.
- Fixed: an import from an alias like `@server/lib` near the top of a file changed
  the tier of its components.
- `@krate/core` no longer publishes a stray Linux compiler binary, and the npm
  wrapper picks the platform package when installed from a registry. In
  `0.5.0-beta.1` a clean npm or pnpm install failed on Linux with
  `spawn .../bin/krate EACCES`.
- Build failures print the diagnostic hint. A broken tsconfig path alias now
  shows the directory its targets resolved against.
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
- Serverless targets now serve per-segment not-found and error pages (nearest
  page, inside layouts, `no-store`, sanitized error with a digest, never cached
  by ISR) and run middleware for prerendered pages: with middleware, Vercel and
  Netlify route page requests to the function (assets stay on the CDN) instead
  of bypassing it, and the deploy warning is gone. The error digest is the same
  value on every host (portable sha256 in the render core).
