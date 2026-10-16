---
title: Component Tiers
description: Static, client, server, and runtime components — where each runs.
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

1. **Directive in source** (first non-comment line):
   ```tsx
   // @server
   // @runtime
   // @static
   ```
2. **File convention**:
   - `*.server.tsx` → server
   - `*.runtime.tsx` → runtime
   - `*.static.tsx` → static
3. **Config name list**:
   ```ts
   serverComponents: ["DataTable"]
   runtimeComponents: ["AuthCheck"]
   ```
4. **Directory membership**:
   ```ts
   serverDirs: ["src/components/server"]
   runtimeDirs: ["src/components/runtime"]
   ```
5. **Default** → client.

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
into the static shell at the component's marker (`<!--region:…-->`), streaming
as it resolves. Inside a `<Suspense>` boundary the fallback is what's baked and
shown until the region arrives (`<!--suspense:…-->` marker).

Pages that import runtime components are automatically treated as **streaming**
(unless the page opts into ISR or SSR, whose per-request page region is the
whole body).

Runtime components render on the server at request time — good for data that
changes, without sending JavaScript to the browser. Named exports are fully
supported, including arrow functions (`export const Live = () => …`).

## Why tiers matter

Static and server components produce zero client JavaScript. Runtime components
render on the server at request time — good for data that changes, without
sending JavaScript to the browser. Client components get hydration so they can
be interactive.

See [Rendering](/docs/core-concepts/rendering/) for how tiers interact with
SSG/ISR/SSR/streaming.
