# Changelog

Krate uses [GitHub Releases](https://github.com/kratejs/krate/releases) for the
detailed changelog. This file records the release train at a glance.

## Unreleased

- Fixed: in production builds a keyed list (`items.map(...)` in a component with
  state) never updated after the first render, because the trimmed runtime
  dropped the list feature. Production builds now ship every runtime feature a
  page's helper calls need, and a test checks that every runtime name the emitted
  code uses is defined by the trimmed runtime.
- Fixed: a custom hook that takes an argument threw `krateParam is not defined`
  in production builds and stopped every component after it. One component
  that throws while hydrating no longer stops the rest of the page.
- Fixed: text, conditionals and lists directly inside `<Suspense>` were never
  bound, so they stayed as rendered; the slots of a conditional's inactive
  branch (`{user && <p>{user.name}</p>}`) no longer run while the branch is
  hidden, which logged a `TypeError` on every load.
- Fixed: destructuring with a function or runtime value (`const { fmt } = cfg`,
  `const { title } = getInfo()`) declared every name as the whole object, and
  declared nothing in a page component. A module-level destructure
  (`const { a } = cfg`) read its source before it was declared and aborted
  hydration.
- Fixed: a component that calls a function prop from an expression
  (`fmt={(s) => s.toUpperCase()}`, render props, `children` as a function) rendered
  nothing and shipped no JavaScript; the browser now fills it in.
- Fixed: `{open && createPortal(...)}` and `{open ? createPortal(...) : null}`
  failed with `createPortal is not defined`. A portal left as a call reports
  KR8010.
- Fixed: the HTML minifier collapsed white space in elements styled
  `white-space: pre*` (`whitespace-pre-wrap`, an inline style, or a rule in any
  linked stylesheet).
- Fixed at build time: `JSON.stringify(value, null, 2)` indented nothing,
  `Number.MAX_SAFE_INTEGER` and `Math.SQRT2` rendered empty, built-in members the
  compiler lacks evaluated to `undefined` instead of staying unknown,
  `for (let ...)` closures shared one binding, and `[...Array(n)].map(...)`
  rendered one row. `Array.from({ length: n }, fn)` now renders its rows in the
  static HTML.
- Fixed: two `const [a] = useState(...)` declarations without setters in one
  component redeclared `__krate_hole1`, a SyntaxError that stopped the page script.
- A slot with no build-time value and no client code to fill it now warns
  (`KR8011`) instead of rendering empty silently.
- Tier decorators: `@server`, `@static`, `@runtime` and `@client` can go directly
  on a function, class or const. On a file's default export or only exported
  component they classify the whole file, like a `// @server` comment; elsewhere
  they classify one component. Misplaced ones fail with `KR7115`.
- `krate typecheck` runs `krate types` and `tsc --noEmit` without the errors `tsc`
  reports for tier decorators, and `@krate/ts-plugin` does the same in editors.
  Scaffolds use both.
- Fixed: modules on a page shared one namespace, so two files with a private
  `up`, `Row` or `LABEL`, or two default exports named `Card`, showed one module's
  value for both, in static HTML and in client code. They are now kept apart. A
  page that imports one name from two modules gets `KR7116`.
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
