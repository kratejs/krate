---
title: Styling
order: 6
---

# Styling

Krate ships a full CSS pipeline: CSS Modules, Go-native Tailwind, rule-level
deduplication, minification, and `@import` inlining. There is no PostCSS and no
external CSS tooling.

## CSS Modules

Files named `*.module.css` are scoped automatically:

```tsx
// Card.module.css
.card {
  padding: 1rem;
  border-radius: 8px;
}
```

```tsx
import styles from './Card.module.css';

export default function Card() {
  return <div class={styles.card}>…</div>;
}
```

- Class names are hashed: `className` → `className_<fnv32a_hash>`.
- The hash is **FNV-32a of the absolute file path** (6-char base36), so it's
  deterministic per file and stable across builds.
- Scoping is tokenizer-based: strings, comments, `url(...)`, attribute
  selectors, and `:global(...)` are handled correctly. `:global(.x)` stays
  unscoped (the wrapper is removed), and `composes:` local names are mapped.

## Tailwind CSS

Krate's Tailwind is **Go-native** — no PostCSS, no Node at build time — and
follows the **Tailwind v4 CSS-first** model.

```typescript
tailwind: {
  enabled: true,
  scanDirs: ["src"],          // or: content: ["./src/**/*.{tsx,mdx}"]
  preflight: false,           // opt-in base reset
  strict: false,              // warn on classes that produce no rule
  darkMode: "media",          // "media" | "class" | "selector"
}
```

### CSS-first `@theme`

Define design tokens directly in CSS; Krate reads `@theme { --… }` blocks from
your stylesheets and generates the matching utilities (no `tailwind.config.ts`
needed for these):

```css
@theme {
  --color-brand-500: #ff4d4d;
  --spacing-7: 1.75rem;
  --radius-xl: 1rem;
  --breakpoint-3xl: 120rem;
}
/* now: bg-brand-500, p-7, rounded-xl, 3xl:… all work */
```

Supported namespaces: `--color-*` (incl. `DEFAULT` and `name-<shade>`),
`--spacing-*`, `--radius-*`, `--shadow-*`, `--opacity-*`, `--leading-*`,
`--breakpoint-*`, `--font-*`.

### `@apply`

`@apply` is supported in your CSS. Plain utilities are expanded in place;
variant/descendant/unknown utilities cannot be represented as plain
declarations and are reported as build warnings. `@tailwind` directives are
stripped and `@layer { … }` blocks are unwrapped (Krate owns the layer order).

- A **candidate scanner** extracts Tailwind tokens from every string and
  template literal in the scanned files, so classes inside
  `class={cond ? "a" : "b"}`, `clsx()/cn()/cva()` arguments, and arrays are
  all found.
- The CSS generator maps classes to rules from a built-in rule set. Output is
  **deterministic** — identical inputs always produce the same stylesheet.
- Theme configuration lives in `tailwind.config.ts`. It is **statically
  parsed** by default (no Node required); set `tailwind.executeConfig: true` to
  execute it via `npx tsx` instead. `theme.extend` merges onto the defaults;
  top-level `theme` keys replace them.

```tsx
<div class="p-4 hover:bg-zinc-100 dark:bg-zinc-900 w-[100px] -mt-2 bg-blue-500/50">
  Responsive, dark-mode aware, arbitrary values, negatives, color opacity.
</div>
```

Supported features:

- **Variants:** state (`hover:`, `focus:`, `active:`, `disabled:`, …),
  `group-*`/`peer-*` (incl. named), `data-*`/`aria-*`/`has-*`, responsive
  breakpoints from `theme.screens` (`sm:`, `md:`, …, plus `min-*`/`max-*`),
  `dark:`, `motion-safe:`/`motion-reduce:`, `print:`, capability queries
  (`pointer-coarse:`, `noscript:`, `forced-colors:`, …), positional
  (`nth-*`, `first-line:`), `not-*`, child (`*:`, `**:`), container queries
  (`@sm:`, `@[400px]:`), and arbitrary variants (`[&:nth-child(3)]:`).
- **Arbitrary values** for nearly every property, with type disambiguation
  (`w-[100px]`, `bg-[url(/a.png)]`, `bg-[length:8px]`, `text-[14px]` →
  font-size vs `text-[#fff]` → color, `grid-cols-[repeat(3,_1fr)]`), including
  typed hints (`text-[color:var(--fg)]`).
- Negatives (`-mt-4`), color opacity modifiers (`bg-blue-500/50`,
  `from-indigo-400/50`), and arbitrary spacing multiples (`p-13`, `p-13.5`).
- **Filters** (`blur-*`, `brightness-*`, `grayscale`, `hue-rotate-*`, `invert`,
  `saturate-*`, `sepia`, `drop-shadow-*`, and `backdrop-*`), **animation**
  (`animate-*`), **blend modes**, plus layout, typography, interactivity,
  3D-transform, border, and background families.
- Preflight: opt in with `tailwind.preflight: true`.

### Differences from Tailwind

Krate implements Tailwind's utility and variant syntax but is not byte-identical
to the Tailwind CLI. **JS plugins** and the `@tailwindcss/*` plugin ecosystem
remain out of scope. Unrecognized classes produce no rule (enable
`tailwind.strict` to surface them).

## Global CSS & code splitting

Plain CSS imported or referenced in pages is collected, then **split at the
rule level**:

- Rules used by **two or more pages** are written once to a shared
  `styles.<hash>.css` chunk, linked before per-page CSS on every page.
- Rules **unique to a page** go into that page's own hashed chunk.

So a component library imported across many pages ships once, not per page.
Chunk order is deterministic (shared chunk sorted by canonical rule), so
hashes are stable across builds.

## The CSS processing pipeline

1. **Collect** CSS per page from its module graph (module CSS, layout CSS,
   CSS-signal rules), in source/import order.
2. **Inline `@import`** recursively, relative to each file's own directory
   (circular-safe, depth limit 10); a trailing media/`supports`/`layer` prelude
   is honoured, not leaked.
3. **Resolve `url(...)`** assets relative to the sheet, content-hash and copy
   them to `/assets/…`, and rewrite the URL.
4. **Handle directives** — strip `@tailwind`, unwrap `@layer`, expand `@apply`.
5. **Minify** (string/URL/comment aware) — whitespace collapse, `rgba()`/`rgb()`
   → hex, hex shortening, zero-unit removal, `calc()` simplification, empty-rule
   and duplicate-declaration removal. Preserves strings, `url()`, `data:` URIs,
   `/*!` license comments, vendor-prefixed fallbacks, and `!important`.
6. **Chunk** into shared + per-page stylesheets (above) and hash filenames.

## Custom properties & theming

There's nothing special needed to use CSS custom properties — they pass through
the pipeline unchanged. The docs site uses a `:root[data-theme="dark"]` scheme
toggled at runtime for light/dark theming.
