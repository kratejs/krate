---
title: Styling
order: 6
description: Style pages with CSS Modules, a Go-native Tailwind, and a built-in CSS pipeline.
---

# Styling

Krate ships a CSS pipeline built into the compiler: CSS Modules, Go-native
Tailwind, rule-level deduplication, minification, and `@import` inlining. No
PostCSS or extra tooling is required.

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
  return <div class={styles.card}>...</div>;
}
```

- Class names are hashed: `className` -> `className_<fnv32a_hash>`.
- The hash is **FNV-32a of the absolute file path** (6-char base36), so it's
  deterministic per file and stable across builds.
- Scoping is tokenizer-based: strings, comments, `url(...)`, attribute
  selectors, and `:global(...)` are handled correctly. `:global(.x)` stays
  unscoped (the wrapper is removed), and `composes:` local names are mapped.

## Tailwind CSS

Krate's Tailwind is **Go-native** - no PostCSS, no Node at build time - and
follows the **Tailwind v4 CSS-first** model.

```typescript
tailwind: {
  enabled: true,
  scanDirs: ["src"],          // or: content: ["./src/**/*.{tsx,mdx}"]
  preflight: true,            // base reset (default; false to opt out)
  strict: false,              // warn on classes that produce no rule
  darkMode: "media",          // "media" | "class" | "selector"
}
```

### CSS-first `@theme`

Define design tokens directly in CSS; Krate reads `@theme { --... }` blocks from
your stylesheets and generates the matching utilities (no `tailwind.config.ts`
needed for these):

```css
@theme {
  --color-brand-500: #ff4d4d;
  --spacing-7: 1.75rem;
  --radius-xl: 1rem;
  --breakpoint-3xl: 120rem;
}
/* now: bg-brand-500, p-7, rounded-xl, 3xl:... all work */
```

Supported namespaces: `--color-*` (incl. `DEFAULT` and `name-<shade>`),
`--spacing-*`, `--radius-*`, `--shadow-*`, `--opacity-*`, `--leading-*`,
`--breakpoint-*`, `--font-*`.

### `@apply`

`@apply` is supported in your CSS. Plain utilities are expanded in place;
variant/descendant/unknown utilities cannot be represented as plain
declarations and are reported as build warnings. `@tailwind` directives are
stripped and `@layer { ... }` blocks are unwrapped (Krate owns the layer order).

- A **candidate scanner** extracts Tailwind tokens from every string and
  template literal in the scanned files, so classes inside
  `class={cond ? "a" : "b"}`, `clsx()/cn()/cva()` arguments, and arrays are
  all found.
- The CSS generator maps classes to rules from a built-in rule set. Output is
  **deterministic** - identical inputs always produce the same stylesheet.
- Theme configuration lives in `tailwind.config.ts`. It is **statically
  parsed** by default (no Node required); set `tailwind.executeConfig: true` to
  evaluate it in-process instead. `theme.extend` merges onto the defaults;
  top-level `theme` keys replace them.

```tsx
<div class="p-4 hover:bg-zinc-100 dark:bg-zinc-900 w-[100px] -mt-2 bg-blue-500/50">
  Responsive, dark-mode aware, arbitrary values, negatives, color opacity.
</div>
```

Supported features:

- **Variants:** state (`hover:`, `focus:`, `active:`, `disabled:`, ...),
  `group-*`/`peer-*` (incl. named), `data-*`/`aria-*`/`has-*`, responsive
  breakpoints from `theme.screens` (`sm:`, `md:`, ..., plus `min-*`/`max-*`),
  `dark:`, `motion-safe:`/`motion-reduce:`, `print:`, capability queries
  (`pointer-coarse:`, `noscript:`, `forced-colors:`, ...), positional
  (`nth-*`, `first-line:`), `not-*`, child (`*:`, `**:`), container queries
  (`@sm:`, `@[400px]:`), and arbitrary variants (`[&:nth-child(3)]:`).
- **Arbitrary values** for nearly every property, with type disambiguation
  (`w-[100px]`, `bg-[url(/a.png)]`, `bg-[length:8px]`, `text-[14px]` ->
  font-size vs `text-[#fff]` -> color, `grid-cols-[repeat(3,_1fr)]`), including
  typed hints (`text-[color:var(--fg)]`).
- The v4 CSS-variable shorthand (`w-(--w)`, `text-(length:--s)`,
  `bg-red-500/(--alpha)`), arbitrary properties (`[mask-type:luminance]`,
  `[--x:1px]`, with variants), and v4 arbitrary-value normalization
  (`w-[calc(100%-2rem)]` becomes `calc(100% - 2rem)`, `m-[0_auto]`, and
  `url()` keeps its underscores).
- Negatives (`-mt-4`), color opacity modifiers (`bg-blue-500/50`,
  `from-indigo-400/50`), and arbitrary spacing multiples (`p-13`, `p-13.5`),
  scaled by the configured `--spacing` unit.
- **Animation**: `animate-spin|ping|pulse|bounce`, theme animations
  (`--animate-*`), `animate-[wiggle_1s_infinite]` and `animate-(--var)`. Every
  `@keyframes` an emitted animation references is output with the stylesheet,
  as are the `@property` registrations of the `--tw-*` composition variables.
- **Transitions** compose like v4: `duration-*` and `ease-*` set `--tw-duration`
  and `--tw-ease`, which `transition`, `transition-colors`, ... read, so
  `transition-colors duration-700` is 0.7s in any order.
- **Transforms** use the individual `translate`, `rotate` and `scale`
  properties (plus `transform` for skew and 3D rotation), so they compose with
  each other and with keyframe animations such as `animate-spin rotate-45`.
- Gradients compose like v4 (`bg-linear-*`, `bg-radial`, `bg-conic`, `from-*`,
  `via-*`, `to-*` with colors and `10%` positions, interpolated in oklab; add
  `/oklch` etc. to change it). Rings, shadows and `inset-ring` share the v4
  box-shadow composition, so `ring-2 ring-offset-2 ring-offset-white` draws
  both. `space-*` and `divide-*` style the children through a zero-specificity
  `:where(& > :not(:last-child))` selector.
- **Filters** (`blur-*`, `brightness-*`, `grayscale`, `hue-rotate-*`, `invert`,
  `saturate-*`, `sepia`, `drop-shadow-*`, and `backdrop-*`), **animation**
  (`animate-*`), **blend modes**, plus layout, typography, interactivity,
  3D-transform, border, and background families.
- Preflight is Tailwind v4's reset (including `[hidden]` staying hidden) and
  is emitted by default. Set `tailwind.preflight: false` to opt out.
- Scale names follow v4: `rounded-xs|sm|md`, `shadow-2xs|xs|sm`, `blur-xs|sm`.
  `container` has a `max-width` per configured breakpoint.

### Differences from Tailwind

Krate implements Tailwind's utility and variant syntax but is not byte-identical
to the Tailwind CLI. **JS plugins** and the `@tailwindcss/*` plugin ecosystem
remain out of scope. Unrecognized classes produce no rule (enable
`tailwind.strict` to surface them).

## Global CSS & code splitting

Plain CSS imported or referenced in pages is collected and split at the
**rule level**: rules used by two or more pages are written once to a shared
`styles.<hash>.css` chunk, and page-unique rules go into that page's own hashed
chunk. A component library imported across many pages therefore ships once, not
per page. Chunk order is deterministic, so hashes are stable across builds. See
[Contributing -> Architecture](/docs/contributing/architecture/#css-pipeline) for
the full processing steps.

## Custom properties & theming

There's nothing special needed to use CSS custom properties - they pass through
the pipeline unchanged. The docs site uses a `:root[data-theme="dark"]` scheme
toggled at runtime for light/dark theming.
