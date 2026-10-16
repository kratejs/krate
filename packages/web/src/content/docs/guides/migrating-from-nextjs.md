---
title: Migrating from Next.js
order: 9
description: Move a Next.js App Router or Pages Router site to Krate.
---

# Migrating from Next.js

Krate's pages, routing, and `<Link>` semantics mirror Next.js closely, so most
files need small edits. The biggest conceptual change: **there is no React
runtime** — components are plain functions returning JSX, and state uses
signals.

## Concept mapping

| Next.js | Krate |
|---------|-------|
| `useState` | `createSignal` (`const [v, setV] = createSignal(0)`) |
| `useEffect` | `createEffect` |
| `useMemo` | `createMemo` |
| `useRef` | `ref={el => …}` / `useRef` (krate runtime) |
| `className` | `class` |
| `app/page.tsx` | `src/pages/index.tsx` |
| `app/blog/[slug]/page.tsx` | `src/pages/blog/[slug].tsx` |
| `layout.tsx` | `_layout.tsx` (nearest wins; stacks) |
| `generateStaticParams` | `generateStaticParams` (same shape) |
| `export const revalidate` | `export const revalidate` (ISR) |
| `next/link` | global `<Link>` (or `<a data-krate-link>`) |
| `next/image` | global `<Image>` |
| `next/head` | global `<Head>` |
| API routes `app/api/x/route.ts` | `src/api/x.ts` (JS) or `src/api/x.go` (Go) |

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

4. **Fix attribute names.** `className` → `class`; `htmlFor` → `for`.

5. **Links & images.** Replace `next/link` / `next/image` imports with the
   globals `<Link href="…">` / `<Image src width height alt>`.

6. **Data fetching.** Replace `fetch` in server components with
   `createResource` or build-time reads; see
   [Data Fetching](/docs/features/data-fetching/).

7. **API routes.** Move `app/api/*/route.ts` handlers to `src/api/*.ts`
   exporting `GET`/`POST`/etc. Prefer **Go routes** (`src/api/*.go`) for hot
   endpoints.

8. **Config.** Translate `next.config.js` to `krate.config.ts`
   (`redirects`, `rewrites`, `headers`, `images`, `env`).

9. **Build & check.**

   ```sh
   krate build && krate check
   ```

## Gradual adoption via React interop

React interop is always on. Krate automatically rewrites React hooks
(`useState`, `useEffect`, `useMemo`, `useReducer`, `useRef`, `useCallback`,
`useContext`, `useId`) and `React.*` member calls to signal-based equivalents
during compilation, and auto-calls bare reads (`{count}` → `count()`), so files
deeply tied to React hooks compile without config changes. This is a migration
aid, not a full React runtime — see
[React Compatibility](/docs/guides/react-compatibility/).

## What is intentionally different

- No `getServerSideProps` / `getStaticProps` — use render modes
  (`export const config = { ssr: true }`) and `createResource`.
- No React context; share state via module-level signals or props.
- Streaming uses `<Suspense>` with region rendering (see
  [Rendering](/docs/core-concepts/rendering/)).

## See also

- [Getting Started](/docs/getting-started/)
- [Routing & Layouts](/docs/core-concepts/routing/)
- [Reactivity](/docs/core-concepts/reactivity/)
