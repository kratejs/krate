---
title: Search
order: 7
tags:
  - search
  - pagefind
  - docfind
  - wasm
categories:
  - features
---

# Search

The docs plugin ships a full-text search bar with three interchangeable
backends. The default and recommended backend is
[**Pagefind**](https://pagefind.app) — a mature static search library that
indexes your **built HTML** after a production build and serves a **chunked,
streamed** index, so only the pieces a query needs are downloaded. This scales
best on large documentation sites.

Krate also ships [**docfind**](https://github.com/microsoft/docfind) (Microsoft's
Rust-based engine) and a classic JSON index. **docfind is what Krate uses during
development builds** (see [Engines](#engines)), and it remains available for
production if you'd rather not depend on Node.

## How it works

1. **At build time**, the docs plugin renders every documentation page to HTML
   and tags the content region with `data-pagefind-body`.
2. **After a production build**, the docs plugin runs the Pagefind indexer
   (`npx pagefind`) over the output directory. Pagefind writes a chunked search
   bundle to `pagefind/`:

```
dist/
  pagefind/           # chunked index + Pagefind runtime (search-time fetches)
  docs/
    search/
      search.js       # Krate's search UI (trigger, modal, keyboard nav)
      search.css      # search UI styles
    data/
      search-index.json # classic JSON fallback (always written)
```

3. **In the browser**, pressing the search button (or **Ctrl/Cmd+K**) opens a
   command-palette-style dialog. Typing queries the Pagefind index locally and
   streams the chunks it needs. If Pagefind can't load (offline, or a dev build),
   the UI automatically falls back to the JSON index.

:::note
**In development**, `krate dev` builds the docfind WASM index instead of running
Pagefind, because Pagefind's index is a whole-site post-build pass that dev's
incremental rebuilds don't re-run. Search therefore works identically while you
write, and switches to Pagefind in production.
:::

## Configuration

```ts
docs({
  contentDir: "src/content/docs",
  title: "Docs",
  search: {
    enabled: true,        // default: true
    engine: "pagefind",   // "pagefind" (default) | "docfind" | "json"
    maxResults: 8,        // default: 8
    pagefind: {           // only read when engine: "pagefind"
      excludeSelectors: [],      // extra selectors to skip
      includeCharacters: "",     // e.g. "<>" to index code punctuation
      forceLanguage: "",         // ISO 639-1, e.g. "en"
      outputSubdir: "pagefind",  // bundle dir under the output root
      verbose: false,
    },
  },
})
```

| Option | Default | Description |
|--------|---------|-------------|
| `enabled` | `true` | Turn the search bar on/off |
| `engine` | `"pagefind"` | `"pagefind"` (recommended) runs the [Pagefind](https://pagefind.app) indexer after production builds; `"docfind"` builds the embedded WASM index; `"json"` uses `search-index.json` only |
| `maxResults` | `8` | Max results shown |
| `pagefind` | – | Options for the `"pagefind"` engine |

## Engines

Krate ships three search backends. The client picks the configured engine and
**automatically falls back** to the next best option if it can't load, so search
keeps working in dev, offline, or when an index wasn't built.

| Engine | When it runs | Index location | Needs Node? |
|--------|--------------|----------------|-------------|
| `pagefind` (default, recommended) | After a production build | `pagefind/` bundle | Yes (`npx`) |
| `docfind` | Build, in-process (always used in dev) | `docs/search/docfind_bg.wasm` | No |
| `json` | Build | `docs/data/search-index.json` | No |

### pagefind (recommended)

[Pagefind](https://pagefind.app) is the default backend. It indexes the **built
HTML** after a production build, then serves a chunked, streamed index. It scales
better on large sites because only the chunks a query needs are downloaded, and
it is a mature, widely used search library.

It is on by default — no configuration required:

```ts
docs({ search: { engine: "pagefind" } }) // the default
```

- **Production only.** Pagefind runs as a post-build step (`npx pagefind`). In
  `krate dev`, Krate uses docfind instead, because the index isn't rebuilt on
  every incremental edit.
- **Node + network required.** The first run downloads the Pagefind binary. If
  `npx` or the download fails, Krate prints a warning and the client falls back
  to docfind/JSON — the build never fails.
- **Scoped indexing.** The docs content wrapper is tagged with
  `data-pagefind-body`, so page chrome (navbar, sidebar, table of contents, the
  search dialog) is not indexed. Use `pagefind.excludeSelectors` for extras.
- **Reusing Krate's UI.** Pagefind's search API is driven by Krate's existing
  modal, so the look, `Ctrl/Cmd+K`, keyboard navigation, and `.krate-search-*`
  theming are identical across engines. Pagefind's own component UI is not used.

:::note
`krate build --watch` incremental rebuilds do not re-run the Pagefind indexer —
run a full `krate build` to refresh the index.
:::

### docfind

[**docfind**](https://github.com/microsoft/docfind) is Microsoft's Rust-based
document search engine (FST + FSST + RAKE keyword extraction). Its index is built
**in-process** at build time and embedded into a WASM module, so searching is
fully local with zero network round-trips and no Node required.

Krate uses docfind for **development builds** regardless of the configured
engine, and it's also a good choice for production when you want a fully
self-contained, pure-Go build:

```ts
docs({ search: { engine: "docfind" } })
```

See [No subprocess, no temp files](#no-subprocess-no-temp-files) below.

### json

The classic fallback: a plain `search-index.json` scored in the browser. Always
written, and used automatically whenever a richer engine is unavailable.

## Improving results

docfind extracts keywords from titles, categories, and bodies using the RAKE
algorithm. Add explicit keywords in frontmatter for pages whose content doesn't
capture the right terms:

```md
---
title: Signals
keywords: [reactive, createSignal, createEffect, createMemo, state]
---
```

Keywords are weighted highest, then title words, then body phrases.

## No subprocess, no temp files (docfind)

The docfind index build runs inside the krate process. The Rust builder module is
compiled once and `go:embed`-ed into the krate binary; documents are passed
through WASM memory directly. There's no `docfind` CLI dependency at build time
and no `documents.json` written to disk.

## Customizing the UI

The search UI lives in `dist/docs/search/search.js` and `search.css` after a
build. The trigger/dialog markup is generated by the docs plugin
(`.krate/gen/docs/SearchBar.tsx`); you can restyle it by overriding the
`.krate-search-*` classes in your own stylesheet.
