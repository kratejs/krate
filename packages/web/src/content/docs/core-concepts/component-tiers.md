---
title: Component Tiers
description: Static, client, server, and runtime components - where each runs.
order: 4
---

# Component Tiers

Components are classified into four tiers that determine how they're rendered
and how much JavaScript reaches the client.

| Tier | Client JS | Rendering |
|------|-----------|-----------|
| **Static** (`@static`) | None | Evaluated at build time; output is pure HTML |
| **Client** (default) | Yes (hydration) | SSG + client hydration |
| **Server** (`@server`) | None | Evaluated at build time; HTML output only |
| **Runtime** (`@runtime`) | None | Rendered at request time by the sidecar and spliced into the static shell |

## Detection priority

The tier is resolved in this order (highest wins):

1. **Tier decorator** on the declaration, see [below](#tier-decorators):
   ```tsx
   @server
   export default function Stats() { ... }
   ```
2. **Directive in source** (first non-comment line):
   ```tsx
   // @server
   // @runtime
   // @static
   ```
3. **File convention**:
   - `*.server.tsx` -> server
   - `*.runtime.tsx` -> runtime
   - `*.static.tsx` -> static
4. **Config name list**:
   ```ts
   serverComponents: ["DataTable"]
   runtimeComponents: ["AuthCheck"]
   ```
5. **Directory membership**:
   ```ts
   serverDirs: ["src/components/server"]
   runtimeDirs: ["src/components/runtime"]
   ```
6. **Default** -> client.

An `async` component is always a runtime component, whatever else it says.

## Tier decorators

`@server`, `@static`, `@runtime` and `@client` can go directly on a component.
They work on functions, classes and consts, which ordinary JavaScript decorators
do not, because Krate removes them at compile time. Nothing runs at load time.

```tsx
import { Component } from "react";

@static
const Badge = ({ label }) => <b>{label}</b>;

@server
class Report extends Component {
  render() {
    return <table>...</table>;
  }
}

@server
export default function Dashboard() {
  return <main><Badge label="New" /><Report /></main>;
}
```

- On the **default export**, or on the **only exported component** of a file, the
  decorator classifies the whole file, exactly like a `// @server` comment at the
  top. The composition rules below apply.
- On **any other declaration** it classifies only that component, so one file can
  mix tiers. `@client` is useful here, to opt one component out of a `// @server`
  file.
- `@runtime` must be on the default export or the only exported component,
  because a runtime component is bundled per file.
- On an `async` component only `@runtime` is accepted.

A decorator above `export default () => <div />` or `export default memo(...)`
(also `forwardRef(...)`) applies to the file's default component, like one above
`export default function`. Other misplaced decorators fail the build with
[KR7115](/docs/reference/diagnostics/#kr7115); for a default export of any other
expression, name the component (`@client` above `const View = ...;`, then
`export default View;`). If your project already imports its own decorator named
`server` or `client` and applies it to a class, that one is used instead.

### TypeScript

To TypeScript these are errors, since it does not know they are compile-time
only. Scaffolded projects handle that for you: `npm run typecheck` runs
[`krate typecheck`](/docs/cli/), which leaves them out, and
[`@krate/ts-plugin`](/docs/guides/typescript-setup/#editor-support) does the same
in editors.

Plain `tsc --noEmit` still reports them. If you have to run it, add a comment on
the line above each decorator:

```tsx
// @ts-expect-error tier decorator
@server
export default function Stats() { ... }
```

A `// @server` comment or a `*.server.tsx` file name needs neither, and classifies
the whole file.

## Composition rules

- **Client** components **cannot** import server/runtime components.
- **Static** components can be imported by anyone (they produce no client JS).
- Server/runtime components can import each other freely.

## Server components

```tsx
// src/components/ServerTime.server.tsx  (or: // @server)
export default function ServerTime() {
  return <time>{new Date().toUTCString()}</time>;
}
```

Server components are evaluated at build time. The HTML they produce is baked
into the page; they never ship JavaScript.

## Runtime components

```tsx
// @runtime
export default function PriceTag({ price }) {
  return <span>{formatCurrency(price)}</span>;
}
```

Runtime components are compiled during the build into self-contained bundles
(`dist/server-components/<Name>.runtime.js`) that expose a `__krate_render`
render function. Their resolved props are baked into the page's region registry,
so the sidecar never re-derives them.

Each runtime component instance becomes a **region** of the page. At request
time the sidecar renders the region's bundle and the Go server splices the HTML
into the static shell at the component's marker (`<!--region:...-->`), streaming
as it resolves. Inside a `<Suspense>` boundary the fallback is what's baked and
shown until the region arrives (`<!--suspense:...-->` marker).

Pages that import runtime components are automatically treated as **streaming**
(unless the page opts into ISR or SSR, whose per-request page region is the
whole body).

Runtime components render on the server at request time - good for data that
changes, without sending JavaScript to the browser. Named exports are fully
supported, including arrow functions (`export const Live = () => ...`).

## Why tiers matter

Static and server components produce zero client JavaScript. Runtime components
render on the server at request time - good for data that changes, without
sending JavaScript to the browser. Client components get hydration so they can
be interactive.

See [Rendering](/docs/core-concepts/rendering/) for how tiers interact with
SSG/ISR/SSR/streaming.
