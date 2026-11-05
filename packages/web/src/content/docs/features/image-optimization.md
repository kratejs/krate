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

- **WebP + fallback pair** - every size is emitted twice: a
  `<source type="image/webp">` with a lossy WebP variant, and a `<source>` in
  the original codec (JPEG, or PNG when the source has transparency). The
  `<img>` falls back to the untouched original file for browsers without
  `<picture>` support.
- **Responsive `srcset`** - without a `width`/`height`, sources at the
  `images.breakpoints` (default `640 / 768 / 1024 / 1280 / 1536`, then the
  original width) for both codecs, plus a `sizes` attribute you set per use
  site: `<Image sizes="(max-width: 768px) 100vw, 50vw" />`. With a `width` (or
  `height`) the image is shown at that size, so only 1x and 2x variants are made
  (`width={100}` gives 100w and 200w, never wider than the source) and `sizes`
  defaults to `"100px"`. An explicit `sizes` always wins.
- **Orientation** - a JPEG with an EXIF orientation (most phone photos) is
  rotated before it is resized, so the variants and the reported dimensions match
  what the browser shows for the original. Other EXIF data is not copied.
- **GIF** - a still GIF is converted like a PNG; an animated GIF is served
  unchanged (re-encoding would keep only the first frame).
- **CLS mitigation** - `width` and `height` attributes plus
  `style="max-width:100%;height:auto;aspect-ratio:W/H"` reserve space before the
  image paints. The image is never stretched past its `width`. With only
  `width` (or `height`) the other dimension follows the source ratio; with both,
  the image is cropped to that box with `object-fit:cover`. A `style` you pass
  is merged into the same attribute (yours wins).
- **Lazy by default** - `loading="lazy"` with `decoding="async"`; add
  `priority` for `eager` loading with `fetchpriority="high"`.
- **Blur placeholder (LQIP)** - a 16px box-blurred, base64 data-URI rendered
  as the element's `background-image` until the real image loads
  (`placeholder="blur"`, the default). Use `placeholder="empty"` to disable.

## Errors and warnings

`<Image>` problems fail the build with a `file:line:col` diagnostic and a hint,
instead of shipping a broken image:

- `KR8801` - the source file is missing or cannot be decoded.
- `KR8802` - a remote `src` whose host is not in `images.remotePatterns`.
- `KR8803` - `src`, `width`, `height`, `quality` or `priority` is not known at
  build time. A `const` initialised with a literal works; `alt`, `sizes` and
  `className` may be expressions.
- `KR8804` (warning) - no `alt`. Write `alt=""` for decorative images.

With a `basePath`, the generated `src` and `srcset` URLs are prefixed with it.

## Props reference

The full prop surface is documented on the
[Built-in Components](/docs/features/built-in-components/) page under
`<Image>`. The essential controls are `src`, `width`, `height` (or
`width`/`quality` and the intrinsic ratio for the rest), `quality`,
`sizes`, `loading`, `priority`, and `placeholder`.

## Quality

`quality` (0-100, default depends on the source codec) controls the WebP
encode and, where applicable, the fallback re-encode. JPEG re-encodes for the
WebP variant only; the fallback stays lossless from the original for PNG
sources.

## Default OG image

Configured once in `krate.config.ts`, the SEO default image is used for
`og:image` on every page that does not already declare one in its `<Head>`:

```typescript
// krate.config.ts
export default defineConfig({
  site: {
    url: "https://example.com",
    name: "Example",
    image: "/og-default.png",
  },
});
```

A relative `image` is resolved against `site.url`; an absolute `http(s)`
URL is used as-is. Per-page `<Head>` markup always wins over the default, and
Krate never duplicates an `og:image` tag the page already emits.