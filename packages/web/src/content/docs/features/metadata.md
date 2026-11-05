---
title: Metadata
description: The Next.js Metadata API - metadata and generateMetadata in pages and layouts, merged in Next.js order, with real tags in the first bytes of the head.
order: 6
---

# Metadata

A page declares its `<head>` tags with the Next.js Metadata API: an exported
`metadata` object, or an `async generateMetadata({ params, searchParams }, parent)`
that returns one. Layouts export them too. Krate resolves the chain from the root
layout down to the page and writes real tags into the `<head>` of the response, in
the initial HTML a crawler or link scraper reads, not patched in by script.

```tsx
// src/pages/_layout.tsx
import type { Metadata } from "@krate/runtime/server";

export const metadata: Metadata = {
  metadataBase: new URL("https://example.com"),
  title: { default: "Example", template: "%s | Example" },
  description: "The example site",
};
```

```tsx
// src/pages/blog/[slug].tsx
import type { Metadata, ResolvingMetadata } from "@krate/runtime/server";

export async function generateMetadata(
  { params }: { params: Promise<{ slug: string }> },
  parent: ResolvingMetadata,
): Promise<Metadata> {
  const { slug } = await params;
  const post = await fetch(`https://api.example.com/posts/${slug}`, { next: { tags: ["posts"] } }).then((r) => r.json());
  return {
    title: post.title, // "Hello | Example"
    description: post.summary,
    alternates: { canonical: `/blog/${slug}` },
    openGraph: { title: post.title, images: [post.cover] },
  };
}

export default async function Post({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  return <h1>{slug}</h1>;
}
```

## When it runs

| Page | `generateMetadata` runs | Tags are |
|------|-------------------------|----------|
| Static (SSG) | once at build time | baked into `index.html` |
| Static from `generateStaticParams` | once per generated entry (one run for all entries) | baked into each entry's HTML |
| Dynamic route that stays a template | at build time, with the route's placeholders; the server fills in each request's value | baked into the template |
| SSR, ISR, async page | on every render, next to the page, in the same request and cache scope | sent in the head of the response; an ISR entry stores them with the body |

At build time the chain runs in the embedded JavaScript engine and falls back to
Node when it needs `fetch` or a Node built-in (the `ssrRuntime` or `runtime` setting, as for
`generateStaticParams`). On a request-time page the build bakes nothing from a chain
that has a `generateMetadata`: the renderer resolves the whole chain per request, so
the layout and page parts always agree.

`generateMetadata` receives `{ params, searchParams }` that read both ways
(`params.slug` and `(await params).slug`) and a promise of the metadata resolved so
far. On a static page `searchParams` is empty.

Because it runs inside the render, it shares the page's data cache: a `fetch` with
`next: { tags, revalidate }` in `generateMetadata` and the same request in the page
reach the server once (concurrent identical cached requests are shared), the tags
are recorded on the page's ISR entry, and `revalidateTag` refreshes both. A
`searchParams` key it reads keys the ISR cache like one the page reads.

## Merge order

Segments are merged from the root layout to the page; a deeper segment wins. The
rules are Next.js's:

- A field a deeper segment sets **replaces** the parent's value as a whole.
  `openGraph: { title }` in a page drops the layout's `openGraph.siteName`; read the
  parent (`const { openGraph } = await parent`) to extend it.
- `title` as a string takes the nearest ancestor's `title.template` (`"%s | Example"`).
  `{ absolute }` ignores templates; `{ default, template }` in a layout sets the
  default title and the template for its descendants (the template does not apply to
  the layout's own `default`).
- `openGraph.title` and `twitter.title` have their own templates, declared as
  `openGraph: { title: { default, template } }`; the page `title.template` does not
  apply to them.
- `metadataBase` resolves relative URLs in `alternates`, `openGraph`, `twitter`,
  `icons` and `manifest`; its path is a prefix.
- An explicit `<Head>` on the page still wins over its metadata for the same tag, and
  metadata wins over a layout's own head markup and the site-wide `head` config.

On a request-time page the tags are merged into the head of the response by the same
rules `<Head>` uses (see [Built-in components](/docs/features/built-in-components/)):
a tag replaces a baked tag with the same `name`, `property` or `http-equiv`, a
`link rel="canonical"` replaces the canonical link, `og:image` may repeat.

## Supported fields

`title`, `description`, `applicationName`, `authors`, `generator`, `keywords`,
`referrer`, `creator`, `publisher`, `robots` (a string, or an object with
`googleBot`), `alternates` (`canonical`, `languages`, `media`, `types`), `openGraph`
(including `article` and `profile` fields, images, videos, audio), `twitter`,
`icons`, `manifest`, `themeColor`, `colorScheme`, `viewport`, `verification`,
`appleWebApp`, `formatDetection`, `category`, `itunes`, `appLinks`, `archives`,
`assets`, `bookmarks` and `other`.

A static `export const viewport = { width, initialScale, themeColor, colorScheme, ... }`
is read too (the page's viewport meta replaces the default one). `generateViewport`
is not supported.

## Errors

A `generateMetadata` that throws never costs the page. The error is logged with the
file, the page renders without that segment's metadata (the other segments still
count), and the response stays 200. At build time the same failure is warning
[`KR7122`](/docs/reference/diagnostics/), which also covers a metadata value that
cannot be computed at build time (a browser global, a service that is down); the
page still builds. Fix the cause, or render the page per request so
`generateMetadata` runs with the request.

`notFound()`, `forbidden()`, `unauthorized()` and `redirect()` called from
`generateMetadata` answer the request as they do from the page (404, 403, 401, a
redirect) on a request-time page.

## Limits

- A layout's `metadata` is evaluated for every page below it; a layout's
  `generateMetadata` runs for each of them too (per request on request-time pages).
- `generateViewport` and `export const dynamic` effects on metadata are not
  supported. Metadata files (`sitemap.ts`, `opengraph-image`, ...) are covered in
  [Metadata routes](/docs/features/metadata-routes/).
- A dynamic route served from its template cannot fetch per request. Use
  `generateStaticParams`, or make the page request-time.
