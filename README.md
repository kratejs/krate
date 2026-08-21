<div align="center">

# Krate

**A Go-native static site generator with signal-based reactivity.**

Krate compiles TSX/JSX pages into static HTML at build time and generates a tiny
hydration bundle that makes pages interactive on the client — no React, no
bundler subprocess, no Node.js required for core compilation.

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8.svg)](https://go.dev/)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

</div>

## Highlights

- **Go-native compiler** — custom lexer, parser, bundler, and renderer. Builds run in milliseconds.
- **Signals, not React** — fine-grained reactivity with `createSignal` / `createEffect` / `createMemo`.
- **SSG-first** — every page is pre-rendered to static HTML. Hydration binds signals to the DOM via `data-k`/`data-kh` markers.
- **File-based routing** — `src/pages/` maps to URLs, with nested routes, dynamic segments (`[param]`), and `_layout.tsx` layouts.
- **Component tiers** — static, client, server (`@server`), and runtime (`@runtime`, via embedded QuickJS) components in one page.
- **Full CSS pipeline** — CSS Modules (FNV-32a scoping), Go-native Tailwind, minification, `@import` inlining.
- **SSR, ISR & streaming** — `getStaticProps`, `getServerSideProps`, revalidate-based ISR, and Suspense-based streaming SSR.
- **No external bundler** — esbuild is only used for a few auxiliary tasks (API routes, runtime component bundles); core compilation is 100% custom Go.
- **SPA router** — client-side navigation with DOM tree reconciliation (state, focus, and scroll survive transitions).
- **Plugin system** — Go plugin hooks plus community plugins written in JavaScript, executed inside the embedded QuickJS runtime.
- **WASM-powered docs search** — the docs plugin ships a search bar with a docfind index embedded into a WASM module at build time, so search runs entirely client-side.

## Getting Started

> The Krate docs website lives in [`packages/web`](packages/web/) — a full
> documentation site (with WASM search) you can build and run as a reference.
> A feature-demo example lives in [`examples/`](examples/).

### Install the CLI

```sh
npm install -g @krate/core
# or
pnpm add -g @krate/core
```

### Create a project

```sh
mkdir my-app && cd my-app
krate init
```

Add a page:

```tsx
// src/pages/index.tsx
export default function Home() {
  return <h1>Hello, World!</h1>;
}
```

Build and run:

```sh
krate dev       # dev server with hot reload on http://localhost:3000
krate build     # production build → dist/
krate serve     # preview the production build
```

## Quick Tour

### Signals

```tsx
import { createSignal } from '@krate/runtime';

export default function Counter() {
  const [count, setCount] = createSignal(0);
  return (
    <div>
      <span>{count()}</span>
      <button onClick={() => setCount(c => c + 1)}>+</button>
    </div>
  );
}
```

### Data fetching

```tsx
export async function getStaticProps() {
  const res = await fetch('https://api.example.com/data');
  return { props: { data: await res.json() } };
}

export default function Page({ data }) {
  return <pre>{JSON.stringify(data, null, 2)}</pre>;
}
```

### Layouts

```tsx
// src/pages/_layout.tsx — wraps every page in the directory
export default function Layout({ children }) {
  return (
    <div>
      <nav><a href="/">Home</a></nav>
      <main>{children}</main>
    </div>
  );
}
```

### Dynamic routes

```tsx
// src/pages/video/[id].tsx → /video/abc123
export default function VideoPage({ params }) {
  return <h1>Video {params.id}</h1>;
}
```

### Built-in components

Built-in components (`Head`, `Link`, `Icon`, `Image`, `Script`, `Style`) are
recognized by the compiler by name — no import needed:

```tsx
export default function Page() {
  return (
    <>
      <Head>
        <title>My Page</title>
        <meta name="description" content="A cool page" />
      </Head>
      <Icon name="lucide:heart" />
      <Image src="/photo.jpg" width={800} alt="Photo" />
      <Link href="/about">About</Link>
    </>
  );
}
```

`<Image>` compiles to a **WebP-first `<picture>`**: lossy WebP variants generated
at build time (pure-Go, no cgo) with original-format fallback, responsive
`srcset`/`sizes`, lazy/eager loading, blur placeholders, and
`aspect-ratio`-based CLS mitigation.

`<Link>` renders an SPA-enabled `<a>` with Next.js-style behavior — prefetching
on hover/focus/viewport (default on), `replace`, `scroll`, hash scrolling, and
`aria-current` for the active route — while external links and ⌘/Ctrl/middle
clicks fall through to native browser behavior.

## Component Tiers

Components are classified into four tiers that determine how they're rendered:

| Tier | Client JS | Rendering |
|------|-----------|-----------|
| **Static** (`// @static` or `*.static.tsx`) | None | Evaluated at build time, pure HTML output |
| **Client** (default) | Yes (hydration) | SSR/SSG + client hydration |
| **Server** (`// @server` or `*.server.tsx`) | None | Evaluated at build time, HTML output only |
| **Runtime** (`// @runtime` or `*.runtime.tsx`) | None | Serve-time via embedded QuickJS, streamed through Suspense |

Detection priority: source directive → file convention → config name list →
directory membership → default (client).

## Configuration

`krate.config.ts` in your project root, type-checked with `defineConfig`:

```typescript
import { defineConfig, sitemap, docs } from '@krate/core';

export default defineConfig({
  entry: "src/index.tsx",
  outDir: "dist",
  pagesDir: "src/pages",
  minify: true,
  tailwind: { enabled: false, scanDirs: ["src"] },
  redirects: [{ source: "/old", destination: "/new", permanent: true }],
  plugins: [
    sitemap({ baseUrl: "https://example.com" }),
    docs({ contentDir: "content/docs", title: "Docs", search: { engine: "docfind" } }),
  ],
});
```

`defineConfig` gives you full type-checking of every supported key — unknown or
misspelled config options become compile errors. Community plugins follow the
same factory pattern: `import demoPlugin from './plugins/my-plugin'` then
`demoPlugin({ ... })`.

## CLI

| Command | Description |
|---------|-------------|
| `krate build [dir]` | Production build (`--watch`, `--out-dir`, `--config`) |
| `krate dev [dir]` | Build + dev server (port 3000) + hot reload |
| `krate serve [dir]` | Build + static HTTP server |
| `krate init [dir]` | Scaffold a config |
| `krate version` | Print version |

## Architecture

```
Source (.tsx/.ts/.md/.mdx)
        │
        ▼
    Lexer (tokenize) ──► Parser (AST) ──► Bundler (imports, CSS modules, React rewrite)
        │
        ▼
    Renderer (SSR: AST → HTML + signal/handler detection)
        │
        ▼
    Hydration Codegen (signals → JS bundle with data-k/data-kh bindings)
        │
        ▼
    Build Output (HTML + hashed JS + hashed CSS + manifest.json)
```

### Repo layout

```
packages/
  compiler/      Go compiler (lexer, parser, bundler, renderer, build pipeline)
  runtime/       Client runtime — signals, hydration, SPA router, resources, context
  components/    Built-in UI components (shadcn/ui-style)
  core/         npm CLI wrapper (@krate/core)
  web/           The Krate documentation website (this site)
  core-*/        Per-platform binaries (built in CI, published to npm)
examples/        Docs + feature demo site (also used by the integration tests)
scripts/         Build tooling (platform packages, docfind WASM, versioning)
```

The compiler embeds the WASM modules it needs for the docs search feature:
`packages/compiler/third_party/docfind` (vendored Microsoft docfind Rust
sources) and `packages/compiler/internal/docfind` (wazero-driven build +
search, plus the `go:embed`-ed WASM artifacts). Regenerate them with
`node scripts/build-docfind.mjs`.

## Status

Krate is early stage but actively developed. Core compiler, reactivity, SSR/ISR,
streaming, plugins, and the component library are functional. See the
[roadmap in AGENTS.md](AGENTS.md#roadmap--phase-7-nextjs-feature-parity) for
what's next.

## Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for the
development workflow, and check out [AGENTS.md](AGENTS.md) for a deep technical
reference on the compiler and runtime internals.

## License

[Apache-2.0](LICENSE) © kratejs
