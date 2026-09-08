---
title: Creating Plugins
order: 6
---

# Creating Plugins

Krate exposes a single plugin interface with **9 hooks** across two runtimes:
**JavaScript** community plugins (run in the embedded QuickJS runtime) and
**Go** plugins (run as a subprocess over HashiCorp `go-plugin`). This guide
covers authoring both.

## The 9 hooks

| Hook | When | Mutable context |
|------|------|-----------------|
| `BeforeBuild` | Once, before any pages are compiled | Files, GeneratedPages, Routes |
| `AfterParse` | After a page's source parses | Program (full AST), page |
| `AfterMarkdownParse` | After markdown/MDX renders to HTML | HTML, Title, Route |
| `AfterRender` | After a page renders, before layout wrap | HTML, HeadHTML, RawCSS, MetaTags |
| `GenerateRoutes` | After `BeforeBuild` | Routes (virtual pages) |
| `AfterPage` | After a page is fully built | HTML, OutName |
| `AfterBuild` | Once, after every page is built | Pages, CSS |
| `ServeRequest` | Request time, before a page is served | Action, NewURL, Status, Body, Headers |
| `ServeResponse` | Request time, after a non-streaming response is buffered | Status, Headers, Body |

Build hooks (`BeforeBuild`..`AfterBuild`) run during `krate build`. The two
serve hooks run inside the dev server (`krate dev`) at request time — the
same interceptor pipeline as middleware.

Serve hooks are **non-streaming only**: `ServeResponse` runs on buffered
responses; a streaming response (signalled by a `Flush()`) passes through
unchanged.

## JavaScript plugins (QuickJS)

A community plugin is a plain JS/TS module executed inside Krate's embedded
QuickJS runtime — no subprocess. It lives in local project code and is wired in
from `krate.config.ts`. Modules may be `.js`, `.mjs`, `.cjs`, or TypeScript
`.ts`/`.tsx` — esbuild transpiles TS before the bundle runs in QuickJS, and a
plugin *directory* resolves `index.ts` as its entry point just like `index.js`:

```ts
import demoPlugin from './plugins/krate-plugin-demo';
export default {
  plugins: [demoPlugin({ greeting: 'Hi' })],
};
```

```ts
// plugins/krate-plugin-demo/index.ts
import { definePlugin, definePluginHooks } from '@krate/plugin';
import type { Krate, PluginOutput } from '@krate/plugin';

export const hooks = definePluginHooks({
  BeforeBuild(ctx, options, krate: Krate): PluginOutput {
    return { files: [{ path: 'demo-notice.txt', content: 'hi' }] };
  },
  AfterRender(ctx, options, krate: Krate): PluginOutput {
    return {
      html: '<b>' + ctx.html,
      headHTML: '<meta name=generator content=demo>',
      rawCSS: '.demo{}',
    };
  },
  // A serve hook mutates the incoming request or outgoing buffered response.
  ServeRequest(ctx) {
    if (ctx.path === '/__demo') {
      return { action: 'respond', status: 200, body: 'hello', headers: {} };
    }
    return { action: 'continue' };
  },
  ServeResponse(ctx) {
    return { body: ctx.body + '<!-- served -->', headers: { ...ctx.headers, 'x-demo': '1' } };
  },
});

export default function demoPlugin(options: { greeting?: string } = {}) {
  return definePlugin({
    name: 'demo',
    order: 10,
    module: typeof import.meta !== 'undefined' && import.meta.url ? import.meta.url : '',
    options,
  });
}
```

- **`@krate/plugin`** — import the SDK package for typed contexts, outputs, and
  descriptors. `definePluginHooks` type-checks every hook's `ctx`; `definePlugin`
  types the factory's descriptor; `Krate` types the `{ root, outDir, version }`
  third argument. The helpers are compile-time only and erase to nothing when
  the plugin is bundled for QuickJS, so plain `.js` plugins keep working without
  the package.
- **Signature** — every hook receives `(ctx, options, krate)`: `ctx` is the
  JSON-serialized context (lowercase fields like `ctx.html`, `ctx.page`,
  `ctx.outName`, `ctx.headHTML`, `ctx.rawCSS`), `options` is the per-plugin
  options object, and `krate` is `{ root, outDir, version }`.
- **Return value** — hooks return `{ files, routes, generatedPages, html,
  headHTML, rawCSS, scripts, metaTags, data }` (all optional; may be a
  Promise). `files` are written to the output dir, `routes` become static HTML
  pages, `generatedPages` enter the normal page pipeline, and `html` /
  `headHTML` / `rawCSS` / `scripts` / `metaTags` mutate the build.
- **Serve hooks** — `ServeRequest` returns `{ action }` where `action` is
  `'continue'` (default), `'rewrite'` (with `newURL`), or `'respond'` (with
  `status`/`headers`/`body`). `ServeResponse` returns `{ status, headers,
  body }`; return only the fields you want to change. Async serve hooks are
  rejected — they must be synchronous.
- **Capabilities** — bundled plugins can use `import fs from 'fs'` and `import
  path from 'path'` (polyfilled), plus Web API polyfills (`fetch`, `URL`,
  `Headers`, `Response`, `TextEncoder`, timers, `process.env`). Non-relative
  third-party imports are left external and unavailable.

### AST editing in JavaScript

At `AfterParse`, `ctx.program` is the **kind-tagged AST document** (the same
`astjson` encoding the Go SDK uses) — a fully traversable JSON tree where every
node has a `kind` discriminator plus lowercase-cased fields (position is
`{ line, col }`). Mutate it and return it as `ast`:

```javascript
export const hooks = {
  AfterParse(ctx, options, krate) {
    // Depth-first search for a JSX text node.
    const walk = (node) => {
      if (!node || typeof node !== 'object') return null;
      if (node.kind === 'JSXText' && node.value === 'GO-PLUGIN') return node;
      for (const k in node) {
        for (const v of [].concat(node[k])) { const r = walk(v); if (r) return r; }
      }
      return null;
    };
    const el = walk(ctx.program);
    if (el) el.value = 'edited in JS';
    return { ast: ctx.program }; // decode the edited doc back into the build
  },
};
```

- The AST is the exact same shape Go plugins receive — **JS plugins can do
  everything Go plugins can**, just slower (the JSON round-trip climbs per
  page; Go plugins mutate the live tree in-process).
- Return **`ast`** (not `program`) as the output key; it is decoded by the host
  into the program Krate renders. Re-encoding and structural edits are supported;
  returning an invalid doc is a build error.

See `examples/plugins/krate-plugin-demo/index.ts` for a complete, type-checked
implementation of every build hook.

## Go plugins (go-plugin)

A Go plugin is a standalone Go program (its own `main` package and `go.mod`)
that calls `plug.Serve` with a `plug.Hooks` value. Krate launches it as a
subprocess, keeps it alive across dev hot-reloads, and kills it on teardown.

It relies on the public SDK module
`github.com/kratejs/krate/packages/compiler/pluginsdk` (package `plug`), which
re-exports the plugin context types and the go-plugin bridge.

```go
// main.go
package main

import (
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/pluginsdk"
)

func main() {
	plug.Serve("my-plugin", plug.Hooks{
		BeforeBuild: func(ctx *plug.BuildArgs) error {
			ctx.Files = append(ctx.Files, plug.File{
				Path:    "notice.txt",
				Content: "generated",
			})
			return nil
		},
		AfterParse: func(ctx *plug.ParseArgs) error {
			// ctx.Program is a live *ast.Program; edit it in place.
			return nil
		},
		AfterRender: func(ctx *plug.RenderArgs) error {
			ctx.HeadHTML += `<meta name="generator" content="my-plugin">`
			return nil
		},
		ServeRequest: func(ctx *plug.ServeRequestArgs) error {
			if ctx.Path == "/__ping" {
				ctx.Action = "respond"
				ctx.Status = 200
				ctx.Body = "pong"
			}
			return nil
		},
		ServeResponse: func(ctx *plug.ServeResponseArgs) error {
			ctx.Body += "<!-- served -->"
			return nil
		},
	})
}
```

- **Contexts are pointer args** — mutations propagate back to Krate. This is
  why each hook fn takes `*plug.BuildArgs`, `*plug.ParseArgs`, etc. rather than
  returning a result object.
- `**AfterParse**` — `ctx.Program` is a real `*ast.Program` (decoded from a
  kind-tagged JSON document by the host). Edit the tree directly; Krate renders
  your edits. Related types live in the public
  `github.com/kratejs/krate/packages/compiler/ast` module.
- Every context embeds `plug.Result`, so a hook can also set `Files`, `Routes`,
  `HeadHTML`, `RawCSS`, `Scripts`, `MetaTags`, etc. on its context.

### Building and distributing

The SDK (`github.com/kratejs/krate/packages/compiler/...`) is a fetchable Go
module, so a plugin's `go.mod` can `require` it directly with no local
checkout:

```go
require (
	github.com/hashicorp/go-hclog v0.14.1
	github.com/hashicorp/go-plugin v1.6.3
	github.com/kratejs/krate/packages/compiler vX.Y.Z
)
```

### Publishing your SDK version

Every Krate release (`vX.Y.Z` tag pushed to the repo) automatically publishes
the compiler module to the Go module proxy. Tagging a new version is all that's
needed — a GitHub Release that starts with a `v` triggers the process. Because
the module lives in a subdirectory, Go requires **prefixed version tags**
(`packages/compiler/vX.Y.Z`); the release workflow creates that tag for you at
the same commit as the release.

Version semantics follow Go's rules:

- `go get github.com/kratejs/krate/packages/compiler@vX.Y.Z` fetches an exact
  version.
- At **major version 2 or higher** the module path gains the major version
  suffix, as Go requires: `go get github.com/kratejs/krate/packages/compiler/v2@v2.0.0`.
  The tag stays `packages/compiler/v2.0.0` — the `/vN` suffix appears only in
  the module path, never in the tag.
- `@latest` resolves to the highest **release** version once one exists.
- While only **prereleases** are published (e.g. `v0.3.0-beta.1`), `@latest`
  resolves to the highest prerelease. Once any stable version is tagged,
  `@latest` stops tracking betas — users must request a prerelease by exact
  version (`@v0.3.0-beta.1`).
- Consumers need Go at least the version in the module's `go` directive (or a
  recent toolchain with `GOTOOLCHAIN=auto`).

Build one binary per platform and ship them in an npm package. The package's
`index.js` is a **static descriptor factory** that reports the `runtime` and
the per-platform binary paths (resolved relative to the package root). Set
`module` to the manifest's own URL so Krate can resolve those relative paths
(`import.meta.url` is a `file://` URL in ESM; the compiler converts it to a
filesystem path):

```javascript
module.exports = function () {
  return {
    name: 'my-plugin',
    order: 10,
    module: (typeof import.meta !== 'undefined' && import.meta.url) ? import.meta.url : '',
    runtime: 'go',
    hooks: { AfterRender: null, ServeRequest: null, ServeResponse: null },
    binaries: {
      'windows-amd64': 'bin/my-plugin-windows-amd64.exe',
      'darwin-amd64': 'bin/my-plugin-darwin-amd64',
      'darwin-arm64': 'bin/my-plugin-darwin-arm64',
      'linux-amd64': 'bin/my-plugin-linux-amd64',
      'linux-arm64': 'bin/my-plugin-linux-arm64',
    },
  };
};
```

Krate discovers the manifest by running this factory once (bundled with esbuild
into QuickJS), picks the binary matching the host `GOOS-GOARCH`, and spawns it.

Krate ships a cross-compile script that builds every binary listed in your
manifest:

```
node scripts/build-go-plugin.mjs <plugin-dir>
```

It reads the `binaries` map from `index.js` and runs `go build` per platform
(with `CGO_ENABLED=0`), placing the results in `<plugin-dir>/bin`.

Go plugin binaries are **trusted dependencies**: they run as a subprocess with
full filesystem and network access. Only install plugins from authors you trust.

See `examples/plugins/krate-plugin-demo-go/` for a complete Go example with a
per-platform manifest and an `index.js` descriptor.

## Runtime notes

- **Ordering** — plugins run in `order` sequence (lower first), so a plugin can
  observe the output of earlier plugins.
- **Lifecycle** — build plugins all run inside `krate build`; Go plugin
  subprocesses are launched lazily, reused across hot reloads, and closed when
  the build/serve session ends.
- **Serve hook scope** — both serve hooks run for non-streaming responses. If a
  handler streams, `ServeResponse` is skipped and the response passes through
  unchanged.
