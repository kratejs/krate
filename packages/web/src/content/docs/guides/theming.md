---
title: Theming Components
description: Recolour @krate/components with CSS custom properties.
order: 18
---

# Theming Components

Every `@krate/components` style reads `--krate-*` CSS custom properties, so a
whole theme change is just a few declarations. You usually only need to set
**one** variable:

```css
:root {
  --krate-primary: #4f46e5;
}
```

Hover states, focus rings, active tabs/radios/toggles, and the compiler's
CSS-signal active colours are all **derived from `--krate-primary`**, so they
update automatically.

## Why overrides always work

The library's defaults are emitted at **zero specificity** (`:where(:root){...}`),
so any rule of yours - a plain `:root`, `[data-theme]`, a wrapper class, whatever -
wins no matter which stylesheet loads first. There is no ordering footgun.

:::tip
Put your overrides in a global stylesheet (e.g. `src/styles/global.css`). They
beat the bundle even if the bundle loads later.
:::

## Quick start

```css
:root {
  --krate-primary: #4f46e5;   /* accent: buttons, links, checkboxes, active state */
  --krate-radius: 10px;       /* corner radius */
}
```

Dark mode is handled per scheme; override it the same way if needed:

```css
@media (prefers-color-scheme: dark) {
  :root { --krate-primary: #6366f1; }
}
```

## Tokens

### Brand

| Token | Default | Derived? |
|-------|---------|----------|
| `--krate-primary` | `#c2410c` | - (set this) |
| `--krate-primary-hover` | primary darkened 15% | yes |
| `--krate-primary-fg` | `#ffffff` | no (text on primary) |
| `--krate-primary-text` | primary (light), primary lightened 30% (dark) | yes (primary as text on the page) |
| `--krate-focus-ring` | `var(--krate-primary)` | yes |
| `--krate-danger` / `--krate-danger-hover` | `#dc2626` / derived | hover derived |
| `--krate-danger-text` | `#b91c1c` light, `#f87171` dark | no |
| `--krate-success` / `--krate-success-text` | `#22c55e` / `#15803d` light, `#4ade80` dark | no |
| `--krate-warning` / `--krate-warning-text` | `#f59e0b` / `#854d0e` light, `#facc15` dark | no |

The default primary meets WCAG AA (4.5:1) with white text. If you choose a
lighter brand colour, also set `--krate-primary-fg` to a dark colour so filled
buttons and badges stay readable; use `--krate-primary-text` for primary-coloured
text on the page.

### Surfaces and text

| Token | Light | Dark |
|-------|-------|------|
| `--krate-bg` | `#ffffff` | `#0a0a0a` |
| `--krate-fg` (aliased by `--krate-text`) | `#0a0a0a` | `#fafafa` |
| `--krate-muted` (aliased by `--krate-text-muted`) | `#636363` | `#a3a3a3` |
| `--krate-surface` | `#f5f5f5` | `#171717` |
| `--krate-surface-hover` | `#e5e5e5` | `#262626` |
| `--krate-border` | `#e5e5e5` | `#262626` |
| `--krate-card-bg` | `#ffffff` | `#171717` |
| `--krate-overlay` | `rgba(9,9,11,.4)` | `rgba(0,0,0,.6)` |

### Layout and code

| Token | Default |
|-------|---------|
| `--krate-font` | `Inter, system-ui, ...` |
| `--krate-radius` / `--krate-radius-lg` | `8px` / `12px` |
| `--krate-code-bg` / `--krate-code-text` | unset: taken from the code theme |
| `--krate-code-title-bg` / `--krate-code-title-text` | unset: derived from the code colours |
| `--krate-code-bg-base` / `--krate-code-text-base` | `#f7f7f7` / `#262626` light, `#171717` / `#e5e5e5` dark |

## Light and dark

The defaults follow the OS via `prefers-color-scheme`, and can be forced with a
`data-theme` attribute on `<html>`:

```html
<html data-theme="dark">
```

`[data-theme="dark"]` / `[data-theme="light"]` win over the media query. Your own
overrides (zero-superseding) still win over both.

## Code blocks

`<Code>` and fenced blocks take their colours from the code theme
(`markdown.codeTheme`, `github` in light and `krate-dark` in dark by default), so
a block is light on a light page and dark on a dark one, including when the
page's mode is switched with `data-theme`. A block with no language has no theme
stylesheet and uses the `--krate-code-bg-base` and `--krate-code-text-base`
tokens, which match the default theme.

Set `--krate-code-bg` and `--krate-code-text` to retint every block; they
override the theme, and the title bar and copy button follow them:

```css
:root {
  --krate-code-bg: #fdf6e3;
  --krate-code-text: #586e75;
}
```

Keep the text above 4.5:1 on the background in both modes. Token colours
come from the theme, so override them only if you also pick the surface.

## Per-component tweaks

Any component class can be styled directly - the theme sets the baseline:

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

`bg-brand` then reads `var(--color-brand)`, so it follows `--krate-primary` at
runtime, including in dark mode.
