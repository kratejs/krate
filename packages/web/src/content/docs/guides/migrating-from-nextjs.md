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
`middleware.ts`, `package.json`, ...) and says so at the end: `--apply` then
needs `--force`. The originals are backed up first and `krate migrate rollback`
restores them.

Add `--update-package` to also add `krate` scripts and `@krate/*` dependencies
(pinned to the version of the compiler you run) and `preact` to `package.json`;
`package.json` already exists, so pair it with `--force`. The plan lists each
script it replaces and each dependency it adds or keeps. Next.js dependencies are
left in place until the migrated app builds cleanly.

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
  with its source path. A redirect, rewrite or header rule with `has` or `missing`
  is never translated, because Krate matches on the path only and an
  unconditional copy would apply to every request (a guarded `/store` redirect
  would send `/store` away for everyone); each is reported with its source and
  condition, and the hint points at `middleware.ts`. Identical duplicate rules in
  the output are warned about. When headers could not be translated and the
  config (or a local module it imports) mentions `Content-Security-Policy`,
  `Strict-Transport-Security`, `X-Frame-Options` or similar, a separate item says
  the security headers were not migrated. `krate.config.ts` can import a local
  module, so a helper that builds static headers can be imported there; values
  that depend on the request, such as a CSP nonce, belong in `middleware.ts`.
  Every other `next.config` key gets its own item with a hint instead of one
  generic warning: `transpilePackages`, `output: 'standalone'`, computed
  `images.remotePatterns` or `env`, `experimental` flags, and a
  `loadEnvConfig(...)` call (point `envDir` at the directory that holds the
  `.env*` files). A static `serverExternalPackages` list and static
  `outputFileTracingIncludes` globs are carried over to the Krate keys of the
  same name (see [Self-hosting](/docs/guides/self-hosting/)); computed ones get an item.
  An `async` page keeps `async` and its wrapper is `async` too, so the
  page renders per request;
- keeps `export const revalidate`, `dynamic` and `dynamicParams` on pages, which
  Krate honors, and notes in the plan what each does (ISR, SSR, 404 for unlisted
  params); the same exports in a layout are manual work;
- rewrites `middleware.ts` to the standard `Request`/`Response` API, including a
  small `NextResponse` shim, static/local matcher checks, and cookie/IP/geo
  helpers; computed `config.matcher` arrays are checked at runtime. The Next.js 16
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
- flags inline `"use server"` functions (a directive inside a function body, the
  Auth.js `signOut` pattern) as manual work, because the build rejects them with
  `KR7001`; move each into a module whose first statement is `"use server"`;
- strips root-layout `<html>`/`<body>` wrappers;
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
  (`dashboard/index.tsx`) so those files wrap it;
- maps `next/navigation` onto the runtime: `usePathname`, `useParams`,
  `useSearchParams`, `useRouter` and `useSelectedLayoutSegment(s)` come from
  `@krate/runtime` (see [Router hooks](/docs/reference/runtime-api/#router-hooks)),
  and `notFound`, `redirect`, `permanentRedirect`, `forbidden` and `unauthorized`
  from `@krate/runtime/server` (from `@krate/runtime` in a `"use client"` file).
  No helper file is generated. Names with no equivalent (`RedirectType`,
  `unstable_rethrow`, `useServerInsertedHTML`) stay on `next/navigation` and are
  listed as manual work. `next/headers` and `next/cache` map to
  `@krate/runtime/server`, rewrites common `next/server` API helpers, and maps
  simple `next/dynamic` loaders to React `lazy()`;
- keeps `NextRequest` features in route handlers: the `Request` has
  `request.cookies` (`get`, `getAll`, `has`, read-only) and `request.nextUrl`
  (a `URL` with `searchParams`, `pathname`, `basePath` and `clone()`; the
  pathname never includes the base path), and the handler is typed as
  `KrateRequest` from `@krate/runtime/server`. `NextResponse.json` and
  `NextResponse.redirect` come from a small generated shim whose responses carry
  `cookies.set/delete/get/getAll/has`. `after()` from `next/server` becomes
  `after` from `@krate/runtime/server` and runs once the response is sent (see
  [API routes](/docs/features/api-routes/#after-the-response)). Only what is
  left is manual work: `request.cookies.set/delete`, `request.ip`,
  `request.geo`, `NextResponse.rewrite/next`, and other `next/server` helpers
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
- drops route segment config that only steers Vercel (`runtime`, `maxDuration`,
  `preferredRegion`) with an info note per file and one aggregated warning, since
  Krate self-hosts on Node. Krate has no per-route time limit: the API proxy
  fails a handler that sends no headers within 30 seconds, so a `maxDuration`
  above 30 is called out. `fetchCache` is kept and reported as one aggregated
  manual item (set the cache option on the `fetch` calls that relied on it);
- creates `krate.config.ts` when the project has none, or merges missing
  Next-derived keys into an existing config (existing keys win);
- merges/generates `tsconfig.json` for Krate JSX and route types;
- renames reads of `process.env.NEXT_PUBLIC_NAME` (and the keys of static
  `next.config` `env`) to `process.env.KRATE_PUBLIC_NAME`, the prefix Krate
  inlines at build time. Values are never copied from `.env*` into the migrated
  code, so development URLs and keys cannot ship by accident. One manual item
  lists every variable to rename or set for the Krate build and which `.env`
  files define it;
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
- warns about script files at the project root and directly under `src/` that
  the plan neither copies nor rewrites (`auth.ts`, a stray `proxy.mjs`, ...), skipping
  `*.config.*`, `*.d.ts` and test files, so nothing is dropped without a trace;
- lists `instrumentation.ts`, `instrumentation-*.ts` as manual work: Krate has no
  server startup hook that calls `register()`.

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
imports) resolves from the project, and stops with a message if not: run with
`--update-package`, install the dependencies, then re-run.

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
`--update-package` adds it, otherwise install `preact`.

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
