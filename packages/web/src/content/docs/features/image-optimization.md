---
title: Image Optimization
description: Compile-time, WebP-first responsive images with the Image component.
order: 3
---

# Image Optimization

`<Image>` is a compile-time image pipeline: it generates a responsive,
WebP-first `<picture>` for every image referenced in your pages, using a
pure-Go encoder with no native dependencies. There is no runtime image server,
no network requests to an optimizer, and nothing to configure per request.

## How it works

During `krate build`, `<Image>` compiles each source file into variants written
to `.krate/cache/images/` and copied to `<outDir>/_krate/images/` (served at
`/_krate/images/...`). The rendered markup is a `<picture>` element:

- **WebP + fallback pair** — every size is emitted twice: a
  `<source type="image/webp">` with a lossy WebP variant, and a `<source>` in
  the original codec (JPEG, or PNG when the source has transparency). The
  `<img>` falls back to the untouched original file for browsers without
  `<picture>` support.
- **Responsive `srcset`** — sources at breakpoints
  `640 / 768 / 1024 / 1280 / original-width` for both codecs, plus a `sizes`
  attribute you set per use site: `<Image sizes="(max-width: 768px) 100vw, 50vw" />`.
- **CLS mitigation** — `width` and `height` attributes plus an
  `style="aspect-ratio:W/H"` (the intrinsic ratio; an explicit `width` prop
  wins over the inferred height) reserve space before the image paints.
- **Lazy by default** — `loading="lazy"` with `decoding="async"`; add
  `priority` for `eager` loading with `fetchpriority="high"`.
- **Blur placeholder (LQIP)** — a 16px box-blurred, base64 data-URI rendered
  as the element's `background-image` until the real image loads
  (`placeholder="blur"`, the default). Use `placeholder="empty"` to disable.

## Props reference

The full prop surface is documented on the
[Built-in Components](/docs/features/built-in-components/) page under
`<Image>`. The essential controls are `src`, `width`, `height` (or
`width`/`quality` and the intrinsic ratio for the rest), `quality`,
`sizes`, `loading`, `priority`, and `placeholder`.

## Quality

`quality` (0–100, default depends on the source codec) controls the WebP
encode and, where applicable, the fallback re-encode. JPEG re-encodes for the
WebP variant only; the fallback stays lossless from the original for PNG
sources.

## Default OG image

Configured once in `krate.config.ts`, the SEO default image is used for
`og:image` on every page that does not already declare one in its `<Head>`:

```typescript
// krate.config.ts
export default defineConfig({
  seo: {
    baseUrl: "https://example.com",
    siteName: "Example",
    image: "/og-default.png",
  },
});
```

A relative `image` is resolved against `seo.baseUrl`; an absolute `http(s)`
URL is used as-is. Per-page `<Head>` markup always wins over the default, and
Krate never duplicates an `og:image` tag the page already emits.