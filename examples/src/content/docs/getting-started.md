---
title: Getting Started
order: 2
---

# Getting Started with Krate

This guide walks you through setting up and building your first Krate project.

## Prerequisites

- **Go 1.21+** — for building the compiler
- **Node.js 18+** — for running the runtime bundler
- **npx** — for executing config files and plugins

## Installation

Clone the repository and build the compiler:

```bash
git clone https://github.com/your-org/krate
cd krate/packages/compiler
go build -o bin/krate.exe ./cmd/krate/
```

## Creating a Project

Use the `init` command to scaffold a new project:

```bash
krate init my-project
cd my-project
```

This creates a `krate.config.ts` file with sensible defaults.

## Adding Pages

Pages live in `src/pages/`. Each file becomes a route:

- `src/pages/index.tsx` → `/`
- `src/pages/about.tsx` → `/about/`
- `src/pages/blog/post.tsx` → `/blog/post/`

## Building

```bash
krate build
```

Output goes to the `dist/` directory by default.

## Development Server

```bash
krate dev
```

Starts a dev server on `http://localhost:3000` with **hot reload**. Any file change triggers a targeted rebuild of only the affected pages, and the browser auto-refreshes.

### How Hot Reload Works

Krate uses **Server-Sent Events (SSE)** to push reload signals to the browser:

1. A file watcher monitors your project for changes
2. When a file changes, Krate's dependency graph identifies which pages are affected
3. Only those pages are rebuilt (not the entire project)
4. An SSE event is sent with the list of affected page routes
5. The browser checks if the current page is in the list and reloads if so

This means **large projects rebuild in milliseconds** — only the minimal set of pages is regenerated.

### Partial Reloads

Krate tracks dependencies at the file level. If you edit a shared component used by 3 pages, only those 3 pages are rebuilt. The dependency graph is built automatically during bundling — no manual configuration needed.

### Live Reload Script

In dev mode, Krate injects a small `<script>` tag into each page that connects to the SSE endpoint at `/__krate/hotreload`. The script is only 200 bytes and has zero impact on production builds.
