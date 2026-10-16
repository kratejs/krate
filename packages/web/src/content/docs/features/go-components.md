---
title: Go Components
description: Author server and runtime components in Go, importing arbitrary packages.
order: 6
badge:
  text: New
  variant: new
---

# Go Components

Server and runtime components can be written in **Go** instead of TSX. A Go
component is a plain function that receives props and returns markup (or a
string/number). It compiles into the same Go host binary as the
[Go API routes](/docs/features/api-routes/) and can import any Go package.

## Quick start

Go is **enabled by default** and looks in `src/components` and `src/api`, so if
that layout suits you there is no config to write. To use it:

1. **Install the Go toolchain** (1.21+). Go components are compiled with `go
   build` at build time; there is no prebuilt path.

2. **(Optional) configure** `krate.config.ts` only to customize paths or add
   modules:

   ```typescript
   import { defineConfig } from "@krate/core";

   export default defineConfig({
     go: {
       componentDirs: ["src/components"], // where *.go components live
       // apiDirs: ["src/api"],           // where Go API routes live
       // deps: [{ path: "github.com/google/uuid", version: "v1.6.0" }],
       // replaces: [{ from: "example.com/local", to: "../local" }],
     },
   });
   ```

   See [Configuration](/docs/reference/config/#go-components--api-routes) for
   every key.

3. **Set up editor intellisense** (one command). This writes a `go.mod` +
   `go.work`, vendors the helper module under `.krate/goext`, and adds
   `.krate/types` to your `tsconfig.json` `include`:

   ```sh
   krate go:init
   ```

   If you skip this you can still build, you just lose `goimports`/gopls
   completion for the helper import.

4. **Add a component** under `src/components` (see below), import it from TSX,
   and run `krate dev`.

## Authoring

A component file lives under a `go.componentDirs` directory (default
`src/components`) and exports a function whose tier is set by the file suffix or
a directive:

```go
// src/components/badge.server.go
package components

import kr "github.com/kratejs/krate/packages/compiler/goext/krate"

type BadgeProps struct {
    Label string   `json:"label"`
    Items []string `json:"items"`
    Count int      `json:"count"`
    Kids  kr.HTML  `json:"children"` // JSX children, pre-rendered to HTML
}

// krate:component server
func Badge(p BadgeProps) kr.Node {
    return kr.Span(
        kr.Class("badge"),
        kr.Text(p.Label),
        kr.Each(p.Items, func(it string) kr.Node { return kr.Li(nil, kr.Text(it)) }),
    )
}
```

- **Tier:** `*.server.go` renders at **build time** (static HTML, zero runtime
  artifact). `*.runtime.go` renders **per request** as a region. A
  `//krate:component server|runtime` directive overrides the suffix. A file with
  neither is a plain package file other components can import.
- **Props:** one struct (or pointer) argument. Fields decode from the JSX props
  by their `json` tag. Numbers, booleans, arrays, objects, and pointers map to
  the matching Go types.
- **Return:** `kr.Node` or `kr.HTML` (markup), `string`/number/`bool` (rendered
  as text), or `(T, error)`.
- **Children:** the JSX children of `<Badge>…</Badge>` are rendered to HTML and
  passed under the struct field tagged `json:"children"` (typed `kr.HTML`).
- **Escaping:** the builder escapes text and attribute values by default;
  `kr.HTML`/`kr.Raw` insert markup verbatim.

Files in the same directory form one Go package, so a component may call helpers
in a sibling file.

## Building markup

`goext/krate` is a small, stdlib-only builder. Text and attribute values are
escaped for you, and markup is composed as data rather than concatenated:

```go
func Card(p CardProps) kr.Node {
    return kr.Article(kr.Class("card", p.Tone),
        kr.H2(nil, kr.Text(p.Title)),
        kr.P(nil, kr.Text(p.Body)),
        kr.Ul(nil, kr.Each(p.Tags, func(t string) kr.Node {
            return kr.Li(kr.Attrs{"data-tag": t}, kr.Text(t))
        })),
    )
}
```

- **Elements:** `Div`, `Span`, `P`, `A`, `Ul`/`Ol`/`Li`, `H1`-`H6`, `Section`,
  `Article`, `Form`, `Button`, `Input`, `Img`, `Code`, `Pre`, and more, plus
  `kr.El(tag, attrs, children...)` for any other tag. Void elements (`Img`,
  `Input`, `Br`, `Hr`, `Link`, `Meta`, `Source`) take attributes only.
- **Attributes:** `kr.Attrs{"href": "/x", "hidden": true, "class": []string{"a"},
  "style": map[string]string{"color": "red"}}`. Values may be `string`, `bool`
  (bare attribute), `[]string` (space-joined), or `map[string]string` (`k:v;...`).
  Shorthands: `kr.Class("a", "b")` and `kr.Style("color", "red")`.
- **Text vs markup:** `kr.Text(s)` escapes, `kr.Raw(s)` and the `kr.HTML` type
  insert trusted markup verbatim, and `kr.URL(s)` escapes a URL path.
- **Grouping:** `kr.Frag(nodes...)`, `kr.Join(sep, parts...)`, and
  `kr.Each(items, fn)` (generics) build lists with no wrapper element.
- **Return type:** a component may return `kr.Node` or `kr.HTML` (both render to
  markup), and a Go component can call another Go component directly.

### Templates

For markup-heavy components you can use `html/template` with context-aware
auto-escaping, alone or alongside the builder:

```go
var card = kr.MustTemplate(`<article class="card"><h2>{{.Title}}</h2></article>`)

func Card(p CardProps) (kr.HTML, error) {
    return card.Render(p)
}
```

`kr.SafeHTML(node)` marks a value as trusted inside a template, and
`kr.NewTemplate(name, src)` returns a parse error instead of panicking.

## Importing from TSX

```tsx
import { Badge } from "@/components/badge.server.go";

export default function Page() {
  return <Badge label="Hi" items={["a", "b"]} count={2}>child</Badge>;
}
```

The compiler resolves the `.go` import, serializes the props to JSON, and either
inlines the HTML (server tier) or emits a request-time region (runtime tier).
Every `.go` import is a normal ES import — the `@/` alias and relative paths both
work.

## Runtime regions

`*.runtime.go` components run in the Go host per request. A page that imports
one is force-rendered as a streaming page: the static shell carries a region
marker and `krate serve` renders it via the host (over AF_UNIX) at request time,
splicing the HTML in - exactly like TSX runtime components, but on the Go host.
A runtime component may take a second `kr.Request` parameter to read the incoming
method, URL, route params, query, and headers:

```go
func Show(p ShowProps, r kr.Request) kr.HTML {
    return kr.HTML("<h1>" + kr.Escape(r.Param("id")) + "</h1>")
}
```

Add a `// krate:revalidate 60` comment to cache the region for 60 seconds (the
Go equivalent of `export const revalidate = 60`).

## TypeScript types

Krate generates ambient module declarations under `.krate/types/` so the `.go`
imports type-check in your editor. Add `.krate/types` to your `tsconfig.json`
`include` (the `create-krate-app` scaffold and `krate go:init` both do this):

```json
{ "include": ["src", ".krate/types"] }
```

Prop types come from the Go struct:

| Go | TypeScript |
|----|------------|
| `string` | `string` |
| `int`, `int64`, `float64` | `number` |
| `bool` | `boolean` |
| `[]T` | `T[]` |
| `map[string]T` | `Record<string, T>` |
| struct (by `json` tag) | interface |
| `kr.HTML`, `kr.Node` | `JSX.Element` |

`krate types` (or any build) regenerates them. A component type declared in a
sibling file in the same package resolves too.

## Intellisense

The helper packages live at real import paths
(`github.com/kratejs/krate/packages/compiler/goext/krate` and
`.../goext/runtime`) so `goimports`/gopls provide completion and type
information. `krate go:init` writes a project `go.mod` + `go.work` and vendors
the helper module under `.krate/goext` so those imports resolve offline too.
`krate doctor` reports whether the setup is complete.

> Note: files named with a route param (e.g. `[id].go`) are not valid Go
> filenames, so they cannot get editor intellisense even though Krate compiles
> them (it copies them to a sanitized name first).

## Troubleshooting

- **Import is red / no completion.** Run `krate go:init`, then reload your
  editor. Confirm `tsconfig.json` `include` lists `.krate/types` and a root
  `go.mod` exists.
- **"Go routes/components require the Go toolchain."** Install Go and make sure
  `go` is on `PATH`.
- **Prop is `unknown` / wrong type.** The props type must live in the component
  file or a sibling file of the same package; types from other packages map to
  `unknown`.
- **Editor sees `[id].go` as invalid.** Route-param filenames can't be Go files;
  rename is not possible without changing the route. Everything else still
  compiles.

## Configuration

See [Config Reference](/docs/reference/config/#go-components--api-routes) for the
`go` block (`enabled`, `module`, `deps`, `replaces`, `exclude`, `tidy`,
`componentDirs`, `apiDirs`, `apiEnabled`). Go components require the Go toolchain
at build time.
