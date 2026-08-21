---
title: Configuration
order: 3
---

# Configuration

Krate uses a `krate.config.ts` file at the project root for configuration.

## Basic Config

```typescript
export default {
  entry: "src/index.tsx",
  outDir: "dist",
  pagesDir: "src/pages",
  publicDir: "public",
  minify: true,
};
```

## All Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `entry` | string | `src/index.tsx` | Entry point for shared code |
| `outDir` | string | `dist` | Output directory |
| `pagesDir` | string | `src/pages` | Pages directory |
| `publicDir` | string | `public` | Static files directory |
| `minify` | bool | `true` | Enable all minification |
| `minifyHTML` | bool | â€” | HTML minification |
| `minifyCSS` | bool | â€” | CSS minification |
| `minifyJS` | bool | â€” | JS minification |
| `emitReact` | bool | `false` | Emit React-compatible JSX |
| `sourcemap` | bool | `false` | Generate source maps |

## Markdown Configuration

```typescript
export default {
  markdown: {
    gfm: true,
    headingAnchors: true,
    admonitions: true,
    codeHighlight: true,
  },
};
```

## Tailwind CSS

```typescript
export default {
  tailwind: {
    enabled: true,
  },
};
```
