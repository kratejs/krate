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
- Security: a matcher-less `middleware` no longer skips URLs that merely end in a
  static extension. `/api/files/report.pdf` used to reach the API route without
  middleware; it now runs it, and only a real static file skips it (Go host and
  portable handler).
- Security: the canonical-slash redirect starts with `./`, so a last path segment
  like `ftp:3232235777` cannot redirect off-site.
- Security: the Node, Bun and Deno API sidecar only answers the Krate host (it
  checks a per-process secret), so its loopback port can no longer be used to
  skip middleware, CSRF and body limits.
- Security: with `server.trustProxy`, the client address is the `X-Forwarded-For`
  entry `server.trustedProxies` hops (default 1) from the right, not the leftmost
  entry a client controls.
- Security: the no-JS flash cookie is HMAC-signed, action cookies are `Secure` over
  https and whenever `SameSite=None`, CORS responses carry `Vary: Origin` for
  disallowed origins too, request paths are escaped in logs, request headers are
  capped at 64 KiB, and the Go API socket directory must be private to the user.
- Fixed: the SSR renderer is supervised. After a crash every SSR/ISR page used to
  answer `200` with the empty build-time shell and ISR refresh stopped for good;
  the renderer now restarts, and pages answer `503` (`no-store`) while it is down.
  A region stream cut short is no longer a cacheable success.
- Fixed: sidecars no longer outlive a killed host (Linux parent-death signal, and a
  parent watchdog in the Node scripts).
- Fixed: ISR caches evict the least recently used entry (a client could flush the
  whole page cache with 512 cheap requests), background refresh re-renders only
  variants older than their window, and the first refresh waits one interval.
- Fixed: a middleware or config rewrite to a page path without a trailing slash
  (`/feedback`) answered `301` and changed the URL; it is now served in place.
- Fixed: a page whose default export is `@runtime` (or `// @runtime`) rendered an
  empty region.
- `src/pages/data.json.ts`, `feed.xml.ts`, `robots.txt.ts` and similar build as
  files: `GET` runs once at build time and its body is written to the output,
  instead of an empty HTML shell (`KR7121` for failures).
- A page's `export const metadata` (or a `generateMetadata` that returns a literal)
  becomes head tags; an explicit `<Head>` still wins. Values the build cannot read
  warn (`KR7122`).
- `src/pages/api/*.ts` no longer builds an empty page; it warns (`KR7120`) that API
  routes live in `src/api`.
- `krate deploy --target docker` runs as the `node` user and adds a `HEALTHCHECK`;
  the Cloudflare entry trusts `X-Forwarded-*` only with `server.trustProxy`.
- Fixed: `node_modules/.bin/krate` was missing after `npm install @krate/core` on
  npm 10. The platform packages declared `bin`, and npm 10 removes the shared
  link when it rolls back the ones that do not match the machine. They no
  longer declare it (`check:release` enforces that); the fix reaches users with
  the next release of the platform packages. Until then `npx krate` works, as
  does `npm rebuild`.
- Fixed: `krate migrate nextjs` corrupted an `export default async function`
  page that has `generateMetadata` (it emitted `<async {...props} />`), and the
  generated metadata component was rejected by the renderer
  (`invalid element name "__KrateMetadata"`). An async page keeps `async` and is
  rendered per request.
- Fixed: a page whose default export forwards `{...props}` to another component
  (what the migrator generates) lost its route params at build time, and an async
  `[slug]` page listed by `generateStaticParams` rendered with empty params.
- `krate migrate nextjs` translates the static keys of a `next.config.*` even
  when another key is computed, accepts `async redirects()`, `rewrites()` and
  `headers()` that return a literal array, explains `revalidate`/`dynamic`/
  `dynamicParams` on pages and flags them in layouts, lists every `package.json`
  change, pins `@krate/*` to the running compiler version, and marks the files
  `--apply` would overwrite in the dry run.
- A dev rebuild whose only page fails reports the failure instead of blaming
  `pagesDir`; `krate build` no longer prints each failure twice; esbuild's
  "the opening tag is here" note includes its position; and the generic
  TypeScript hint is gone from syntax errors.
- `krate build ./typo` fails with "project directory ... does not exist" instead
  of creating the directory, and `krate doctor` no longer calls an unset `output`
  a server build.
- `create-krate-app` and `create-krate-docs` pin every `@krate/*` dependency to
  their own version (`^x.y.z`, or the exact version of a prerelease) instead of
  `latest`.
- Fixed: plugins could read and write outside the project through a symlink
  (`pluginapi`), and the MCP policy let `read_file` serve `.env` through a link
  to it or under another case (`.ENV`, `.GIT/config`). The title of a page
  generated by a plugin route is escaped.
- Fixed: a markdown autolink such as `<javascript:alert(1)>` or `<data:text/html,x>`
  was emitted as a link. Autolinks go through the same scheme check as `[x](url)`
  links now and render as text. The icon sanitizer also drops SMIL `set`/`animate`
  that target `href`, `<style>` that imports or fetches external files,
  external `<image>`, `<iframe>`, `<object>` and `<embed>`.
- Fixed: `<Image width={1000}>` (also 2000, 3000, 10000) failed with `KR8803`.
  Images are now sized to what you ask for: `width={100}` ships 100w and 200w
  variants and `sizes="100px"` instead of six 640-2000w files. JPEGs with an EXIF
  orientation (phone photos) are rotated before resizing, so the variants and their
  dimensions match what the browser shows, and GIFs work (animated ones are served
  as they are).
- Fixed: page-level and imported `.md`/`.mdx` ignored the `markdown` config (`math`,
  `mermaid`, `gfm`, `admonitions`, `headingAnchors`, `codeTheme`).
- Fixed: a markdown list, quote or table directly inside a multi-line JSX element
  in MDX swallowed the closing tag and failed the build with an error pointing into
  generated code. A parse failure in compiled markdown now says its position refers
  to the generated code.
- Relative images in markdown, MDX and content collections (`![x](./pic.png)`) are
  copied to `/assets/` with a hashed name and linked there, under `basePath`.
- Content: enum fields are typed as unions, fields with a `default` are no longer
  optional, `date` fields must hold an ISO date (a validation error otherwise),
  hidden files such as editor lock files are skipped, and two entries with the same
  slug (`a.md` and `a.mdx`) are an error. Frontmatter folds multi-line quoted
  strings, and a `+++` (TOML) or unterminated block warns instead of silently
  rendering as text.
- Markdown code: fences with `{1,3-5}` highlight those lines on pages as well as in
  collections, titled fences keep their header inside asides, `mdx`/`astro`/`prisma`
  are highlighted, and short code blocks and tables no longer add a focus stop and a
  landmark each (only those likely to scroll do).
- `<Icon>` sizes to the text (`1em`) like Iconify's own SVGs, keeps a solid icon's
  `fill` instead of outlining it, and maps `size={n}` to width and height.
- Tailwind: `shadow-xl/20`, `drop-shadow-xl/50`, `inset-shadow-*/n`, `text-shadow-*/n`,
  `bg-size-[...]` and `bg-position-[...]` are generated.
- Fixed: `Tabs`, `Accordion` and other zero-JS components built from a `.map()` over a
  build-time array silently showed every panel and ignored `defaultValue`. Their
  values are collected from the unrolled array now; a value that cannot be read at
  build time warns with `KR8820`. A trigger's `class` may be computed.
- Components: `OTPField` edits the visible digits (Backspace, paste, autofill) and
  submits under `name`; the searchable `Select` and `Command` are keyboard operable
  comboboxes with an active descendant; `NativeSelect` shows its placeholder;
  `ContextMenu` opens from the keyboard (ContextMenu key, Shift+F10) with menu roles
  and focus management; `Tooltip` and `HoverCard` are linked with `aria-describedby`,
  close on Escape, can be hovered, and take `asChild` for a real button or link;
  `Pagination` links use `param` and keep the other query parameters; `Checkbox` and
  `Switch` are native inputs with `name`, `value`, `defaultChecked`, `disabled` and
  `id`; every component takes `class`/`className`; modal `Dialog`, `AlertDialog`
  and `Sheet` dim the page, make it inert, lock scrolling and close on an outside
  click; `Form.Field` links its description and error to the control and marks it
  `aria-invalid`; `Card` titles are headings; `Slider` takes `id` and `disabled`.
  Focus rings use one colour and no longer rely on a 15% glow.
- Fixed: docs search snippets showed `&quot;` and `&#39;`.
- Libraries built on React hooks now work. A module that calls a hook of an npm
  React package (zustand, react-hook-form, SWR, TanStack Query, a custom hook
  wrapping one) is bundled for Preact and rendered as an island: server-rendered,
  hydrated, and sharing one store per page. They used to build with no
  diagnostic and throw in the browser. A layout or server page that needs one
  fails with `KR8013`. See "Libraries built on React hooks" in the React
  compatibility guide.
- CSS-in-JS: styled-components, `@emotion/styled`, emotion's `css` prop (with
  `/** @jsxImportSource @emotion/react */`) and MUI run as client-only islands.
  A `css` prop without emotion's runtime warns (`KR8014`).
- `client:visible` and `client:idle` on an island's root tag defer mounting (and
  the chunk download) until the island is near the viewport or the browser is
  idle. A directive on anything else warns (`KR8012`).
- Fixed: islands from libraries written as `import React from "react"` (recharts,
  Headless UI) failed in the browser because the shared `preact/compat` chunk
  had no default export.
- Islands now compile project modules with Preact's JSX runtime even when
  `tsconfig.json` sets `jsxImportSource` to `@krate/runtime`.
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
- Fixed: `export { Card } from "./Card"` and `export { default as Card }` barrels
  never loaded the module, so `<Card />` rendered nothing; named re-exports and
  `default as` renames now resolve, also through a tsconfig alias like
  `@/components`.
- Fixed: two same-named exports imported by one page (`import { Card as A } from
  "./a"`, `import { Card as B } from "./b"`) showed one module's component for
  both. One module's export now gets a private name and its importers are
  rewritten. `KR7116` is reported only for modules that cannot move (re-exported,
  imported as a namespace, `import()`ed, or "use client"/"use server").
- Fixed: a file beside a same-named directory (`Button.tsx` and `Button/`)
  resolved to the directory; a file now wins, as in Node and tsc.
- A bare import that `paths` and `node_modules` cannot satisfy is tried against
  tsconfig `baseUrl`. The package.json `browser` field is honoured for client
  builds.
- Fixed: minified code with a string right after an identifier or `}`
  (`import{a}from"./b.js"`) failed to parse, and so did a JSX attribute named
  `extends`.
- Fixed: a binding named like a runtime name (`export const krateTheme`,
  `export function h`) lost its export or import name; it now keeps both. A
  project component named `Link`, `Head`, `Script`, `Style`, `Icon`, `Image` or
  `Suspense` replaces the built-in instead of being silently ignored.
- Fixed: `function Box(p)` next to any module-level const failed the build with
  `cannot read properties of undefined`; a lone component parameter is now
  treated as the props object whatever its name. `Table.Cell = function ...`
  (or an arrow) is now a component.
- `krate.config.ts`: shell environment variables and `NODE_ENV` are visible in
  `process.env`, `fs.existsSync` works, and `process.cwd/argv/platform`,
  `__dirname`, `__filename`, `module.exports` beside imports, `node:os` and a
  small `Buffer` are provided. Plugin hooks see the same environment.
- Build errors instead of silent output: importing `.scss`, `.sass`, `.less` or
  `.styl` (`KR7123`, Krate has no compiler for them), and importing a name
  `@krate/runtime` does not export (`KR7124`, it used to fail in the browser with
  `x is not defined`).
- Fixed: an asset imported through a tsconfig alias (`@/assets/logo.png`) was
  emitted but referenced as a bare identifier.
- A tier decorator on a function or const wins over a same-named binding in the
  file (it used to leave an esbuild "Decorators are not valid here" error), and
  `@client export default <expression>` now explains how to name the component.

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
