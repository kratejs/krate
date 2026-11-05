---
title: Architecture
order: 2
description: How the Krate compiler works - lexing, parsing, bundling, rendering, CSS, markdown, search, and typed routes.
---

# Architecture

How Krate works internally. This is aimed at contributors; for using Krate, see
the [Core Concepts](/docs/core-concepts/) and [Features](/docs/features/).

## The compiler pipeline

```
Source (.tsx/.ts/.md/.mdx)
        |
        v
    Lexer (tokenize) --> Parser (AST) --> Bundler (imports, CSS modules, React rewrite)
        |
        v
    Renderer (SSR: AST -> HTML + signal/handler detection)
        |
        v
    Hydration Codegen (signals -> JS bundle with data-k/data-kh bindings)
        |
        v
    Build Output (HTML + hashed JS + hashed CSS + manifest.json)
```

The compiler is 100% Go: lexer, parser, bundler, renderer, CSS pipeline, and
Tailwind generator. There is no external bundler subprocess, and no Node.js
requirement for core compilation.

## Markdown & MDX

1. The markdown source is parsed with frontmatter extracted.
2. Markdown is rendered to HTML; JSX blocks are preserved as segments.
3. The docs plugin (or a `.mdx` page) re-inserts the JSX segments and compiles
   the result through the normal TSX pipeline.

Component directives (`:::component Name`, `:::card`, `:::steps`, `:::tabs`,
`:::code-group`) are expanded to real component JSX at build time, so they appear
in the output as static HTML with no client runtime.

## CSS pipeline

Plain CSS imported or referenced in pages is collected per page and written as
one stylesheet per page. Output filenames are content-hashed
(`styles.<hash>.css`), so pages with byte-identical stylesheets share one file
and hashes are stable across builds. There is no rule-level splitting: moving or
sorting rules between files would reorder the cascade. The processing steps are:

1. **Collect** CSS per page from its module graph (module CSS, layout CSS,
   CSS-signal rules), in source/import order.
2. **Inline `@import`** recursively with a comment- and string-aware scan,
   relative to each file's own directory (files anywhere under the project root
   and bare `node_modules` specifiers, including a package's `style` field).
   Cycles are detected along the import chain only, so the same file can be
   imported twice with different conditions (depth limit 10). The prelude
   becomes nested at-rules: `@supports (...){@media ...{@layer name{...}}}`.
   External `@import url(https://...)` statements are hoisted to the top of the
   page stylesheet. An import that cannot be resolved stays in place and is
   reported as a warning.
3. **Resolve `url(...)`** assets (`url(x)`, `url("x")`, `url( 'x' )`) relative to
   the file that contains them, including imported files, content-hash and copy
   them to `/assets/...`, and rewrite the URL. Root-absolute and external URLs
   are untouched; a relative file that does not exist is reported. Every
   imported file and asset is a dependency of the page, so editing one rebuilds
   it (build cache and `krate dev`).
4. **Handle directives** - strip `@tailwind` and expand `@apply`; `@layer`
   blocks are kept as written.
5. **Minify** (string/URL/comment/escape aware) - whitespace collapse, then
   value transforms that run only on declaration values (never on selectors,
   at-rule preludes, names, or custom property values): `rgba()`/`rgb()` -> hex,
   hex shortening, zero-length unit removal (not inside `calc()`/`min()`/`max()`/
   `clamp()`, `flex`, or for `fr`, `%`, time and angle units), type-safe
   `calc()` folding, empty-rule removal, and duplicate-declaration folding. A
   duplicate fold keeps the last declaration at its own position and is skipped
   when a related shorthand sits between the two or the earlier value looks like
   an intentional fallback. Strings, `url()`, `data:` URIs, `/*!` license
   comments, and `!important` are preserved. With minification off, only the
   duplicate-declaration fold runs and formatting is kept.
6. **Hash** the result into `styles.<hash>.css`; identical sheets share a file.

CSS Modules names are hashed as `className_<fnv32a_hash>`, where the hash is
FNV-32a of the absolute file path (6-char base36). Scoping is tokenizer-based:
strings, comments, `url(...)`, attribute selectors, and `:global(...)` are
handled correctly. `:global(.x)` and bare `:global` / `:local` switch scoping
for the rest of the selector. Local `@keyframes` names, and their uses in
`animation` / `animation-name`, are scoped too. `composes: a b`,
`composes: x from global` and `composes: x from "./other.module.css"` are
removed from the CSS and appended to the class mapping (the composed module's
CSS joins the page). The mapping also contains camelCase aliases of dashed names
and the scoped keyframes names, so `styles["card-title"]` and
`styles.cardTitle` both work. SSR, ISR and streaming server bundles use the
same mapping, so server-rendered class names match the stylesheet.

## Tailwind generation

Krate's Tailwind follows the Tailwind v4 CSS-first model and is Go-native (no
PostCSS, no Node at build time).

- A **candidate scanner** extracts Tailwind tokens from every string and
  template literal in the scanned files, so classes inside
  `class={cond ? "a" : "b"}`, `clsx()/cn()/cva()` arguments, and arrays are all
  found.
- The CSS generator maps classes to rules from a built-in rule set. Output is
  **deterministic** - identical inputs always produce the same stylesheet.
- Theme configuration lives in `tailwind.config.ts`, **statically parsed** by
  default; set `tailwind.executeConfig: true` to evaluate it in-process.
  `theme.extend` merges onto the defaults; top-level `theme` keys replace them.

## Docs search

1. **At build time**, the docs plugin renders every documentation page to HTML
   and tags the content region with `data-pagefind-body`.
2. **After a production build**, the docs plugin runs the Pagefind indexer
   (`npx pagefind`) over the output directory. Pagefind writes a chunked search
   bundle to `pagefind/`. A classic JSON index is always written too.
3. **In the browser**, the search dialog queries the configured engine locally
   and streams the chunks it needs, falling back to the JSON index if the engine
   can't load.

The **docfind** backend builds its index in-process at build time and embeds it
into a WASM module: the Rust builder is compiled once and `go:embed`-ed into the
krate binary, and documents are passed through WASM memory directly. There is no
`docfind` CLI dependency at build time and no `documents.json` written to disk.

## Typed routes & content

- Routes come from the page tree (`src/pages/**`), including plugin-generated
  pages and dynamic `[param]` segments. Segment pages (`404`/`not-found`/`500`/`error`) are excluded.
- Collection entries are discovered under each collection's `dir`; frontmatter
  is parsed with the same mini-YAML parser used by the docs plugin, and each
  entry's body is rendered through the markdown pipeline.
- `getCollection(...)` chains are evaluated and inlined as literal arrays before
  rendering, so collection-driven lists and single-entry reads bake into the
  page.
- Route/params types are advisory: `krate-env.d.ts` imports them and augments
  the runtime. If generation fails, the build warns rather than stopping.
- Content **schema violations are build errors**, because they indicate a
  content bug.

## MCP server

- **No MCP SDK dependency.** The protocol surface is implemented directly with
  `encoding/json`.
- **Stdout is reserved** for protocol frames; any incidental compiler output is
  redirected to stderr, and build/check output is captured.
- **Paths are anchored** to the project root and traversal is rejected.
- **Environment values never cross the bridge**, matching the compiler's
  existing discipline for `.env` handling.
