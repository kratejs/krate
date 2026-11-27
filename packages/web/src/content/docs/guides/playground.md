---
title: Playground
description: Build Krate projects in the browser with the real compiler running as WebAssembly.
order: 23
---

# Playground

The [playground](/playground/) runs the Krate compiler in your browser. It is
the same Go code as `krate build`, compiled to WebAssembly and run in a Web
Worker, so what you see is what the CLI would build, with no server involved.

Open it, pick an example, and edit. Each change rebuilds after a short pause (or
press `Ctrl+Enter` / `Cmd+Enter` to rebuild now). The output panel shows:

- **Preview**: the built site, running live. Links between pages work.
- **HTML**, **JS**, **CSS**: the generated files. Turn off **Minify** to read the
  JavaScript.
- **Files**: every output file with its size and gzip size.
- **Diagnostics**: compiler errors and hints. Selecting one jumps to the line.
- **Console**: `console.log` and errors from the preview.

The status bar shows the build time and the gzipped size of the generated
JavaScript, which is a quick way to see what a change costs. The CSS signals
example, for instance, ships no script at all.

## Files

A project is a set of files under `src/`. Pages live in `src/pages/` (so
`src/pages/about.tsx` becomes `/about/`), and a `_layout.tsx` wraps them. Add,
rename and delete files with the controls beside the file tabs. Imports of
`@krate/runtime` and `@krate/components` resolve to the packages bundled with
the playground.

## What works in the browser build

- TSX pages, components, layouts and file-based routing
- Signals, effects, resources and the CSS signals primitives
- `@krate/components`
- CSS, CSS Modules and Tailwind
- Markdown pages
- Static output (SSG) with the same HTML, CSS and JavaScript the CLI produces

## What does not

The browser build has no operating system underneath it, so features that need
one are left out:

- `krate.config.ts`: the playground builds with default settings. Minify and
  dev mode are toggles in the toolbar.
- `generateStaticParams` and other build steps that run a JavaScript runtime
- Go server components, API routes, server actions, SSR, ISR and middleware
- Plugins and installing other npm packages
- Network access during the build

`fetch` calls in your code run in the preview as in any web page, so they work
as long as the target allows cross-origin requests.

## Sharing

**Share** (or `Ctrl+S` / `Cmd+S`) copies a link that contains the whole project.
The files are compressed with deflate and stored in the URL fragment
(`#code=...`), so nothing is uploaded and the link works anywhere. Long projects
make long links.

Because a shared link carries code, opening one does not run it. The project
loads into the editor and builds, but the preview starts only after you choose
**Run the preview**. Read the code first, as you would before running any script
you were sent.

Your work is also saved in the browser's local storage and restored the next time
you visit. **Reset** returns to the selected example, and a link such as
`/playground/#example=counter` opens an example directly.

## Demos

The [demos gallery](/demos/) is built from the same examples. Each demo page shows
its source and an **Open in playground** button that loads the matching
`demo-*` example (for instance `/playground/#example=demo-css-signals`), so you
can edit what you just used. The demos run on the static site with no server.

## How the preview works

The compiler's output refers to files by root-absolute URLs such as
`/styles.abc123.css`. The playground registers a service worker scoped to
`/playground/preview/` that serves the latest build for a per-tab session, so the
preview behaves like a real site without a server. If your browser blocks
service workers (some private windows), the preview shows a notice, and the
other tabs still work.

## Running it locally

`pnpm build` in `packages/web` builds the WebAssembly compiler when Go is
installed (`pnpm build:playground` rebuilds only that part). Without Go the site
still builds and the playground page explains that the compiler is unavailable.
