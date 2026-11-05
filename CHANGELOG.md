# Changelog

Krate uses [GitHub Releases](https://github.com/kratejs/krate/releases) for the
detailed changelog. This file records the release train at a glance: each release
lists what breaks first, then what is new, fixed and security-relevant. Upgrading
from 0.4? Read [Upgrading](https://krate.js.org/docs/guides/upgrading/#from-04-to-05).

## Unreleased

### Breaking

- Importing `.scss`, `.sass`, `.less` or `.styl` is a build error (`KR7123`, Krate
  has no compiler for them); the styles used to be dropped silently. Importing a name
  `@krate/runtime` does not export is a build error too (`KR7124`); it used to fail
  in the browser with `x is not defined`.
- A page that renders inside an error boundary no longer disables layout
  persistence (the runtime tears down class-component owners by route scope).
- Content collections: `date` fields must hold an ISO date (a validation error
  otherwise), two entries with the same slug (`a.md` and `a.mdx`) are an error,
  enum fields are typed as unions, and fields with a `default` are no longer optional.
- Static pages no longer load the shared runtime chunk when other pages of the site
  have islands (26.1 KB gzip, 17.8 KB with `spa: false`, saved per static page). A
  site with no islands already shipped none.
- Behaviour changes for servers behind a proxy or with middleware are listed under
  Security: matcher-less middleware runs on URLs with a static extension, and
  `server.trustProxy` reads `X-Forwarded-For` from the right.

### Added

- `krate fix` repairs the 0.4 template's `tsconfig.json`: `"baseUrl": "./src"` with
  `"@/*": ["./src/*"]` can only mean the project root, so it sets `baseUrl` to `"."`
  (`KR7125`). The edited file also builds on 0.4. The build error for an alias that
  does not resolve names this command.
- Tier decorators: `@server`, `@static`, `@runtime` and `@client` can go directly on
  a function, class or const. On a file's default export or only exported component
  they classify the whole file, like a `// @server` comment; elsewhere they classify
  one component. Misplaced ones fail with `KR7115`. They work in `.js` and `.jsx`
  files too, and a decorator on a function or const wins over a same-named binding.
- `krate typecheck` runs `krate types` and `tsc --noEmit` without the errors `tsc`
  reports for tier decorators, and the new `@krate/ts-plugin` does the same in
  editors. Scaffolds use both.
- Libraries built on React hooks now work. A module that calls a hook of an npm React
  package (zustand, react-hook-form, SWR, TanStack Query, a custom hook wrapping one)
  is bundled for Preact and rendered as an island: server-rendered, hydrated, and
  sharing one store per page. A layout or server page that needs one fails with
  `KR8013`. See "Libraries built on React hooks" in the React compatibility guide.
- CSS-in-JS: styled-components, `@emotion/styled`, emotion's `css` prop (with
  `/** @jsxImportSource @emotion/react */`) and MUI run as client-only islands. A
  `css` prop without emotion's runtime warns (`KR8014`).
- `client:visible` and `client:idle` on an island's root tag defer mounting (and the
  chunk download) until the island is near the viewport or the browser is idle. A
  directive on anything else warns (`KR8012`).
- Islands compile project modules with Preact's JSX runtime even when `tsconfig.json`
  sets `jsxImportSource` to `@krate/runtime`.
- `src/pages/data.json.ts`, `feed.xml.ts`, `robots.txt.ts` and similar build as files:
  `GET` runs once at build time and its body is written to the output, instead of an
  empty HTML shell (`KR7121` for failures).
- A page's `export const metadata` (or a `generateMetadata` that returns a literal)
  becomes head tags; an explicit `<Head>` still wins. Values the build cannot read
  warn (`KR7122`). `src/pages/api/*.ts` warns (`KR7120`) that API routes live in
  `src/api`, instead of building an empty page.
- `krate deploy` runs server features on more hosts. A portable server handler
  (`@krate/runtime/server-handler`) is bundled with the build data into one module
  for `--target node` (`dist/.krate/server/server.mjs`, no Go binary), and for `vercel`
  (Build Output API v3), `netlify` (Functions) and `cloudflare` (Workers with static
  assets) instead of refusing projects with SSR/ISR/API routes/actions/middleware. A
  request matrix checks the node target against `krate start`. Go API routes, Go
  components and sidecars are refused with a hint to use Docker. Caches are per
  instance on the serverless targets, so `revalidatePath`/`revalidateTag` reach only
  the instance that handled the request (reported under `skipped`). They also serve
  per-segment not-found and error pages and run middleware for prerendered pages
  (Vercel and Netlify route page requests to the function, assets stay on the CDN).
  The error digest is the same value on every host. `--target docker` runs as the
  `node` user and adds a `HEALTHCHECK`; the Cloudflare entry trusts `X-Forwarded-*`
  only with `server.trustProxy`.
- Per-segment not-found and error pages: `404.tsx`/`not-found.tsx` and
  `error.tsx`/`500.tsx` in any pages directory. The nearest not-found page answers
  unmatched URLs, closed dynamic params and `notFound()` (404, inside the segment's
  layouts); the nearest error page answers uncaught server errors (500, sanitized
  message plus a digest, never cached) and is a client error boundary around the
  segment with `reset()`. Two files for one role, or a segment page in a `@slot`, are
  build errors (`KR9001`, `KR9002`). The Next.js migrator maps nested
  `not-found.tsx`, `error.tsx` and `global-error.tsx` to native files.
- `krate.config.ts` and plugin hooks see shell environment variables and `NODE_ENV` in
  `process.env`; `fs.existsSync`, `process.cwd/argv/platform`, `__dirname`,
  `__filename`, `module.exports` beside imports, `node:os` and a small `Buffer` are
  provided. A Node built-in the sandbox lacks fails with its name, also when imported
  without the `node:` prefix.
- `krate migrate nextjs` translates the static keys of a `next.config.*` even when
  another key is computed, accepts `async redirects()`, `rewrites()` and `headers()`
  that return a literal array, explains `revalidate`/`dynamic`/`dynamicParams` on
  pages and flags them in layouts, lists every `package.json` change, pins `@krate/*`
  to the running compiler version, and marks the files `--apply` would overwrite in
  the dry run.
- `create-krate-app` and `create-krate-docs` pin every `@krate/*` dependency to their
  own version (`^x.y.z`, or the exact version of a prerelease) instead of `latest`.
- `krate version` reports the native binary instead of the npm wrapper, and warns when
  the two differ. Backslashes in tsconfig `baseUrl` and `paths` are normalized, so a
  config written on Windows resolves on Linux and macOS. A bare import that `paths`
  and `node_modules` cannot satisfy is tried against `baseUrl`, and the package.json
  `browser` field is honoured for client builds.
- Relative images in markdown, MDX and content collections (`![x](./pic.png)`) are
  copied to `/assets/` with a hashed name and linked there, under `basePath`. Fences
  with `{1,3-5}` highlight those lines on pages as well as in collections, titled
  fences keep their header inside asides, and `mdx`/`astro`/`prisma` are highlighted.
- Docs themes: a theme can set `titleSlot: true` in its `defineDocsTheme` descriptor to
  receive the page's leading `<h1>` as `titleHTML`. `@krate/base-docs-theme` marks the
  current nav link with `aria-current` (`SiteNavLink.match: "exact"` limits a link to its
  own page), takes a footer brand block (`footerBrand`), and labels the search shortcut
  with the platform's modifier (`Ctrl K`, or the command key on Apple devices).
- Components: `OTPField` edits the visible digits (Backspace, paste, autofill) and
  submits under `name`; the searchable `Select` and `Command` are keyboard operable
  comboboxes; `ContextMenu` opens from the keyboard; `Tooltip` and `HoverCard` are
  linked with `aria-describedby`, close on Escape and take `asChild`; `Pagination`
  links use `param` and keep other query parameters; `Checkbox` and `Switch` are native
  inputs with `name`, `value`, `defaultChecked`, `disabled` and `id`; every component
  takes `class`/`className`; modal `Dialog`, `AlertDialog` and `Sheet` dim the page,
  make it inert, lock scrolling and close on an outside click; `Form.Field` links its
  description and error to the control and marks it `aria-invalid`; `Card` titles are
  headings. `<Icon>` sizes to the text (`1em`), keeps a solid icon's `fill` and maps
  `size={n}`. Focus rings use one colour.
- Tailwind: `shadow-xl/20`, `drop-shadow-xl/50`, `inset-shadow-*/n`, `text-shadow-*/n`,
  `bg-size-[...]` and `bg-position-[...]` are generated.
- A slot with no build-time value and no client code to fill it warns (`KR8011`)
  instead of rendering empty silently. A decorated class component always warns
  (`KR3011`). `Tabs`, `Accordion` and other zero-JS components built from a `.map()` over
  a value that cannot be read at build time warn with `KR8820`.
- JS plugin hooks and custom check rules are much cheaper on large pages: the page, AST
  and hook context are passed as JSON, and only when the plugin implements the hook.
  `examples/` builds in about 6 s cold (was about 12 s) on a loaded machine. The docs
  table of contents indicator measures layout once and draws when the page is idle.

### Fixed

- In production builds a keyed list (`items.map(...)` in a component with state) never
  updated after the first render, because the trimmed runtime dropped the list feature.
  Production builds ship every runtime feature a page's helper calls need, and a test
  checks that every runtime name the emitted code uses is defined by the trimmed runtime.
- A custom hook that takes an argument threw `krateParam is not defined` in production
  builds and stopped every component after it. One component that throws while hydrating
  no longer stops the rest of the page.
- Text, conditionals and lists directly inside `<Suspense>` were never bound; the slots
  of a conditional's inactive branch (`{user && <p>{user.name}</p>}`) no longer run while
  the branch is hidden, which logged a `TypeError` on every load.
- Destructuring with a function or runtime value (`const { fmt } = cfg`) declared every
  name as the whole object, and declared nothing in a page component. A module-level
  destructure read its source before it was declared and aborted hydration. Two
  `const [a] = useState(...)` without setters in one component redeclared
  `__krate_hole1`.
- A component that calls a function prop from an expression (`fmt={(s) => ...}`, render
  props, `children` as a function) rendered nothing and shipped no JavaScript.
  `{open && createPortal(...)}` failed with `createPortal is not defined`; a portal left
  as a call reports `KR8010`.
- Build-time evaluation: `JSON.stringify(value, null, 2)` indented nothing,
  `Number.MAX_SAFE_INTEGER` and `Math.SQRT2` rendered empty, built-in members the
  compiler lacks evaluated to `undefined` instead of staying unknown, `for (let ...)`
  closures shared one binding, and `[...Array(n)].map(...)` rendered one row.
  `Array.from({ length: n }, fn)` renders its rows in the static HTML.
- The HTML minifier collapsed white space in elements styled `white-space: pre*`.
- Modules on a page shared one namespace, so two files with a private `up`, `Row` or
  `LABEL`, or two default exports named `Card`, showed one module's value for both. They
  are kept apart; `KR7116` is reported only for modules that cannot move (re-exported,
  imported as a namespace, `import()`ed, or "use client"/"use server"). An import from an
  alias like `@server/lib` near the top of a file no longer changes the tier of its
  components.
- `export { Card } from "./Card"` and `export { default as Card }` barrels never loaded
  the module; they resolve now, also through a tsconfig alias. A file beside a
  same-named directory (`Button.tsx` and `Button/`) resolved to the directory; the file
  wins, as in Node and tsc. An asset imported through an alias (`@/assets/logo.png`) was
  emitted but referenced as a bare identifier.
- Minified code with a string right after an identifier or `}` (`import{a}from"./b.js"`)
  and a JSX attribute named `extends` failed to parse. A binding named like a runtime
  name (`export const krateTheme`, `export function h`) lost its export or import name.
  A project component named `Link`, `Head`, `Script`, `Style`, `Icon`, `Image` or
  `Suspense` replaces the built-in. `function Box(p)` next to a module-level const failed
  the build, and `Table.Cell = function ...` is a component now.
- A page whose default export is `@runtime` (or `// @runtime`) rendered an empty region.
- The SSR renderer is supervised: after a crash every SSR/ISR page used to answer `200`
  with the empty build-time shell and ISR refresh stopped for good. It restarts, and pages
  answer `503` (`no-store`) while it is down. A region stream cut short is no longer a
  cacheable success. Sidecars no longer outlive a killed host.
- ISR caches evict the least recently used entry, background refresh re-renders only
  variants older than their window, and the first refresh waits one interval.
- A middleware or config rewrite to a page path without a trailing slash (`/feedback`)
  answered `301` and changed the URL; it is served in place.
- `node_modules/.bin/krate` was missing after `npm install @krate/core` on npm 10. The
  platform packages no longer declare `bin` (`check:release` enforces it); the fix reaches
  users with the next release of the platform packages. Until then `npx krate` works, as
  does `npm rebuild`. `@krate/core` no longer publishes a stray Linux compiler binary and
  the wrapper picks the platform package when installed from a registry; in
  `0.5.0-beta.1` a clean install failed on Linux with `spawn .../bin/krate EACCES`.
- `krate migrate nextjs` corrupted an `export default async function` page that has
  `generateMetadata` (it emitted `<async {...props} />`), and the generated metadata
  component was rejected by the renderer. A page that forwards `{...props}` lost its route
  params at build time, and an async `[slug]` page listed by `generateStaticParams`
  rendered with empty params.
- Build output and messages: a dev rebuild whose only page fails reports the failure
  instead of blaming `pagesDir`; `krate build` prints each failure once and keeps the
  diagnostic hint; esbuild's "the opening tag is here" note includes its position;
  `krate build ./typo` fails with "project directory ... does not exist" instead of
  creating the directory; `krate doctor` no longer calls an unset `output` a server build.
- Plugins could read and write outside the project through a symlink (`pluginapi`), and the
  MCP policy let `read_file` serve `.env` through a link or under another case (`.ENV`). The
  title of a page generated by a plugin route is escaped.
- A markdown autolink such as `<javascript:alert(1)>` was emitted as a link; it goes through
  the same scheme check as `[x](url)` and renders as text. The icon sanitizer also drops SMIL
  `set`/`animate` that target `href`, `<style>` that fetches external files, external
  `<image>`, `<iframe>`, `<object>` and `<embed>`.
- `<Image width={1000}>` (also 2000, 3000, 10000) failed with `KR8803`. Images are sized to
  what you ask for: `width={100}` ships 100w and 200w variants and `sizes="100px"` instead of
  six 640-2000w files. JPEGs with an EXIF orientation are rotated before resizing and GIFs
  work (animated ones are served as they are).
- Page-level and imported `.md`/`.mdx` ignored the `markdown` config; a list, quote or table
  directly inside a multi-line JSX element in MDX swallowed the closing tag. Frontmatter folds
  multi-line quoted strings, and a `+++` (TOML) or unterminated block warns. Hidden files such
  as editor lock files are skipped in collections. Short code blocks and tables no longer add
  a focus stop and a landmark each.
- `Tabs`, `Accordion` and other zero-JS components built from a `.map()` over a build-time
  array showed every panel and ignored `defaultValue`; the values are collected from the
  unrolled array. Islands from libraries written as `import React from "react"` (recharts,
  Headless UI) failed because the shared `preact/compat` chunk had no default export. Docs
  search snippets showed `&quot;` and `&#39;`.

### Security

- A matcher-less `middleware` no longer skips URLs that merely end in a static extension.
  `/api/files/report.pdf` used to reach the API route without middleware; only a real static
  file skips it (Go host and portable handler).
- The canonical-slash redirect starts with `./`, so a last path segment like
  `ftp:3232235777` cannot redirect off-site.
- The Node, Bun and Deno API sidecar only answers the Krate host (it checks a per-process
  secret), so its loopback port cannot be used to skip middleware, CSRF and body limits.
- With `server.trustProxy`, the client address is the `X-Forwarded-For` entry
  `server.trustedProxies` hops (default 1) from the right, not the leftmost entry a client
  controls.
- The no-JS flash cookie is HMAC-signed, action cookies are `Secure` over https and
  whenever `SameSite=None`, CORS responses carry `Vary: Origin` for disallowed origins too,
  request paths are escaped in logs, request headers are capped at 64 KiB, and the Go API
  socket directory must be private to the user.

## 0.5.0-beta.1 (2026-11-04)

### Breaking

- tsconfig `paths` resolve exactly like `tsc`: targets are relative to `baseUrl`. The 0.4
  template (`"baseUrl": "./src"` with `"@/*": ["./src/*"]`) fails with `cannot resolve import
  "@/..."`; set `"baseUrl": "."`. A broken tsconfig is reported, and `extends` is followed.
- Tailwind output follows v4: oklch palette and the v4 font stack, renamed scale
  (`shadow-sm` is the old `shadow`, `shadow-xs` the old `shadow-sm`, likewise `blur`,
  `rounded`), `ring` is 1px `currentcolor`, `outline-none` removes the outline, and Preflight is
  the v4 one, on by default since alpha.1 (`tailwind.preflight: false` opts out). Output is layered
  (`@layer theme, base, components, utilities`): unlayered CSS beats Tailwind utilities, so
  a global reset such as `* { margin: 0; padding: 0 }` must move into `@layer base { ... }` or it
  overrides `p-*`/`m-*`.
- `krate dev` binds `127.0.0.1` (new `devServer.host`; `krate serve` still binds all interfaces).
- `krate start` only serves an existing `dist/`; `krate serve` builds and serves.
- Server-only output moved under `dist/.krate/`, including `manifest.json` (was
  `dist/manifest.json`). Static serving no longer lists directories or serves dotfiles.
- Unresolved imports, content schema errors and `<Image>` problems (`KR8801`..`KR8804`) fail
  the build with located errors; config values of the wrong type fail with the key and the
  expected type, and unknown keys warn.
- The production hydration runtime ships a core plus feature bundles; a build includes only the
  features its pages reference.

### Added

- Markdown renders with goldmark, with real MDX (ESM, inline JSX, expressions) and GFM alerts.
- A real CSS pipeline: `@import` with layers and cycles, CSS modules (keyframes, `composes`,
  `:global`), per-page CSS output, `@theme` and CSS-first Tailwind directives, `@apply`
  anywhere, container queries, and a candidate extractor for the whole file.
- `KRATE_PUBLIC_*` and `import.meta.env` constants are inlined and unexposed reads warn
  (`KR8310`); JSON, `?raw` and `?url` imports inline as constants; `KR8302` flags imported
  values that hydration code cannot see; `KR8810` flags collections shipped to the browser.
- No-JS server action posts carry state back in a flash cookie; `useActionState` keeps its
  state on redirect.
- Hydration source maps point at the `.tsx` handlers; the dev server reloads config and env,
  watches new directories and `public/`, and shows an error shell.
- Components: keyboard-operable overlays, ARIA roles, AA contrast, logical RTL properties,
  controlled overlays, `toast()`, higher-order components and store factories.
- SEO, `basePath`, fonts and search hotkey options; spec-shaped Web APIs in the embedded
  API/middleware runtime; Node `exports` semantics in the resolver.

### Fixed

- Several runtime fixes: update-depth errors reach the error boundary, effects re-track
  dependencies and report loops, `createMemo` is pull-based, `twMerge` groups derive from the
  utility. Tailwind candidates with apostrophes, regexes and template literals are found.
- Windows spawn in the npm wrapper and scaffolders, and a binary version check.
- Static serving: an open redirect and path handling were hardened.

## 0.5.0-alpha.4 (2026-10-28)

### Added

- Next.js migration (`krate migrate nextjs`) maps App Router routing, server components,
  client boundaries, cache/revalidation, and framework features.
- `@krate/runtime/cache` with `revalidatePath`, `revalidateTag` and `unstable_cache`, Next-style
  `revalidate`/`dynamic` segment exports, and an authenticated on-demand revalidation host.
- React islands: compiled React packages run as Preact islands, with React SSR.
- MCP server overhaul (M2-M5), the Simple docs theme, and the new SVG logo.

### Fixed

- Server, MCP, migration and runtime hardening, including ISR safety.

## 0.5.0-alpha.3 (2026-10-23)

### Added

- Server actions, with React 19 action hooks on the same transport.
- React parity: Suspense, `lazy`, `use()` and transitions, class lifecycles and error
  boundaries, keyed lists, React 18/19 events, forms and refs; a browser differential suite
  against React 19.
- Stable diagnostic codes (`KRxxxx`) with a registry, and a typed build-time interpreter for
  compile-time evaluation.
- A rewritten lexer and parser for ES2024 and JSX with source positions.

### Fixed

- Sidecar port collisions between concurrent servers, and a cluster bind retry on Windows.
- Minifier whitespace and comment handling; module-level `let`/`var` shared across components.

## 0.5.0-alpha.2 (2026-10-21)

- `tsx` is gone: `generateStaticParams`, API routes and the SSR renderer are bundled to
  JavaScript and run on plain Node.
- A lite router and a smaller runtime bundle.
- React compatibility R1 complete.

## 0.5.0-alpha.1 (2026-10-20)

- Precompressed assets (`--precompress`, gzip and brotli), router prefetch and reconcile
  batching, i18n, a config schema (`krate config --schema`) with unknown-key warnings.
- Faster builds: esbuild context reuse, image processing cache, a cached file resolver, an
  expression evaluation VM pool, CPU and heap profile flags.
- Tailwind Preflight on by default; no dev serve cache; MCP overhaul; React compatibility R1 beta.

## 0.4.0 (2026-10-16)

- Persistent build cache, CSS dead-code elimination and de-duplication, `krate inspect --json`,
  profiling, and a parallel page pipeline.
- `krate.config.ts` is evaluated in-process (esbuild plus QuickJS) instead of through `tsx`.
- Go runtime and server components with a unified Go API, a Bun API sidecar and Unix sockets
  for Go API routes, API timeouts and panic protection.
- CSS signals and CSS-only components, legacy browser support, and a docs theme with versioning,
  i18n and customisable search.
