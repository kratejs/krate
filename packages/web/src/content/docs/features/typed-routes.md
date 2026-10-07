---
title: Typed Routes & Content
description: End-to-end TypeScript safety for links, route params, and content collections.
order: 12
---

# Typed Routes & Content

Krate knows every route and content entry at build time, so it generates
TypeScript declarations for them. `<Link href>`, dynamic route params, and
content collections become fully type-checked — with no configuration and no
runtime cost.

## Generated files

Every `krate build` (and the standalone `krate types` command) writes:

| File | Purpose |
|------|---------|
| `.krate/types/routes.d.ts` | `Route`, `StaticRoute`, `DynamicRoute`, `RouteParams`, and a `routes` manifest |
| `.krate/types/content.d.ts` | One `*Data` / `*Entry` interface per collection plus `ContentTypes` (only when collections are configured) |
| `src/krate-env.d.ts` | Bridge that augments `@krate/runtime` with the above |
| `src/krate-content.d.ts` | Ambient types for the virtual `krate/content` module |
| `.krate/gen/content.ts` | Codegen'd `krate/content` module (baked entries + `getCollection`) |

All of these are generated — add them to `.gitignore` and regenerate in CI. The
`.krate/` directory is ignored by default; add `krate-env.d.ts` and
`krate-content.d.ts` too.

## Typed links

The generated `Route` is a union of your static routes plus template-literal
types for dynamic ones:

```ts
export type StaticRoute = "/" | "/about" | "/docs/getting-started";
export type DynamicRoute = `/video/${string}`;
export type Route = StaticRoute | DynamicRoute;
```

Because `src/krate-env.d.ts` augments `@krate/runtime`, `<Link href>` and
`<a href>` get autocomplete for every route. Arbitrary URLs (external links,
hashes, query strings) are always accepted, so nothing that works today breaks.

```tsx
<Link href="/docs/getting-started">Get started</Link>
<Link href="/video/abc123">Watch</Link>
<Link href="https://example.com">External</Link>
```

## Typed route params

`RouteParams` maps each dynamic pattern to its params object:

```ts
export interface RouteParams {
  "/video/[id]": { id: string };
}
```

## Content collections

Declare collections in `krate.config.ts` under `content`. Krate validates every
entry's frontmatter against the schema and generates types for it:

```ts
// krate.config.ts
import { defineConfig, defineContent } from "@krate/core";

export default defineConfig({
  content: defineContent({
    blog: {
      dir: "src/content/blog",
      schema: {
        title: "string",
        description: "string",
        date: "date",              // authored as an ISO string
        tags: "string[]",
        draft: "boolean",
        order: { type: "number", required: true },
      },
    },
  }),
});
```

`defineContent` is an optional identity helper — a plain object works too:

```ts
export default defineConfig({
  content: {
    blog: { dir: "src/content/blog", schema: { title: "string" } },
  },
});
```

Field specs are either a shorthand type string (`"string"`, `"number"`,
`"boolean"`, `"string[]"`, `"number[]"`, `"date"`) or an object with `type` and
an optional `required` flag. Unknown fields are ignored; omitted optional fields
are fine.

A **required** field that is missing — or a value of the wrong type — fails the
build (and `krate types` exits non-zero):

```text
Content error: blog: src/content/blog/post.md: missing required field "order"
```

The generated declarations type each entry:

```ts
export interface BlogData {
  author?: string;
  date?: string;
  description?: string;
  draft?: boolean;
  order: number;      // required
  tags?: string[];
  title?: string;
}
export interface BlogEntry {
  slug: string;
  path: string;
  body: string;       // raw markdown
  html: string;       // rendered HTML
  data: BlogData;
}
```

## Reading content with `getCollection`

Import `getCollection` from the virtual `krate/content` module. It returns each
collection's entries as a typed array:

```tsx
import { getCollection } from "krate/content";

export default function BlogIndex() {
  const posts = getCollection("blog")
    .filter((p) => !p.data.draft)
    .sort((a, b) => a.data.order - b.data.order);

  return (
    <ul>
      {posts.map((post) => (
        <li><a href={`/blog/${post.slug}`}>{post.data.title}</a></li>
      ))}
    </ul>
  );
}
```

Because `getCollection` returns literal data at build time, it **folds into
static HTML** — the example above ships zero client JavaScript for the list.
Supported operations include `filter`, `sort`, `find`, `slice`, and `map`
(evaluated in the embedded engine at build time). Each entry exposes:

| Field | Description |
|-------|-------------|
| `slug` | Collection-relative path without extension (e.g. `"guides/intro"`) |
| `path` | Project-relative source path |
| `data` | Parsed frontmatter, typed from the schema |
| `body` | Raw markdown (frontmatter stripped) |
| `html` | Rendered HTML |

Render an entry's body with `dangerouslySetInnerHTML`:

```tsx
const post = getCollection("blog").find((p) => p.slug === "hello");
<div dangerouslySetInnerHTML={{ __html: post.html }} />
```

Named exports (`import { blog } from "krate/content"`) also work when a
collection name is a valid identifier.

## Entry pages with `[slug].tsx`

Collections are a data source; routes come from `src/pages`. To render one page
per entry, add a dynamic route and list the entries from
`generateStaticParams`:

```tsx
// src/pages/blog/[slug].tsx
import { getCollection } from "krate/content";

export function generateStaticParams() {
  return getCollection("blog").map((p) => ({ slug: p.slug }));
}

export default function BlogPost(props: { params?: { slug: string } }) {
  const post = getCollection("blog").find((p) => p.slug === props.params?.slug);
  if (!post) return <h1>Not found</h1>;
  return (
    <article>
      <h1>{post.data.title}</h1>
      <div dangerouslySetInnerHTML={{ __html: post.html }} />
    </article>
  );
}
```

`generateStaticParams` bakes one static page per entry (`/blog/hello-collections`,
…). The route parameter (`props.params.slug`) is substituted before rendering, so
the `find(...)` lookup folds to the matching entry at build time — no runtime
data access.

:::note
Both places that run user TypeScript at build time resolve `krate/content`: the
Go bundler for page/component code, and `npx tsx` for `generateStaticParams`
(via the generated `.krate/tsconfig.json`, which also carries your project's own
path aliases).
:::

## Static output & dynamic params

By default a dynamic route (`[slug].tsx`) keeps a **fallback template**, so any
URL matching the pattern is answered at request time. When the set of valid
params is closed, turn that off with `dynamicParams`:

```tsx
// src/pages/blog/[slug].tsx
export function generateStaticParams() {
  return getCollection("blog").map((p) => ({ slug: p.slug }));
}
generateStaticParams.dynamicParams = false;   // only the baked slugs are valid
```

With `dynamicParams = false`:

- The build emits **only** the concrete pages from `generateStaticParams`
  (`/blog/hello-collections`, …). The `[param]` fallback template is not written.
- Requests for other params (`/blog/nope`) **404**, with no server required — the
  output is safe to host on any static CDN.

The equivalent forms are `export const dynamicParams = false` or
`export const config = { dynamicParams: false }`.

### Site-wide static output

Add `output: "static"` to `krate.config.ts` to make the whole site static:

```ts
export default defineConfig({
  output: "static",
});
```

This does two things:

1. **Dynamic routes are closed by default** — every `[param]` route behaves as
   `dynamicParams = false` unless it opts back in with
   `export const dynamicParams = true`.
2. **Request-time rendering is disabled** — pages that would have been SSR, ISR,
   or streaming are built as plain static pages.

`output: "static"` is the way to guarantee that *only* statically renderable
routes exist in the build.

## The `krate types` command

Generate declarations without running a full build — useful in CI and editors:

```sh
krate types          # writes .krate/types + src/krate-env.d.ts + src/krate-content.d.ts
krate types ./site   # explicit project directory
```

A typical CI type-check:

```sh
krate types && tsc --noEmit
```

:::note
`krate types` discovers routes from `src/pages` and validates content
collections, but it does not run plugins. For sites whose pages are generated by
a plugin (for example the [docs plugin](/docs/features/plugins/)), run
`krate build` to include those routes in the generated declarations.
:::

## How it works

- Routes come from the page tree (`src/pages/**`), including plugin-generated
  pages and dynamic `[param]` segments. Error pages (`404`/`500`) are excluded.
- Collection entries are discovered under each collection's `dir`; frontmatter
  is parsed with the same mini-YAML parser used by the docs plugin, and each
  entry's body is rendered through the markdown pipeline.
- `getCollection(...)` chains are evaluated and inlined as literal arrays before
  rendering, so collection-driven lists and single-entry reads bake into the
  page.
- Route/params types are advisory: `krate-env.d.ts` imports them and augments
  the runtime. If generation fails, the build warns rather than stopping.
- Content **schema violations are build errors**, because they indicate a
  content bug.
