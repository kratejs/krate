---
title: Metadata routes
description: sitemap.ts, robots.ts, manifest.ts, opengraph-image and twitter-image files, with ImageResponse drawing real PNGs at build time in pure Go.
order: 7
---

# Metadata routes

The Next.js metadata file conventions work as files in the pages directory. They
run once at build time (on Node, with your project's environment) and write
static files, so they work on every deploy target.

| File | Output |
| --- | --- |
| `sitemap.ts` | `sitemap.xml` (or `sitemap/<id>.xml` per `generateSitemaps` entry) |
| `robots.ts` | `robots.txt` |
| `manifest.ts` | `manifest.webmanifest` |
| `opengraph-image.tsx` | `opengraph-image.png`, wired into `og:image` |
| `twitter-image.tsx` | `twitter-image.png`, wired into `twitter:image` |

The extensions are `.ts`, `.js` or `.mjs` for sitemap, robots and manifest, and
`.ts`, `.tsx`, `.js` or `.jsx` for the image files. A file in a directory applies
to that URL path (`src/pages/blog/opengraph-image.tsx` writes
`/blog/opengraph-image.png`); route groups do not count towards the URL.

## sitemap, robots, manifest

The default export returns the same shapes as Next.js's `MetadataRoute`:

```ts
// src/pages/sitemap.ts
import type { MetadataRoute } from "@krate/runtime/server";

export default function sitemap(): MetadataRoute.Sitemap {
  return [{ url: "https://example.com/", lastModified: new Date(), changeFrequency: "weekly", priority: 1 }];
}
```

`robots.ts` returns `{ rules, sitemap, host }` and `manifest.ts` returns the web
app manifest fields (`name`, `short_name`, `icons`, `display`, ...). The function
may be async. `sitemap.ts` may also export `generateSitemaps()` to split a large
sitemap into `sitemap/<id>.xml` files; the function is then called once per id.

A metadata file wins over the sitemap and robots plugins and the `robots` and
`seo` config. The build says so (`KR7133`) so you can remove the one you no longer need.

## opengraph-image and twitter-image

Export a default function that returns an `ImageResponse`:

```tsx
// src/pages/blog/[slug]/opengraph-image.tsx
import { ImageResponse } from "@krate/runtime/og";

export const alt = "Post cover";
export const size = { width: 1200, height: 630 };

export default async function Image({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  return new ImageResponse(
    <div style={{ width: "100%", height: "100%", display: "flex", alignItems: "center", padding: 60, backgroundImage: "linear-gradient(135deg, #0f172a, #334155)", color: "white", fontSize: 88 }}>
      {slug}
    </div>,
    { width: 1200, height: 630 },
  );
}
```

- In a dynamic segment the file runs once per `generateStaticParams` entry of the
  segment's page. Without a matching page or `generateStaticParams` nothing is
  built and you get a `KR7132` warning.
- `alt`, `size` and `contentType` exports set `og:image:alt`, the width/height
  tags and `og:image:type`. `generateImageMetadata({ params })` returns a list of
  `{ id, alt, size, contentType }` for several images per segment; the default
  export then also receives `id`.
- A ready-made image (`opengraph-image.png`, `.jpg`, `.gif`, `.webp`) in place of
  the module is copied as it is.
- The generated URLs are added to the page's metadata automatically: `og:image`,
  `og:image:width`, `og:image:height`, `og:image:alt`, `og:image:type`,
  `twitter:image` and `twitter:image:alt`, and `twitter:card` becomes
  `summary_large_image`. They apply to the segment and everything below it, and the
  `metadata.openGraph.images` or `metadata.twitter.images` a segment sets itself wins there. Set
  `metadataBase` (or `seo.baseUrl`) so the URLs are absolute.

## What ImageResponse draws

The image is drawn by a renderer written in Go: no headless browser, no native
module and no JavaScript dependency. It supports a defined subset and refuses the
rest at build time. It is not a browser: do not expect arbitrary CSS to work.

**Elements.** `div`, `span`, `p`, `h1`-`h6`, `b`, `strong`, `i`, `em`, `small`,
`a`, `label`, `section`, `article`, `header`, `footer`, `main`, `nav`, `ul`, `ol`,
`li` and `img`. Every element is a flex box. Function components are called;
async components are not allowed. The `tw` prop is not supported.

**Layout.** `display` (`flex`, `none`), `flexDirection`, `flexWrap`,
`justifyContent`, `alignItems`, `alignSelf`, `flex`, `flexGrow`, `flexShrink`,
`flexBasis`, `width`, `height`, `min/maxWidth`, `min/maxHeight`, `margin*`,
`padding*`, `gap`, `rowGap`, `columnGap`, `position` (`relative`, `absolute`) with
`top`, `right`, `bottom`, `left`, and `overflow` (`visible`, `hidden`). Lengths are
numbers (px), `px`, `%`, `em`, `rem` or `auto`.

**Painting.** `backgroundColor`, `background` and `backgroundImage` with
`linear-gradient` (angles or `to` directions, colour stops) and `radial-gradient`,
`opacity`, `borderRadius` (and the four corner properties), `border`,
`borderWidth`, `borderColor`, `borderStyle: solid`, per-side borders and
`boxShadow` (outer shadows). Colours are hex, `rgb()`, `rgba()`, `hsl()`,
`hsla()` and a short list of named colours (`black`, `white`, `red`, `gray`, `navy`, ...); use hex for anything else.

**Text.** `color`, `fontFamily`, `fontSize`, `fontWeight`, `fontStyle`,
`lineHeight`, `letterSpacing`, `textAlign`, `textTransform`, `whiteSpace`
(`normal`, `nowrap`, `pre`, `pre-wrap`) and `textOverflow` (`clip`, `ellipsis`).
Text wraps inside its box.

**Fonts.** A built-in Go font (regular, bold, italic, bold italic; Latin
characters) is the default. Pass TrueType or OpenType files in `options.fonts`
(`{ name, data, weight, style }`) for other scripts or a brand face; `fontFamily`
picks them by name. A character with no glyph in any font is a build error, not a
blank box.

**Images.** `<img src>` with a PNG, JPEG, GIF or WebP from `public/`, the project
root or an `http(s)` URL (fetched at build time), sized with `width`/`height`
and `objectFit` (`fill`, `contain`, `cover`).

**Not supported** (each is reported by `KR7131` with its location): `display:
grid` and `inline-*`, `transform`, `filter`, `calc()`, `var()`, `vh`/`vw` units,
`position: fixed`/`sticky`, inset shadows, `repeating-*` and `conic` gradients,
SVG and `<canvas>`, CSS classes and stylesheets, async components, and emoji
without an emoji font.

## Per request

`ImageResponse` also works in a Node API route served by `krate start`
(`src/api/og.ts`): the route builds the response and the Krate host draws it
through an authenticated internal endpoint, with the same renderer and the same
errors. Runtimes without that host (a Cloudflare Worker, a serverless function) have
no renderer and the response fails with a message saying so; generate the image
at build time with an `opengraph-image` file instead.

## Next.js migration

`krate migrate nextjs` maps these files natively: `sitemap`, `robots`,
`manifest`, `opengraph-image` and `twitter-image` modules move to the same
segment of `src/pages` with `next/og` rewritten to `@krate/runtime/og`; static
`robots.txt`, `sitemap.xml` and manifest files go to `public/`; static
`opengraph-image.png` files are copied beside their page. `icon` and `apple-icon`
files, and `opengraph-image.alt.txt` (export `alt` instead), are still listed as
manual items.
