---
title: Styling
order: 6
description: Style pages with CSS Modules, a Go-native Tailwind, and a built-in CSS pipeline.
---

# Styling

Krate ships a CSS pipeline built into the compiler: CSS Modules, Go-native
Tailwind, duplicate-declaration folding, minification, and `@import` inlining. No
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
  unscoped (the wrapper is removed); a bare `:global` or `:local` switches the
  mode for the rest of the selector.
- `@keyframes` names defined in a module are scoped, and `animation` /
  `animation-name` references are rewritten; use `@keyframes :global(name)` to
  keep a name global.
- `composes: other;`, `composes: a b;`, `composes: x from global;` and
  `composes: x from "./base.module.css";` add the composed classes to
  `styles.<name>` and are removed from the output CSS.
- Dashed names have camelCase aliases: `styles["card-title"]` and
  `styles.cardTitle` are the same class.

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

### CSS-first directives

Krate reads the Tailwind v4 CSS-first directives from your stylesheets (files
under the scan directories and the CSS your pages import), uses them to
configure the generator, and **removes them from the shipped CSS**:

```css
@import "tailwindcss" prefix(tw) important; /* modifiers: prefix(), important, source() */

@theme {
  --color-brand-500: #ff4d4d;
  --spacing: 0.3rem;                  /* base spacing unit */
  --text-xl--line-height: 1.6;
  --font-display: "Playfair Display", serif;
  --animate-fadein: fadein 1s ease-out;
  @keyframes fadein { from { opacity: 0 } to { opacity: 1 } }
}

@source "../shared-ui";               /* extra directory to scan */
@source not "../shared-ui/legacy";    /* excluded directory */
@source inline("underline p-{1,2}");  /* always generate these */

@utility tab-4 { tab-size: 4; }       /* static custom utility, works with variants */
@custom-variant theme-mid (&:where([data-theme=mid] *));
@custom-variant dark (&:where(.dark, .dark *));
```

- `@theme` is parsed with real brace matching, so nested `@keyframes` and
  values containing `}` work. Supported namespaces: `--color-*` (incl.
  `name-<shade>`), `--spacing`, `--spacing-*`, `--text-*` (and
  `--text-*--line-height`), `--font-*`, `--font-weight-*`, `--tracking-*`,
  `--leading-*`, `--radius-*`, `--shadow-*`, `--opacity-*`, `--breakpoint-*`,
  `--container-*`, `--animate-*`. `--color-*: initial` (or any
  `--ns-*: initial`, or `--*: initial`) resets a namespace. `@theme inline`,
  `static` and `default` are accepted; `@theme reference` makes variables usable
  by utilities without emitting them. Variables for namespaces Krate has no
  utilities for (`--ease-*`, `--blur-*`, ...) are still emitted and reported
  as a warning.
- Theme variables are emitted in `@layer theme { :root, :host { ... } }`. Krate
  emits every user-defined `@theme` variable (v4 emits only the used ones unless
  `static`) plus any built-in theme variable you reference as `var(--color-red-500)`,
  `var(--spacing)`, etc. in your CSS or scanned files.
- `@utility` supports static declaration blocks only; functional utilities
  (`name-*` with `--value()`) and nested rules are reported and skipped.
  `@custom-variant` supports the `(selector)` form, `@media`/`@supports`, and the
  single `selector { @slot; }` block form.
- `@config` and `@plugin` are **not supported**: they are removed and reported
  as a warning (Krate reads `tailwind.config.{ts,js,mjs}` from the project root
  itself; JS plugins are never executed). `@variant` inside rules is reported
  and left in place.
- Warnings are printed once per build.

### Layering

The generated stylesheet uses cascade layers like Tailwind v4:

```css
@layer theme, base, components, utilities;
@layer theme { :root, :host { --color-brand-500: #ff4d4d } }
@layer base { /* preflight */ }
@layer utilities { /* .p-4, .hover\:bg-red-500, ... */ }
@keyframes ...   /* used animations, outside the layers */
```

The Tailwind sheet is linked before page CSS. Your **unlayered** CSS therefore
beats both preflight and utilities (same as v4), your own `@layer base { ... }`
rules come after preflight, and utilities beat preflight and any layered
base/components rules. To make a utility win over an unlayered component rule,
put that rule in `@layer components`.

**Migrating a reset:** a global reset such as `*, *::before, *::after { margin: 0;
padding: 0 }` written as plain unlayered CSS now beats every utility (`p-3`,
`mt-2` compute to `0`). Unlayered CSS beats Tailwind utilities, so put resets in
`@layer base { ... }`.

### `@apply`

`@apply` is supported anywhere inside a rule body (`.btn { @apply px-4 py-2; }`,
including on one line). Plain utilities are expanded in place in utility sort
order; animation utilities also emit their `@keyframes`. Variant,
descendant-selector (`space-x-*`) and unknown utilities cannot be represented as
plain declarations and are reported as build warnings. `@tailwind` directives are
stripped, and `@layer` blocks are left as authored.

- A **candidate scanner** extracts Tailwind tokens from the whole file, in the
  spirit of Tailwind's own extractor: it does not depend on string tokenization,
  so TSX, TS, MDX, Markdown and HTML (including prose with apostrophes, regex
  literals and `${}` template interpolations) all work, and arbitrary values and
  variants containing `= ' " & > +` (`data-[state=open]:p-4`, `[&>svg]:size-4`,
  `has-[>img]:`, `nth-[2n+1]:`, `content-['x']`, `bg-[url('/a.png')]`,
  `bg-(--brand)`) are kept. As in Tailwind v4, plain words in scanned prose such
  as `hidden`, `table`, `block` or `border` can generate the matching utility.
  Dynamic fragments like `bg-${x}` generate nothing.
- With `tailwind.strict: true`, candidates that look like utilities but matched
  no rule are printed as a build warning.
- The CSS generator maps classes to rules from a built-in rule set. Output is
  **deterministic** - identical inputs always produce the same stylesheet.
- Theme configuration lives in `tailwind.config.ts`. It is **statically
  parsed** by default (no Node required); set `tailwind.executeConfig: true` to
  evaluate it in-process instead. `theme.extend` merges onto the defaults;
  top-level `theme` keys replace them. Supported keys include `colors`,
  `spacing`, `screens`, `fontFamily` (strings or arrays), `fontSize` (including
  the `[size, { lineHeight }]` form), `letterSpacing`, `animation`, `keyframes`,
  `borderRadius`, `boxShadow`, `darkMode` (`'class'`, `'media'`,
  `['selector', '.x']`, `['class', '[data-mode="dark"]']`), `prefix` and
  `important`. Unsupported or statically unevaluable entries (`plugins`,
  `presets`, `safelist`, `screens` with `{ min, max }`, computed values) are
  reported as build warnings, never ignored silently.

```tsx
<div class="p-4 hover:bg-zinc-100 dark:bg-zinc-900 w-[100px] -mt-2 bg-blue-500/50">
  Responsive, dark-mode aware, arbitrary values, negatives, color opacity.
</div>
```

Supported features:

- **Variants:** state (`hover:`, `focus:`, `active:`, `disabled:`, ...),
  `group-*`/`peer-*` (incl. named: `group-hover/item:`), `data-*`/`aria-*`/
  `has-*`/`in-*` (these match the element itself: `data-[state=open]:` is
  `.x[data-state="open"]`), responsive breakpoints from `theme.screens` (`sm:`,
  `md:`, ..., plus `min-*`/`max-*` and `min-[900px]:`), `dark:`,
  `motion-safe:`/`motion-reduce:`, `print:`, capability queries
  (`pointer-coarse:`, `noscript:`, `forced-colors:`, ...), positional
  (`nth-*`, `first-line:`), `not-*`, child (`*:`, `**:`), `starting:`, container
  queries (`@container`, `@container/name`, `@sm:`, `@max-md:`, `@sm/name:`,
  `@[400px]:`, sizes from `--container-*`), and arbitrary variants
  (`[&:nth-child(3)]:`, `[&_p]:`, `[@media(min-width:900px)]:`).
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

### Variant order, stacking, prefix and important

- Stacked variants apply left to right as in Tailwind v4: `hover:before:` is
  `.x:hover::before` and `*:hover:` is `:is(.x > *):hover`. `hover:` is gated by
  `@media (hover: hover)`.
- Rules are emitted in Tailwind's cascade order: utilities without variants
  first, then variants by Tailwind's registration order (so `disabled:` beats
  `hover:`, `dark:` beats breakpoints, and `max-md:` comes before `max-sm:`),
  breakpoints by size (compared in px across `px` and `rem`), and utilities by
  Tailwind's property order (so `pl-4` beats `px-2` regardless of name).
- Class-mode dark mode uses `:where(.dark, .dark *)`; `darkMode:
  ["class", "[data-mode=dark]"]` uses that selector instead of `.dark`.
- `prefix: "tw"` makes utilities `tw:flex` / `tw:hover:flex`; unprefixed
  utilities are then unknown. CSS variables are not prefixed (Krate resolves
  theme values at build time).
- `important: true` marks every utility `!important`; a selector string
  (`important: "#app"`) scopes every utility under it (the Tailwind v3 form).
  The per-class `!` works as a prefix (`!p-4`) or suffix (`p-4!`).

### Differences from Tailwind

Krate implements Tailwind's utility and variant syntax but is not byte-identical
to the Tailwind CLI. **JS plugins** and the `@tailwindcss/*` plugin ecosystem
remain out of scope. Unrecognized classes produce no rule (enable
`tailwind.strict` to surface them).

## Global CSS & code splitting

Plain CSS imported or referenced in pages is collected per page, in source and
import order, and written as **one stylesheet per page** to a content-hashed
`styles.<hash>.css`. Pages whose stylesheets are byte-identical share a single
file, so a layout-only site ships one stylesheet. Rules are never reordered or
moved between files, so the cascade you wrote is the cascade that ships, and
`@layer` blocks are preserved as authored. See
[Contributing -> Architecture](/docs/contributing/architecture/#css-pipeline) for
the full processing steps.

## Custom properties & theming

There's nothing special needed to use CSS custom properties - they pass through
the pipeline unchanged. The docs site uses a `:root[data-theme="dark"]` scheme
toggled at runtime for light/dark theming.
