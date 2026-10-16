---
title: TypeScript Setup
order: 11
description: Configure tsconfig for a Krate app and understand the generated types.
---

# TypeScript Setup

Krate compiles TSX itself (a Go lexer/parser), but you still get full editor
types via a standard `tsconfig.json` plus generated declarations.

## tsconfig.json

The scaffold ships a working config. The essentials:

```json
{
  "compilerOptions": {
    "target": "ES2020",
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "jsx": "preserve",
    "jsxImportSource": "@krate/runtime",
    "strict": true,
    "noEmit": true,
    "skipLibCheck": true,
    "esModuleInterop": true,
    "baseUrl": ".",
    "paths": { "@/*": ["./src/*"] }
  },
  "include": ["src", ".krate/types", "*.d.ts"]
}
```

Notes:

- **`jsx: "preserve"`** — Krate's compiler owns JSX transformation; `tsc` is
  only used to typecheck (hence `noEmit`).
- **`paths`** — `@/*` maps to `src/*`. The compiler reads the same `paths` and
  `baseUrl` from `tsconfig.json` for bundling resolution, so they stay in sync.
- **`include`** must cover `.krate/types` so generated route/content types are
  visible.

## Generated types

`krate types` (or any build) writes:

| File | Provides |
|------|----------|
| `.krate/types/routes.d.ts` | `StaticRoute`, `DynamicRoute`, `Route`, `RouteParams` |
| `.krate/types/content.d.ts` | Typed entries for configured collections |
| `.krate/types/**/*.krate.d.ts` | Go component declarations so `import { X } from "./x.go"` type-checks |
| `.krate/gen/content.ts` | Build-time content data (`getCollection`) |

Regenerate after adding pages or content:

```sh
krate types
```

For Go components, run `krate go:init` once — it adds `.krate/types` to your
`tsconfig` `include` (see [Go Components](/docs/features/go-components/)).

See [Typed Routes & Content](/docs/features/typed-routes/) for usage.

## Global JSX types

`@krate/runtime` provides the global `JSX` namespace (intrinsics, ARIA, SVG
attributes, `Krate.TypedRoutes`). Ensure your `tsconfig` resolves it — either
via `jsxImportSource` above or a `/// <reference types="@krate/runtime" />`
in an ambient `.d.ts`.

`src/krate-env.d.ts` (shipped in the scaffold) bridges Krate's globals like
`Head`, `Link`, `Image`, and `Icon`.

## Typechecking in CI

```sh
krate types && npx tsc --noEmit
```

Add this after `krate build`. It catches type errors the compiler tolerates
(Krate drops type annotations rather than validating them).

## Strictness

Keep `strict: true`. Krate's generated types assume it. If you need looser
settings during a migration, scope them per-directory rather than globally.
