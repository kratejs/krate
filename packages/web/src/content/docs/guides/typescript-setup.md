---
title: TypeScript Setup
order: 14
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

- **`jsx: "preserve"`** - Krate's compiler owns JSX transformation; `tsc` is
  only used to typecheck (hence `noEmit`).
- **`paths`** - `@/*` maps to `src/*`. The compiler reads the same `paths` and
  `baseUrl` from `tsconfig.json` for bundling resolution, so they stay in sync.
  `tsconfig.json` may contain comments and trailing commas, and `extends`
  (a relative path or a package) is followed. Targets in `paths` resolve
  relative to `baseUrl` exactly as `tsc` does, so with `baseUrl: "."` write
  `"@/*": ["./src/*"]`; an import that does not resolve fails the build at the
  import statement. A tsconfig that cannot be parsed is reported as a warning.
  With `baseUrl` set, a bare import that no `paths` entry and no installed
  package satisfies is tried as a path under it (`import Btn from
  "components/Btn"` with `baseUrl: "src"`); an installed package always wins.
  Asset imports through an alias (`import logo from "@/assets/logo.png"`) work
  like relative ones.
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

For Go components, run `krate go:init` once - it adds `.krate/types` to your
`tsconfig` `include` (see [Go Components](/docs/features/go-components/)).

See [Typed Routes & Content](/docs/features/typed-routes/) for usage.

## Global JSX types

`@krate/runtime` provides the global `JSX` namespace (intrinsics, ARIA, SVG
attributes, `Krate.TypedRoutes`). Ensure your `tsconfig` resolves it - either
via `jsxImportSource` above or a `/// <reference types="@krate/runtime" />`
in an ambient `.d.ts`.

`src/krate-env.d.ts` bridges Krate's globals like `Head`, `Link`, `Image`, and
`Icon`. It is generated (by `krate dev`, `krate build` or `krate types`) and
git-ignored by the scaffold, so run `krate types` once after cloning for editors
and `tsc` to see it. The scaffold's `typecheck` script does exactly that:
`krate typecheck`.

## Typechecking in CI

```sh
krate typecheck
```

Add this after `krate build`. It catches type errors the compiler tolerates
(Krate strips types rather than validating them). It generates the types, then
runs your project's `tsc --noEmit` without the errors `tsc` reports for
[tier decorators](/docs/core-concepts/component-tiers/#tier-decorators). Any other
error fails it, including a misspelled decorator.

## Editor support

Tier decorators are errors to TypeScript, so editors underline them. The scaffold
turns that off with a language service plugin in `tsconfig.json`:

```json
{
  "compilerOptions": {
    "plugins": [{ "name": "@krate/ts-plugin" }]
  }
}
```

For an existing project, install `@krate/ts-plugin` as a dev dependency and add
the entry above. In VS Code, run **TypeScript: Select TypeScript Version** and
choose the workspace version, since plugins load from your project.

Plugins only run in editors, which is why the command line uses `krate typecheck`
instead of `tsc`.

## How Krate compiles TypeScript

Before compiling a `.ts`/`.tsx` page, layout or component, Krate strips its
types with esbuild (built into the `krate` binary). Everything TypeScript
accepts is supported, including:

- `enum`, `const enum` and `namespace` (lowered to plain objects)
- constructor parameter properties (`constructor(private a: number)`)
- decorators (standard, or legacy when `experimentalDecorators` is set)
- nested and conditional generics (`Map<string, Set<string>>`), generic
  arrows (`<T,>(x: T) => x`), `satisfies` and `as const`

Imports follow `verbatimModuleSyntax`: `import type` and inline `type`
specifiers are removed, every other import is kept. Use `import type` for
type-only imports from modules that have no runtime export of that name.

From your `tsconfig.json`, Krate honours `experimentalDecorators` and
`useDefineForClassFields`. TypeScript syntax errors fail the build with the
original `file:line:col`. Comment directives such as `// @server` still apply.

### Decorators

Decorators on classes and their members work on ordinary classes, in `.js` and
`.jsx` files too. Those always use standard decorators, since
`experimentalDecorators` is a TypeScript option.

A class component is compiled to a function component, so most decorators cannot
apply to it: the build warns with
[KR3011](/docs/reference/diagnostics/#kr3011). Decorators on class components are
only supported for [tiers](/docs/core-concepts/component-tiers/#tier-decorators).

## Strictness

Keep `strict: true`. Krate's generated types assume it. If you need looser
settings during a migration, scope them per-directory rather than globally.
