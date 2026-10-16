---
title: Theming Components
description: Recolour @krate/components with CSS custom properties.
order: 13
---

# Theming Components

Every `@krate/components` style reads `--krate-*` CSS custom properties, so a
whole theme change is just a few declarations. You usually only need to set
**one** variable:

```css
:root {
  --krate-primary: #6366f1;
}
```

Hover states, focus rings, active tabs/radios/toggles, and the compiler's
CSS-signal active colours are all **derived from `--krate-primary`**, so they
update automatically.

## Why overrides always work

The library's defaults are emitted at **zero specificity** (`:where(:root){…}`),
so any rule of yours — a plain `:root`, `[data-theme]`, a wrapper class, whatever
— wins no matter which stylesheet loads first. There is no ordering footgun.

:::tip
Put your overrides in a global stylesheet (e.g. `src/styles/global.css`). They
beat the bundle even if the bundle loads later.
:::

## Quick start

```css
:root {
  --krate-primary: #6366f1;   /* accent: buttons, links, checkboxes, active state */
  --krate-radius: 10px;       /* corner radius */
}
```

Dark mode is handled per scheme; override it the same way if needed:

```css
@media (prefers-color-scheme: dark) {
  :root { --krate-primary: #818cf8; }
}
```

## Tokens

### Brand

| Token | Default | Derived? |
|-------|---------|----------|
| `--krate-primary` | `#f97316` | — (set this) |
| `--krate-primary-hover` | primary darkened 15% | yes |
| `--krate-primary-fg` | `#ffffff` | no (text on primary) |
| `--krate-focus-ring` | `var(--krate-primary)` | yes |
| `--krate-danger` / `--krate-danger-hover` | `#ef4444` / derived | hover derived |
| `--krate-success` | `#22c55e` | no |
| `--krate-warning` | `#f59e0b` | no |

### Surfaces and text

| Token | Light | Dark |
|-------|-------|------|
| `--krate-bg` | `#ffffff` | `#09090b` |
| `--krate-fg` (aliased by `--krate-text`) | `#09090b` | `#fafafa` |
| `--krate-muted` (aliased by `--krate-text-muted`) | `#71717a` | `#a1a1aa` |
| `--krate-surface` | `#f4f4f5` | `#18181b` |
| `--krate-surface-hover` | `#e4e4e7` | `#27272a` |
| `--krate-border` | `#e4e4e7` | `#27272a` |
| `--krate-card-bg` | `#ffffff` | `#18181b` |
| `--krate-overlay` | `rgba(9,9,11,.4)` | `rgba(0,0,0,.6)` |

### Layout and code

| Token | Default |
|-------|---------|
| `--krate-font` | `Inter, system-ui, …` |
| `--krate-radius` / `--krate-radius-lg` | `8px` / `12px` |
| `--krate-code-bg` / `--krate-code-text` | `#18181b` / `#fafafa` |

## Light and dark

The defaults follow the OS via `prefers-color-scheme`, and can be forced with a
`data-theme` attribute on `<html>`:

```html
<html data-theme="dark">
```

`[data-theme="dark"]` / `[data-theme="light"]` win over the media query. Your own
overrides (zero-superseding) still win over both.

## Per-component tweaks

Any component class can be styled directly — the theme sets the baseline:

```css
.krate-button-primary { border-radius: 9999px; }
.krate-card { box-shadow: 0 8px 30px rgba(0, 0, 0, 0.08); }
```

## CSS signals

Active tabs, selected radios, and pressed toggles use `--krate-css-active`
(falling back to `--krate-primary`) and the derived `--krate-active*` colours, so
changing `--krate-primary` recolours them too. To style the active state
independently, set `--krate-css-active` (and optionally
`--krate-css-active-fg`).

## With Tailwind

Because the tokens are plain CSS variables, Tailwind v4's `@theme` can consume
them:

```css
@theme {
  --color-brand: var(--krate-primary);
}
```
