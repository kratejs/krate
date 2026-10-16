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
  boundaries become splice markers (`<!--suspense:…-->` for Suspense regions,
  `<!--region:…-->` for standalone runtime components).
- SSR/ISR pages carry one coarse "page" region marker around the whole body.
- The Node (or bun/deno) sidecar renders **only** the regions the Go server asks
  for — via `/__krate/regions` — using the compiled page bundle
  (`dist/.krate/server-bundles/…`) or the compiled runtime-component bundles
  (`dist/server-components/…`). The Go server splices the returned HTML into the
  shell and streams it.
- The Go server resolves the concrete URL to the page's canonical route
  (e.g. `/video/abc` → `/video/[id]`), forwards the URL params, and keys ISR
  caching by route + params + query.
- `manifest.json` records each page's mode, and `server-manifest.json` lists the
  page bundles and each page's region registry for the sidecar.

See [Choosing an approach](/docs/core-concepts/rendering/) for what to use when.
