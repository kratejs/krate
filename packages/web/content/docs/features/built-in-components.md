---
title: Built-in Components
order: 2
---

# Built-in Components

Krate provides a set of built-in components that are recognized by the compiler
by name — they work without any import:

| Component | Usage | Description |
|-----------|-------|-------------|
| `<Head>` | `<Head><title>…</title></Head>` | Injects content into `<head>` |
| `<Script>` | `<Script src="/app.js"/>` or `<Script>{inline}</Script>` | External or inline script in `<body>` |
| `<Style>` | `<Style>{css}</Style>` | Inline `<style>` in `<head>` |
| `<Link>` | `<Link href="/about">About</Link>` | SPA-enabled `<a>` |
| `<Icon>` | `<Icon name="lucide:menu"/>` | SVG from Iconify API (compile-time) |
| `<Image>` | `<Image src="/photo.jpg" width={800}/>` | Responsive `<picture>` (compile-time) |

## `<Head>`

```tsx
<Head>
  <title>My Page</title>
  <meta name="description" content="A cool page" />
</Head>
```

Content inside `<Head>` is hoisted into the page's `<head>`. This works at any
component depth, including inside layouts and plugin-generated pages.

## `<Script>`

```tsx
<Script src="/app.js" />
<Script>{`console.log('inline');`}</Script>
```

External scripts are emitted as `<script src>` in the body; inline children are
written raw (never HTML-escaped).

## `<Style>`

```tsx
<Style>{`.my-class { color: red; }`}</Style>
```

Inline CSS is captured into `<head>`.

## `<Link>`

`<Link>` renders a real `<a>` wired for SPA navigation via `initRouter`:

```tsx
<Link href="/about">About</Link>
<Link href="/faq" prefetch={false}>No prefetch</Link>
<Link href="/docs" replace>Replace history entry</Link>
<Link href="/blog" scroll={false}>No scroll-to-top</Link>
<Link href="https://example.com">External</Link>
<Link href="/new" target="_blank">New tab</Link>
```

- **Prefetching** (default on) — `data-prefetch` triggers a prefetch on
  hover/focus (debounced) and on viewport entry (`IntersectionObserver`,
  `rootMargin: 200px`).
- **`replace`** — swaps the history entry instead of pushing.
- **`scroll`** — scrolls to top on forward navigation by default.
- **Hash links** — `href="/about#section"` fetches the page then scrolls to
  the element.
- **Modified clicks** — ⌘/Ctrl/Shift/Alt-click and middle-click are never
  intercepted.
- **External links** — `http(s)://`, `//`, `mailto:`, `tel:`, `#`,
  `target="_blank"` and `download` become plain anchors the router ignores.
  `target="_blank"` auto-adds `rel="noopener noreferrer"`.
- **Attributes** — `className` (→ `class`), `target`, `rel`, `title`, `id`,
  `aria-label`, and other static attributes are forwarded.
- **Active link** — after navigation, the link matching the current path gets
  `aria-current="page"`.

## `<Icon>`

```tsx
<Icon name="lucide:heart" />
<Icon name="menu" />          // from the local icons/ registry
```

- **Name resolution** — `icons/<name>.svg` in the project root (local, no
  network) or `set:name` from the Iconify API (e.g. `lucide:heart`).
- Local SVGs keep their own `viewBox` (fallback `0 0 24 24`); user attributes
  override defaults.
- SVG markup is **sanitized** before embedding (scripts, comments, and
  `on*`/`javascript:`/`data:` attributes stripped).
- Fetched SVGs are cached on disk (`.krate/cache/icons/`); icon names are
  validated against `[a-zA-Z0-9_.-]` (max 64 chars).

## `<Image>`

```tsx
<Image
  src="/hero.jpg"        // resolved against public/
  width={800}            // display width (optional; intrinsic used if omitted)
  alt="Hero"
  loading="lazy"         // "lazy" (default) | "eager"
  priority               // eager + fetchpriority="high"
  quality={82}           // WebP/fallback quality (0-100)
  sizes="(max-width: 768px) 100vw, 50vw"
  placeholder="blur"     // "blur" (default) | "empty"
/>
```

`<Image>` compiles to a **WebP-first `<picture>`** at build time (pure-Go
encoder, no cgo):

- **WebP + fallback** — every variant is emitted twice: a
  `<source type="image/webp">` (lossy WebP via a pure-Go encoder) and a
  `<source>` in the original codec (JPEG, or PNG when the source has
  transparency). The `<img>` falls back to the original file for browsers
  without `<picture>`.
- **Responsive** — a `srcset` at breakpoints (640/768/1024/1280/original
  width) for both codecs, plus `sizes`.
- **CLS mitigation** — `width`/`height` attributes and `aspect-ratio:W/H`
  (intrinsic ratio; width wins if props mismatch) reserve space.
- **Blur placeholder (LQIP)** — a 16px box-blurred, base64 data-URI as the
  `background-image` until the real image loads.
- **Loading** — `loading="lazy"` by default; `priority` forces `eager` +
  `fetchpriority="high"`; `decoding="async"` always set.
- **Output** — variants written to `.krate/cache/images/` and copied to
  `<outDir>/_krate/images/` (served at `/_krate/images/...`).
