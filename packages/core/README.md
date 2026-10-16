# @krate/core

The Krate CLI and Go-native compiler.

`@krate/core` installs the `krate` binary (the Go compiler) plus a small Node
wrapper, and re-exports the config/plugin helpers used in `krate.config.ts`.

## Install

```sh
npm install -g @krate/core
# or as a dev dependency of an app
npm install -D @krate/core
```

## Commands

```sh
krate build      # build the site for production
krate dev        # dev server with hot reload
krate serve      # build, then serve for preview
krate types      # generate route/content TypeScript declarations
krate check      # run quality gates (a11y/SEO/perf)
krate plugin add <package>
krate mcp        # MCP server for editors/agents
krate version
```

Flags: `--config <path>`, `--out-dir <path>`, `--watch`, `--verbose`.

## Scaffolding

```sh
npx create-krate-app@latest my-app     # application
npx create-krate-docs@latest my-docs   # documentation site
```

## Config helpers

```ts
import { defineConfig, docs, sitemap } from "@krate/core";
```

See the [Krate docs](https://krate.js.org/docs/) for the full configuration
reference. Companion packages: [`@krate/runtime`](https://www.npmjs.com/package/@krate/runtime)
and [`@krate/components`](https://www.npmjs.com/package/@krate/components).

## License

Apache-2.0
