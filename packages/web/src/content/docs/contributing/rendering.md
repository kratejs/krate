---
title: Rendering internals
order: 3
description: How static shells, dynamic regions, and the SSR/ISR sidecar fit together.
---

# Rendering internals

How the rendering modes in [Rendering](/docs/core-concepts/rendering/) are
implemented.

- The Go compiler builds every page to a static shell. Server components are
  baked; static suspense content is baked as its resolved HTML; dynamic
  boundaries become splice markers (`<!--suspense:...-->` for Suspense regions,
  `<!--region:...-->` for standalone runtime components).
- SSR/ISR pages carry one coarse "page" region marker around the whole body.
- The Node (or bun/deno) sidecar renders **only** the regions the Go server asks
  for - via `/__krate/regions` - using the compiled page bundle
  (`dist/.krate/server-bundles/...`) or the compiled runtime-component bundles
  (`dist/.krate/server-components/...`). The Go server splices the returned HTML into the
  shell and streams it.
- The Go server resolves the concrete URL to the page's canonical route
  (e.g. `/video/abc` -> `/video/[id]`), forwards the URL params, and keys ISR
  caching by route + params + query.
- `dist/.krate/manifest.json` records each page's mode, and `dist/.krate/server-manifest.json` lists the
  page bundles and each page's region registry for the sidecar.
- A client component's hydration scope reads its call-site props from a
  registration its parent's scope makes. The registration holds only the props
  that scope reads (`props.x`, destructured keys, and props it forwards to a
  child that reads them); props used only for the server-rendered HTML are left
  out. Code that passes `props` whole, spreads it or indexes it with a computed
  key gets every prop, and so do renderer boundaries
  (`internal/renderer/propsreads.go`). A registration left empty is not
  emitted, and the child then binds no `props` at all.
- A handler that names a function of the component body (`onClick={handleCopy}`)
  binds that function, so its body appears once per scope however many elements
  use it. Side-effect-free declarations nothing in the scope references (helper
  functions, locals folded to a build-time value) are left out, and so are
  scopes with no statements unless another scope's effects owner hangs off them
  (`internal/renderer/newhydrate.go`).

See [Choosing an approach](/docs/core-concepts/rendering/) for what to use when.
