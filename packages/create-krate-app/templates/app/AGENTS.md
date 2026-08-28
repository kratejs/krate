# AGENTS.md

Guidance for AI agents and human contributors working in this Krate app.

__PROJECT_DISPLAY_NAME__ is a [Krate](https://github.com/kratejs/krate) app — a
Go-native static site generator with signal-based reactivity. Pages are TSX/JSX
that compile to static HTML at build time; interactivity is hydrated by a tiny
signal-based runtime (`@krate/runtime`), not React.

## Commands

```bash
npm install      # install deps (@krate/core CLI + @krate/runtime + @krate/components)
npm run dev      # dev server with hot reload (native file watching, no polling)
npm run build    # production build into dist/
npm run serve    # build and serve the production output
```

The `krate` CLI is the Go binary from `@krate/core`; `npm run dev` starts a
native fs watcher and rebuilds on change.

## Project layout

```
krate.config.ts      # Krate config: entry, outDir (dist), pagesDir (src/pages), publicDir
tsconfig.json        # targets ES2022, jsx preserve, @/* -> src/* alias
public/              # static assets served at /
src/
  pages/             # file-based routes: index.tsx -> /, 404.tsx, _layout.tsx shell
  components/        # reusable components + CSS modules
```

- `src/pages/_layout.tsx` wraps every page in a shared shell (nav + footer).
- `src/pages/404.tsx` is the not-found page.
- `@/` maps to `src/` (see `tsconfig.json` paths); use `@/components/...` for imports.
- Treat any file/dir whose name starts with `_` as internal/private (e.g.
  `_layout.tsx`); the watcher and compiler skip these, so don't add routes there.

## Page & component conventions

- Default-export a component from each file in `src/pages/`; the filename becomes
  the route.
- Use the `Head` component for per-page `<head>` content (title, meta).
- Import `createSignal` and friends from `@krate/runtime`; this is NOT React —
  no `useState`, `useEffect`, etc. `onClick={() => setCount(c => c + 1)}` works.
- Use plain `class=` (not `className`).
- CSS modules: import `styles from './X.module.css'` for component-scoped styles;
  global styles live in `public/` (e.g. `global.css`).

## Key runtime APIs

- `createSignal(initial)` -> `[read, write]`; call the read fn to get the value.
- `Head`, `Script`, `Link`, `Image` are provided globally for head/link/assets.

## Gotchas

- `dist/` is build output — never edit, and the dev watcher ignores it plus
  `.krate`, `node_modules`, `.git`, and `_`-prefixed internal files.
- Don't run `krate build`/`dev` from inside `dist/`.
- The compiler runs as a Go binary; runtime/type errors surface as compile
  errors on `dev`/`build`. If TypeScript typechecking is needed beyond what the
  CLI reports, `npx tsc --noEmit` uses the project tsconfig.

## Docs

- Krate docs: https://kratejs.pages.dev/docs/
- Getting started: https://kratejs.pages.dev/docs/getting-started/
