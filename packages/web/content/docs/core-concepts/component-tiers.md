---
title: Component Tiers
order: 4
---

# Component Tiers

Components are classified into four tiers that determine how they're rendered
and how much JavaScript reaches the client.

| Tier | Client JS | Rendering |
|------|-----------|-----------|
| **Static** (`@static`) | None | Evaluated at build time; output is pure HTML |
| **Client** (default) | Yes (hydration) | SSR/SSG + client hydration |
| **Server** (`@server`) | None | Evaluated at build time; HTML output only |
| **Runtime** (`@runtime`) | None | Serve-time via embedded QuickJS, streamed through Suspense |

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
(`dist/server-components/<Name>.runtime.js`) and executed on the server via the
embedded QuickJS runtime. They're streamed to the client through Suspense
boundaries.

Pages that import runtime components are automatically upgraded to **streaming
mode**:

1. Phase 1 (fallback) — renders the page with fallback content.
2. Phase 2 (resolved) — streams resolved content via `<!--suspense-resolved:N-->` markers.

## Why tiers matter

Static and server components produce zero client JavaScript. Runtime components
render on the server at request time — good for data that changes, without
sending JavaScript to the browser. Client components get hydration so they can
be interactive.

See [Rendering](/docs/core-concepts/rendering/) for how tiers interact with
SSG/SSR/streaming.
