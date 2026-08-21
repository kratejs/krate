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

## Tailwind CSS

Krate's Tailwind is **Go-native** — no PostCSS, no Node at build time.

```typescript
tailwind: {
  enabled: true,
  scanDirs: ["src"],
}
```

- A **class scanner** extracts class names from all source files.
- The CSS generator maps classes to rules from a built-in rule set.
- Configuration lives in `tailwind.config.ts` (executed via `npx tsx`).

```tsx
<div class="p-4 hover:bg-zinc-100 dark:bg-zinc-900 w-[100px]">
  Responsive, dark-mode aware, arbitrary values.
</div>
```

Supported features:

- Variants: `hover:`, `focus:`, responsive breakpoints, `dark:`
- Arbitrary values: `w-[100px]`, `bg-[#ff0000]`
- The class scanner drives Tailwind output — only classes actually used in
  scanned sources produce CSS.

## Global CSS

Plain CSS imported or referenced in pages is collected, deduplicated at the
rule level, and written as a single hashed `styles.<hash>.css`.

## The CSS processing pipeline

1. **Collect** CSS from all pages.
2. **Merge** with deduplication (rule-level).
3. **Inline `@import`** recursively (circular-safe, depth limit 10).
4. **Minify** — 7 transforms:
   - Comment stripping
   - Whitespace collapsing
   - Hex color shortening
   - `rgba()` → hex
   - Zero-unit removal
   - `calc()` simplification
   - Duplicate declaration removal
5. **Hash-based filename** — `styles.<hash>.css`.

## Custom properties & theming

There's nothing special needed to use CSS custom properties — they pass through
the pipeline unchanged. The docs site uses a `:root[data-theme="dark"]` scheme
toggled at runtime for light/dark theming.
