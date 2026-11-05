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
- keeps `"use client"` (client boundary inside server component trees) and strips
  default imports from `next/link`, `next/image`, and `next/head` (Krate exposes
  those as globals);
- translates static `next.config.*` redirects, rewrites, headers, `basePath`, and
  image breakpoints/formats/remote patterns into the generated config (also when
  `redirects()`, `rewrites()` or `headers()` is an `async` function that returns a
  literal array), maps static root `metadata` to `site` config, and wraps static,
  synchronous, and async `generateMetadata` results in `<Head>`. A key it cannot
  prove static is reported as manual work on its own; the other keys still
  translate. An `async` page keeps `async` and its wrapper is `async` too, so the
  page renders per request;
- keeps `export const revalidate`, `dynamic` and `dynamicParams` on pages, which
  Krate honors, and notes in the plan what each does (ISR, SSR, 404 for unlisted
  params); the same exports in a layout are manual work;
- rewrites `middleware.ts` to the standard `Request`/`Response` API, including a
  small `NextResponse` shim, static/local matcher checks, and cookie/IP/geo
  helpers; computed `config.matcher` arrays are checked at runtime;
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
- maps `next/font/google` to a generated helper and Google Fonts CSS;
- creates `krate.config.ts` when the project has none, or merges missing
  Next-derived keys into an existing config (existing keys win);
- merges/generates `tsconfig.json` for Krate JSX and route types;
- inlines `NEXT_PUBLIC_*` values from `.env*` and static `next.config.env`;
- leaves the original Next.js tree untouched.

Every apply writes a rollback manifest under
`.krate/migrate/nextjs-<timestamp>/`. Undo with:

```sh
krate migrate rollback .krate/migrate/nextjs-<timestamp>
```

Add `--verify` to build the migrated app, run `krate check`, and diff the built
route inventory against the migration report before you accept the migration:

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
`getServerSideProps`, middleware response rewrites, local font files, async
layouts (move the data fetching into an async child component), `next/cache`
exports without an equivalent (`updateTag`, `cacheLife`, `cacheTag`, `"use cache"`),
route segment config without an equivalent (`runtime`, `preferredRegion`,
`fetchCache`, `maxDuration`), a `default.tsx` outside a parallel-route slot, nested
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
