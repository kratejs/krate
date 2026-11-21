---
title: Migrating from Next.js
order: 12
description: Move a Next.js App Router or Pages Router site to Krate.
---

# Migrating from Next.js

Krate's pages, routing, and `<Link>` semantics mirror Next.js closely, so most
files need small edits. The biggest conceptual change: **there is no React
runtime** - components are plain functions returning JSX, and state uses
signals.

## Concept mapping

| Next.js | Krate |
|---------|-------|
| `useState` | `createSignal` (`const [v, setV] = createSignal(0)`) |
| `useEffect` | `createEffect` |
| `useMemo` | `createMemo` |
| `useRef` | `ref={el => ...}` / `useRef` (krate runtime) |
| `className` | `class` |
| `app/page.tsx` | `src/pages/index.tsx` |
| `app/blog/[slug]/page.tsx` | `src/pages/blog/[slug].tsx` |
| `layout.tsx` | `_layout.tsx` (nearest wins; stacks) |
| `generateStaticParams` | `generateStaticParams` (same shape) |
| `export const revalidate` / `dynamic` | `export const revalidate` / `dynamic` (ISR, SSR) |
| `async` server components, `await fetch()` | `async` components render per request on the SSR sidecar; see [Server Components](/docs/features/server-components/) |
| `fetch(url, { next: { tags, revalidate } })`, `unstable_cache` | same, cached and tagged by the runtime; see [Caching and Revalidation](/docs/guides/caching-and-revalidation/) |
| `revalidatePath` / `revalidateTag` (`next/cache`) | same names from `@krate/runtime/server` |
| `"use client"` | kept: a client boundary inside server component trees |
| `template.tsx` | `_template.tsx` (remounts on every navigation) |
| `(group)`, `@slot`, `default.tsx`, `(.)x` | same directory names under `src/pages`; see [Routing](/docs/core-concepts/routing/) |
| `next/link` | global `<Link>` (or `<a data-krate-link>`) |
| `next/navigation` (`usePathname`, `useParams`, `useSearchParams`, `useRouter`, `useSelectedLayoutSegment(s)`) | the same hooks from `@krate/runtime` |
| `notFound()`, `forbidden()`, `unauthorized()`, `redirect()` | the same names from `@krate/runtime/server`; `forbidden.tsx` / `unauthorized.tsx` become `403.tsx` / `401.tsx` |
| `next/image` | global `<Image>` |
| `next/head` | global `<Head>` |
| API routes `app/api/x/route.ts` | `src/api/x.ts` (JS) or `src/api/x.go` (Go) |
| `"use server"` actions | `"use server"` module; see [Server Actions](/docs/guides/server-actions/) |

## Use the migration CLI

Start with a dry run from the Next.js project root:

```sh
krate migrate nextjs
```

The planner detects App Router (`app/`) and Pages Router (`pages/`) files and
prints every copy plus every item it will not rewrite. Apply it with:

```sh
krate migrate nextjs --apply
krate migrate nextjs --report migration-report.json
```

The dry run marks every file that already exists (`tsconfig.json`,
`middleware.ts`, ...) and says so at the end: `--apply` then needs `--force`.
The originals are backed up first and `krate migrate rollback` restores them.

The migration also updates `package.json` (it is backed up like every other
file, so it never needs `--force`; pass `--no-update-package` to leave it alone,
and `--update-package` is still accepted). The plan lists each change:

- `dev` becomes `krate dev`; `build` and `start` become the commands for the
  deploy target (`krate build` and `krate start`, or `krate deploy` and
  `node dist/.krate/server/.../server.mjs` for `output: 'standalone'`). Other
  scripts keep their text with `next <command>` rewritten the same way:
  `next typegen` becomes `krate types`, and a `next lint` or other command Krate has no
  equivalent for stays, with an info line, and keeps `next` installed.
- Every `@krate/*` package the migrated code imports is added (`@krate/core`,
  `@krate/runtime`, `@krate/components`, and `@krate/auth`, `@krate/three` or
  `@krate/konva` when the code now uses them), plus `preact`. A release of the
  compiler writes `^<version>` of each package, taken from a table generated from
  the packages (`@krate/three` and `@krate/konva` version on their own). A
  development build of the compiler writes `link:` ranges to the packages of the
  Krate checkout it runs from (found from `KRATE_BINARY` or the project's
  `@krate/core`) and `latest`, with an info line, when there is none.
- `next`, `next-auth` and `@next/env` are removed when no file left in the
  project imports them. When one does (a test file, a script), the dependency
  stays and the info line names the files. `react` and `react-dom` stay: the
  Preact islands alias them.

Installing is the one thing left to you: the migration prints the install
command of your package manager (for pnpm, in the workspace root that holds
`pnpm-workspace.yaml`). It never runs a network install or edits a lockfile.

The first pass:

- copies `app/**/page.*`, `pages/**/*`, layouts, templates, `loading` files,
  `not-found` and `error` files, and API handlers into `src/pages/` and
  `src/api/`, keeping
  route groups, `@slot` directories, slot `default.tsx` files and intercepting
  `(.)x` segments in place (the Krate file router understands them);
- copies Pages Router `_app` and wraps it as the root `_layout.tsx` shell;
- copies app-local relative imports under `src/migrated/` and rewrites the
  migrated route imports to point at them;
- follows imports from the migrated files to project modules outside `app/`
  (relative imports and tsconfig `paths` / `baseUrl` aliases such as `@/*`,
  including `extends`) and applies the same Next import rewrites to them
  (`next/link`, `next/navigation`, `next/cache`, `next/headers`, `next/dynamic`,
  `next/server`, `Metadata` from `next`). These modules stay where they are and
  are rewritten **in place**, so the plan lists them with `target` equal to
  `source`; each is backed up first and `krate migrate rollback` restores it.
  Because they overwrite your source, `--apply` needs `--force` when any are
  planned. Workspace packages that import `next/*` are outside the project and
  are listed as manual items;
- keeps `"use client"` (client boundary inside server component trees) and strips
  default imports from `next/link`, `next/image`, and `next/head` (Krate exposes
  those as globals);
- translates static `next.config.*` redirects, rewrites, headers, `basePath`, and
  image breakpoints/formats/remote patterns into the generated config (also when
  `redirects()`, `rewrites()` or `headers()` is an `async` function that returns a
  literal array), maps static root `metadata` to `site` config, and wraps static,
  synchronous, and async `generateMetadata` results in `<Head>`. A key it cannot
  prove static is reported as manual work on its own; the other keys still
  translate, and so does each rule: in a `headers()` array, the static entries
  translate while a computed one (a helper call, `value: compute()`) is reported
  with its source path. `has` and `missing` conditions
  (query, header, cookie, host) translate to the same conditions in the Krate
  config, and a redirect is written with `statusCode: 308` for `permanent: true`
  and `307` for `false`, the statuses Next sends. Next sources keep their syntax,
  including a leading negative lookahead in a parameter such as
  `/store/:slug((?!compare$)[^/]+)`. A redirect whose regex Go's RE2 engine
  cannot run (lookbehind, a backreference) is written as a marked block
  (`// krate migrate: next.config redirects[i]`) at the start of the middleware
  function, creating `middleware.ts` when there is none, and an info line says
  so; a rewrite or header rule like that, or a middleware whose signature cannot
  be edited safely, is reported as manual work. Identical duplicate rules in
  the output are warned about. Header rules and `images.remotePatterns`
  entries that are built in code are carried over as code: a `headers()` that
  returns `[{ source: '/:path*', headers: securityHeaders }, ...buildCacheRules()]`,
  where `securityHeaders` is a top-level const computed by an imported helper,
  and a `remotePatterns` list that an `if (process.env.X) { ...push(...) }`
  extends. The migration copies next.config's top-level imports of local modules,
  the consts the carried code depends on and the statements that change them into
  `krate.config.ts`, and leaves out type-only imports, `import ... from 'next'`
  and the `loadEnvConfig` call with its `@next/env` import (it is mapped to
  `envDir`). The file stays plain JSON-shaped when every value is static. After
  writing the plan the migration evaluates the new `krate.config.ts` with the real
  config loader and your environment (the shell plus the `.env` files, as a
  production build loads them). A rule that fails there, for example because a
  helper imports `next/server`, is written as a comment with the error and a
  manual item quotes it. The values are computed again each time Krate loads the
  config, so `krate build`, `krate start` and `krate dev` each use their own
  environment; `krate deploy --target node` bakes the build-time result, as Next
  bakes `headers()` into its routes manifest. When the headers set security
  headers but no `Strict-Transport-Security`, an info line notes that Krate also
  sends HSTS for an https `baseUrl`; set `security.hsts: "off"` to match Next
  exactly. `images.remotePatterns` keeps `protocol`, `port` and `search`.
  Values that depend on the request, such as a CSP nonce, belong in
  `middleware.ts`. Every other `next.config` key gets its own item with a hint
  instead of one generic warning: computed `env`, `experimental` flags, and a
  `loadEnvConfig(...)` call whose path is not static. When the path is static
  (a literal, `path.resolve(__dirname, '../..')`, `join(process.cwd(), '..', '..')`,
  `fileURLToPath(new URL('../..', import.meta.url))`), the migration writes the
  matching `envDir` and there is no item. A static `serverExternalPackages` list and static
  `outputFileTracingIncludes` globs are carried over to the Krate keys of the
  same name (see [Self-hosting](/docs/guides/self-hosting/)); computed ones get an item.
  `output: 'standalone'` becomes `deploy: { target: "node" }`; in the package update
  the `build` script becomes `krate deploy` (it builds first, then bundles the
  server) and `start` becomes `node dist/.krate/server/server.mjs`. A static
  `outputFileTracingRoot` (`path.resolve(import.meta.dirname, '../..')` and the
  `__dirname`, `process.cwd()` and `fileURLToPath` forms) becomes the
  `outputFileTracingRoot` key, `start` points at
  `dist/.krate/server/<project below the root>/server.mjs`, and
  `outputFileTracingIncludes` globs that leave the project are carried over
  verbatim when the root contains them (the route keys stay, but the node
  output copies the union). A monorepo Docker image is an info line: build with
  the node target and write the Dockerfile yourself.
  `transpilePackages` is checked against the installed packages: when each one
  resolves, to workspace source or a compiled package, the key is dropped with
  an info line, because Krate needs no such setting. A package that does not
  resolve gets a manual item that says whether it is not installed or its
  `main`/`exports` point at a file that does not exist (build it first).
  `turbopack.root` is dropped with an info line when it is the project, the
  tracing root or the `envDir` root; `turbopack.resolveAlias` gets a hint to add
  the aliases to tsconfig `paths`; `turbopack.rules` (webpack loaders) stays a
  manual item.
  An `async` page keeps `async` and its wrapper is `async` too, so the
  page renders per request;
- keeps `export const revalidate`, `dynamic` and `dynamicParams` on pages, which
  Krate honors, and notes in the plan what each does (ISR, SSR, 404 for unlisted
  params); the same exports in a layout are manual work;
- rewrites `middleware.ts` to the standard `Request`/`Response` API, including a
  `NextResponse` class (a `Response` subclass: `new NextResponse(body, init)`,
  `redirect`, `json`, `next`, `rewrite`, `cookies`; `next({ request: { headers } })`
  reaches layouts through `headers()`), static/local matcher checks, and
  cookie/IP/geo helpers; computed `config.matcher` arrays are checked at runtime. The Next.js 16
  `proxy.ts` (root or `src/`, a `proxy` function or a default export) is migrated
  the same way and written to `middleware.ts`, the name Krate loads (`--apply`
  moves the original `proxy.ts` into the backup); when both files exist only
  `proxy.ts` is migrated and the other is a manual item;
- copies static `favicon.ico`, `icon.*` and `apple-icon.*` files from the app root
  to `public/` and links the root ones from the generated `head` config
  (`<link rel="icon">`, `<link rel="apple-touch-icon">`). Icons inside a
  segment are copied but need their `<link>` added by hand. Dynamic `icon.tsx`
  and `apple-icon.tsx` are manual items;
- keeps the public URL of route handlers outside `app/api`: they move under
  `/api/`, and the plan adds a `rewrites` entry from the old path (`/r/[code]`
  becomes `/r/:code` to `/api/r/:code`; catch-alls use `:path+` and `:path*`).
  `krate serve` and `krate dev` answer such a rewrite from the API handler. A
  handler at the app root (`app/route.ts`) is a manual item, because a rewrite
  for `/` would shadow the home page. Deploy targets that do not run the Krate
  server may not apply the rewrite;
- leaves inline `"use server"` functions (a directive inside a function body, the
  Auth.js `signOut` pattern) as they are: the build hoists them into server
  actions, sealing the values they close over (see
  [Server Actions](/docs/guides/server-actions/#inline-actions)). One inside a
  `"use client"` module is flagged as manual work, because it fails the build
  with `KR7001` there as it does in Next.js;
- strips root-layout `<html>`/`<body>` wrappers and carries their attributes (`lang`,
  `className`, `data-*`, ...) into a `<Head>` with `<html>` and `<body>` elements, which
  set the attributes of the document elements (see
  [Document attributes](/docs/features/built-in-components/#document-attributes));
  `suppressHydrationWarning` is dropped;
- maps `forbidden.tsx` to `403.tsx` and `unauthorized.tsx` to `401.tsx` (the
  Next.js 15.1 `authInterrupts` pages), and drops the `experimental.authInterrupts`
  flag, because `forbidden()` and `unauthorized()` always work;
- maps `not-found.tsx` to `404.tsx` and `error.tsx` to `error.tsx` in the same
  directory (route groups kept), where they are native
  [segment pages](/docs/core-concepts/routing/#not-found-and-error-pages): the
  nearest not-found page answers `notFound()` and unmatched URLs inside the
  layouts, and `error.tsx` is the segment's error page and client error boundary
  with `reset`. `global-error.tsx` becomes the root `error.tsx` (remove the
  `<html>` and `<body>` it renders; Krate's root layout provides them), or a
  manual item when a root `error.tsx` exists too. A directory that has a layout,
  template, loading, error or not-found file keeps its page in that directory
  (`dashboard/index.tsx`) so those files wrap it. A route segment named like a
  Krate special file (`error`, `loading`, `not-found`, `layout`, `template`,
  `404`, `500`, `403`, `401`, `forbidden`, `unauthorized`, `index`) also keeps its
  page in the directory (`auth/error/index.tsx`), so `/auth/error` stays a page;
- maps `next/navigation` onto the runtime: `usePathname`, `useParams`,
  `useSearchParams`, `useRouter` and `useSelectedLayoutSegment(s)` come from
  `@krate/runtime` (see [Router hooks](/docs/reference/runtime-api/#router-hooks)),
  and `notFound`, `redirect`, `permanentRedirect`, `forbidden` and `unauthorized`
  from `@krate/runtime/server` (from `@krate/runtime` in a `"use client"` file).
  No helper file is generated. Names with no equivalent (`RedirectType`,
  `unstable_rethrow`, `useServerInsertedHTML`) stay on `next/navigation` and are
  listed as manual work. `next/headers` and `next/cache` map to
  `@krate/runtime/server`, rewrites common `next/server` API helpers, and maps
  `next/dynamic` loaders to React `lazy()`. Next accepts a loader that resolves
  to a module or straight to a component, but `lazy()` needs `{ default }`, so
  `.then((m) => m.Pane)` becomes `.then((m) => ({ default: m.Pane }))`,
  `.then(() => Pane)` becomes `.then(() => ({ default: Pane }))`, and any other
  loader that post-processes the import is wrapped in a small `asLazyModule`
  helper added to the file. A `dynamic(() => import('./x'),
  { ssr: false, loading })` call (with or without a type argument,
  `dynamic<Props>(...)`) becomes a module-level `lazy()` plus a component that
  renders the `loading` fallback until it has mounted in the browser, then the
  lazy component inside a `<Suspense>` with the same fallback (shown while the
  chunk loads, as in Next.js): the page HTML holds the fallback, the component lives in its
  own `lazy-*.js` chunk and the build still renders it once, hidden, so a viewer
  that touches `window` while rendering needs a real-React island (see
  [React compatibility](/docs/guides/react-compatibility/#libraries-built-on-a-react-reconciler-real-react-islands))
  or a guard. A `loading` that is not an arrow function returning JSX or a
  component name, and `ssr: false` outside a top-level `const`, are manual items;
- moves `@react-three/fiber` and `@react-three/drei` code to
  [`@krate/three`](/docs/features/three/) when every fiber and drei API the
  project's modules use is provided (drei's `OrbitControls`, `Environment` with
  `Lightformer` children or a `map`, and `useGLTF` come from `@krate/three/drei`),
  and adds `@krate/three` to `package.json` (a manual item with
  `--no-update-package`). The scene then runs on Krate, or on the Preact island of a
  `"use client"` component, instead of a real-React island that ships React,
  react-dom and a second copy of the React libraries around the canvas. One
  unsupported API (another drei helper, `<Environment preset>`, fiber's
  `reconciler`, a package built on fiber such as `@react-spring/three`) keeps
  every fiber module as it is, because they render each other's components in
  one canvas, and each blocking use is listed as a manual item. Keep
  `@react-three/fiber` and drei installed while a real-React island renders one
  of the 3D components: there the build runs `@krate/three` as fiber;
- moves `react-konva` code to [`@krate/konva`](/docs/features/konva/) when every
  react-konva API the project's modules use is provided (Stage, the layers and
  shapes, Transformer, `useStrictMode`, `useContextBridge`), and adds
  `@krate/konva` to `package.json` (a manual item with `--no-update-package`).
  Type imports from `konva` stay as they are. The canvas then runs on Krate, or
  on the Preact island of a `"use client"` component, instead of a real-React
  island. `KonvaRenderer`, an internal `react-konva/...` subpath or a package
  built on react-konva (such as `react-konva-utils`) keeps every react-konva
  module as it is, and each blocking use is listed as a manual item;
- converts `next/script`: `<Script strategy="afterInteractive">` (the default)
  becomes a Krate `<Script defer>`, `lazyOnload` an `async` one and
  `beforeInteractive` a `<script>` inside `<Head>`; `src`, `id`, `data-*`
  attributes and inline children are kept. `strategy="worker"` and computed
  strategies are loaded deferred on the main thread and listed as manual items,
  as are `onLoad`, `onReady` and `onError` handlers, which a static `<script>`
  cannot run. `after()` from `next/server` becomes `after` from
  `@krate/runtime/server` in pages and layouts as well as route handlers;
- writes `trailingSlash: "never"` into `krate.config.ts` (Next's URL form: `/store`
  answers 200, `/store/` redirects), or `"always"` when `next.config` sets
  `trailingSlash: true`, and `tailwind: { enabled: true }` when `postcss.config.*`
  uses `@tailwindcss/postcss` or `tailwindcss`, and `compress: { brotli: true,
  gzip: true }` (Next.js compresses responses unless `next.config` sets
  `compress: false`): `krate build` writes `.br` and best-ratio `.gz` siblings
  of the assets, which `krate start` sends, instead of gzipping on the fly;
- writes `viewTransitions: "off"` into `krate.config.ts`, unless `next.config`
  sets `experimental: { viewTransition: true }`: Next.js changes pages without
  a View Transition, and Krate wraps every client navigation in one by
  default. A transition snapshots the page, and a `backdrop-filter` surface
  (frosted glass) can flash darker or lighter for a frame while it runs. An app
  migrated before this, which has no `viewTransitions` key, can add the line;
- keeps `NextRequest` features in route handlers: the `Request` has
  `request.cookies` (`get`, `getAll`, `has`, read-only) and `request.nextUrl`
  (a `URL` with `searchParams`, `pathname`, `basePath` and `clone()`; the
  pathname never includes the base path), and the handler is typed as
  `KrateRequest` from `@krate/runtime/server`. `NextResponse` comes from a small
  generated shim: a `Response` subclass (`new NextResponse(body, init)`,
  `NextResponse.json`, `NextResponse.redirect`) whose responses carry
  `cookies.set/delete/get/getAll/has`. `after()` from `next/server` becomes
  `after` from `@krate/runtime/server` and runs once the response is sent (see
  [API routes](/docs/features/api-routes/#after-the-response)). Only what is
  left is manual work: `request.cookies.set/delete`, `request.ip`,
  `request.geo`, `NextResponse.rewrite/next` in route handlers, and other `next/server` helpers
  such as `userAgent`;
- maps `next/font/google` to a generated helper and Google Fonts CSS, and
  `next/font/local` plus `geist/font/sans` and `geist/font/mono` to self-hosted
  `@font-face` CSS: each `localFont({ src, weight, style, variable, display,
  declarations, fallback })` call (a single `src` or an array of `{ path, weight,
  style }`, all literals) is replaced by an inline `{ className, variable, style }`
  object, the font files are copied to `public/migrated-fonts/`, and the rules go
  to `src/migrated/next-fonts.css`, which the file imports. `GeistSans` and
  `GeistMono` use the variable font files shipped in the installed `geist`
  package. `adjustFontFallback` and `preload` are ignored (Krate does not
  generate size-adjusted fallbacks). A call with a computed option or an
  imported `src` stays on `next/font/local` and is reported as manual work;
  the other Geist fonts (`pixel`, `serif`, `*-non-variable`) are not mapped;
- keeps `export const maxDuration = N` in API routes: Krate reads it as the
  route's request timeout in seconds (default 30), in every runtime and deploy
  target (see [Timeouts](/docs/features/api-routes/#timeouts)). It must be a
  literal number; a computed value is reported as manual work. On a page it has
  no meaning and is dropped;
- keeps `export const runtime` in API routes: `'nodejs'` serves the route from
  Node and `'edge'` keeps it on the embedded runtime. Without it, a route runs
  on the embedded runtime unless its code (or a helper it imports) uses a Node
  built-in, a native package or a Node-only global such as `Buffer`; then it
  runs on Node automatically. `runtime` on a page and `preferredRegion` only
  steer Vercel, so they are dropped with an info note per file and one
  aggregated warning. `fetchCache` is kept and reported as one aggregated
  manual item (set the cache option on the `fetch` calls that relied on it);
- creates `krate.config.ts` when the project has none, or merges missing
  Next-derived keys into an existing config (existing keys win);
- merges/generates `tsconfig.json` for Krate JSX and route types;
- keeps reads of `process.env.NEXT_PUBLIC_NAME` as they are and writes
  `envPrefix: ["KRATE_PUBLIC_", "NEXT_PUBLIC_"]` into `krate.config.ts`, so Krate
  inlines them from the same `.env` files and build environment Next did. The
  migration looks for their definitions next to the app, in the `envDir` that
  `loadEnvConfig` points at and in `.env.example`, and prints one info line
  (`defined in ../../.env.example: 4 of 4`); names it cannot find are listed there
  and must come from the build environment. Variables that only a static
  `next.config` `env` creates are renamed to `KRATE_PUBLIC_NAME`, and one manual
  item lists them. Values are never copied from `.env*` into the migrated code, so
  development URLs and keys cannot ship by accident;
- moves the original Next.js route trees (`app/` or `src/app/`, and `pages/` when
  it was migrated) into the backup directory on `--apply`, after every file is
  written, so Krate does not compile them next to the migrated copies. The plan
  says which trees move, and `krate migrate rollback` moves them back. Files in
  those trees that the migrator does not copy (a README, a `.txt` file) go to the
  backup with the tree and are listed as a warning. A tree stays in place when a
  migrated file is written inside it. Copied test files (`*.test.*`, `*.spec.*`,
  `__tests__`) are kept in `src/migrated/` next to their modules, and their
  relative imports of migrated routes (`./page`) are re-pointed at
  `src/pages/`; with the originals moved aside each test runs once;
- ports next-auth v5 to [`@krate/auth`](/docs/features/authentication/#coming-from-nextauth)
  when it finds a `NextAuth({...})` config (an object, or a function returning one): the
  import becomes `defineAuth` from `@krate/auth`, `next-auth/providers/*` imports become the
  matching `@krate/auth` providers, `next-auth/react` becomes `@krate/auth/client`, and
  `cookies: { prefix: "authjs" }` is added so the existing `authjs.*` cookies and database
  sessions keep working. `handlers`, `auth`, `signIn` and `signOut` keep their names, so
  the `[...nextauth]` route, server actions and server components need no edits, and
  `@auth/drizzle-adapter` is kept as is. The package update adds `@krate/auth` and removes `next-auth` once nothing imports it. A provider,
  option, callback, cookie override, `getToken`, `useSession` or `auth(handler)` wrapper
  with no `@krate/auth` equivalent is listed as manual work with what to do instead, and
  next-auth is never left to fail at run time as an unresolved import;
- converts a `declare module 'next-auth'` augmentation of `Session` (`user: { ... } & DefaultSession['user']`)
  or `User` into `declare module '@krate/auth'` with `SessionUserExtensions` or
  `UserExtensions` (see [Typing extra session fields](/docs/features/authentication/#typing-extra-session-fields)).
  The file is written as `krate-auth.d.ts` next to the original, which moves into the backup;
  imports of your own types are kept and `DefaultSession` imports are dropped. Any other shape
  (a `next-auth/jwt` module, a conditional type) is a manual item that quotes the declaration;
- leaves a stub at the old path of a file that another workspace package references from outside the
  project. A stylesheet that `packages/static-site/src/styles.css` imports as `../../web/src/app/tokens.css`
  keeps its path as a file containing `@import` of the moved copy (Krate's CSS pipeline inlines nested
  imports); a script or module becomes `export * from` the moved file (plus its default export). A file of
  another type, such as an image, stays as a copy at the old path and is a manual item that names it. The
  stubs are recorded in the backup manifest and `krate migrate rollback` removes them;
- warns about script files at the project root and directly under `src/` that
  the plan neither copies nor rewrites (`auth.ts`, a stray `proxy.mjs`, ...), skipping
  `*.config.*`, `*.d.ts` and test files, so nothing is dropped without a trace;
- keeps `instrumentation.ts` and `instrumentation-*.ts` where they are: Krate calls
  `register()` once per server start (see
  [Instrumentation](/docs/guides/self-hosting/#instrumentation)). The migrator folds
  `process.env.NEXT_RUNTIME === "nodejs"` to `true` and `=== "edge"` to `false` in those
  files, and lists only `onRequestError` (not called by Krate) and
  `instrumentation-client` as manual work. A `loadEnvConfig(...)` call keeps working
  and is harmless next to `envDir`. `register()` runs in the API sidecar's primary
  process only, so state it puts on `globalThis` is not visible to cluster workers.

Every apply writes a rollback manifest under
`.krate/migrate/nextjs-<timestamp>/`. Undo with:

```sh
krate migrate rollback .krate/migrate/nextjs-<timestamp>
```

Add `--verify` to build the migrated app, run `krate check`, and diff the built
route inventory against the migration report before you accept the migration.
Verification evaluates `krate.config.ts`, which imports `@krate/core`, so the
packages must be installed first. Before it writes anything, `--apply --verify`
checks that `@krate/core` (and any other `@krate/*` package a generated file
imports) resolves from the project, and stops with a message if not: install the dependencies
the plan adds to `package.json`, then re-run.

```sh
krate migrate nextjs --apply --verify
```

Verification fails on a build error, failing quality gate, missing route, missing
API target, or extra generated route. It leaves the backup in place so you can
inspect the diff and roll back. The reference-app suite runs the same check for
App Router, Pages Router, and client-island/dynamic fixtures; the complex App
Router fixture also runs the browser conformance suite.

The migrator intentionally reports manual work instead of guessing:
async/class page defaults with `generateMetadata`, `getStaticProps`/
`getServerSideProps`, middleware response rewrites, local font files with computed options, async
layouts (move the data fetching into an async child component), `next/cache`
exports without an equivalent (`updateTag`, `cacheLife`, `cacheTag`, `"use cache"`),
`fetchCache` route segment config, a `default.tsx` outside a parallel-route slot, nested
`not-found`, unresolved relative imports, and function/plugin
`next.config.*` files (or keys of one) are not rewritten automatically. Resolve those before
running `krate build`. `useSelectedLayoutSegment(s)` count from the layout whose
file calls them; a hook called from a shared component counts from the root.

Client components rendered inside async server components hydrate with Preact:
the package update adds it (with `--no-update-package`, install `preact`).

## Steps

1. **Scaffold & install**

   ```sh
   npx create-krate-app@latest my-site
   cd my-site && npm install
   ```

2. **Move pages.** Copy `app/**/page.tsx` (or `pages/**`) into `src/pages/`,
   renaming `index` and stripping the `page` filename:

   ```
   app/page.tsx                  -> src/pages/index.tsx
   app/about/page.tsx            -> src/pages/about.tsx
   app/blog/[slug]/page.tsx      -> src/pages/blog/[slug].tsx
   ```

   Wrap `app/layout.tsx` content in `src/pages/_layout.tsx`.

3. **Convert state.** Replace hooks:

   ```tsx
   // Next.js
   const [n, setN] = useState(0);
   // Krate
   const [n, setN] = createSignal(0);
   // read by calling: n()  (not n)
   ```

4. **Fix attribute names.** `className` -> `class`; `htmlFor` -> `for`.

5. **Links & images.** Replace `next/link` / `next/image` imports with the
   globals `<Link href="...">` / `<Image src width height alt>`.

6. **Data fetching.** `async` server components that `await fetch()` work as
   they do in Next.js (they render per request); tag and cache the reads with
   `fetch(url, { next: { tags, revalidate } })` and invalidate them from server
   actions with `revalidateTag` / `revalidatePath`. For client-side data use
   `createResource`; see [Data Fetching](/docs/features/data-fetching/).

7. **Server actions & API routes.** Move `"use server"` functions to action modules (see [Server Actions](/docs/guides/server-actions/)). Move `app/api/*/route.ts` handlers to `src/api/*.ts`
   exporting `GET`/`POST`/etc. Prefer **Go routes** (`src/api/*.go`) for hot
   endpoints.

8. **Config.** `krate migrate nextjs` translates static `redirects`,
   `rewrites`, `headers`, `basePath`, and common `images` settings from
   `next.config.*` into the generated `krate.config.ts`. Review custom image
   loaders, `env`, experimental keys, and computed/plugin configs manually.

9. **Build & check.**

   ```sh
   krate build && krate check
   ```

## Gradual adoption via React interop

React interop is always on. Krate automatically rewrites React hooks
(`useState`, `useEffect`, `useMemo`, `useReducer`, `useRef`, `useCallback`,
`useContext`, `useId`) and `React.*` member calls to signal-based equivalents
during compilation, and auto-calls bare reads (`{count}` -> `count()`), so files
deeply tied to React hooks compile without config changes. This is a migration
aid, not a full React runtime - see
[React Compatibility](/docs/guides/react-compatibility/).

## What is intentionally different

- No `getServerSideProps` / `getStaticProps` - use render modes
  (`export const config = { ssr: true }`, or Next-style `export const dynamic` /
  `revalidate`) and async server components.
- Catch-all params (`[...slug]`) are a `/`-joined string in the page's `params`
  prop, not `string[]`. `useParams()` returns the array, like Next.js.
- `usePathname()` never has a trailing slash (`/` stays), whatever URL the
  server canonicalizes to.
- Layouts are built once and cannot call `forbidden()` or `unauthorized()` (see
  [Forbidden and unauthorized](/docs/core-concepts/routing/#forbidden-and-unauthorized)).
- React context is supported through React interop; module-level signals and
  props are still the lowest-overhead option.
- Streaming uses `<Suspense>` with region rendering (see
  [Rendering](/docs/core-concepts/rendering/)).

## See also

- [Getting Started](/docs/getting-started/)
- [Routing & Layouts](/docs/core-concepts/routing/)
- [Reactivity](/docs/core-concepts/reactivity/)

## Deploying a migrated app

A migrated app's server features (async server components, route handlers,
server actions, `middleware`, ISR) deploy to Vercel, Netlify, Cloudflare or a
plain Node server with `krate deploy --target vercel|netlify|cloudflare|node`.
Differences from Next.js on those hosts: caches are per instance (`revalidatePath`
and `revalidateTag` reach only the instance that handled the request and report
the rest under `skipped`), Vercel and Netlify serve prerendered pages from the
CDN before middleware runs, and Cloudflare builds fail at bundle time on Node
built-ins that Workers lack. See [Deployment](/docs/guides/deployment/).
