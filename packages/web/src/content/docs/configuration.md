---
title: Configuration
order: 3
description: The krate.config.ts entry point and a map of every option group.
---

# Configuration

Krate is configured with a `krate.config.ts` file at the project root, typed
with `defineConfig` from `@krate/core`. Every supported key is type-checked -
unknown or misspelled options become compile errors.

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
    docs({ contentDir: "src/content/docs", title: "Docs" }),
  ],
});
```

React syntax (`useState`, `useEffect`, `useRef`, JSX) is transpiled to Krate
signals and effects automatically - no config is required. See
[React Compatibility](/docs/guides/react-compatibility/).

## Validation

Beyond editor type-checking, the compiler validates the file when it loads it:

- An unknown key at any depth, including inside arrays of objects
  (`redirects[].permanant`), is reported as a warning and ignored.
- A value of the wrong type is an error that names the file, the key and the
  expected type, for example
  `krate.config.ts: devServer.port must be a number (got string)`.
- A `pagesDir` that does not exist (with no `entry` to fall back to) is an error:
  `pagesDir src/nope does not exist`.
- In `krate dev`, editing `krate.config.*`, `tailwind.config.*`, `tsconfig.json`
  or an `.env*` file reloads the configuration and restarts the dev server; an
  invalid edit is reported and the running server is left alone.

## What a config file can use

`krate.config.ts` runs in-process (esbuild plus an embedded JavaScript engine),
not under Node, so it sees a subset of the Node API:

- `process.env` holds the shell environment overlaid with the project's `.env`
  files (the shell wins), and `NODE_ENV` is `"development"` in `krate dev` and
  `"production"` otherwise unless the shell sets it. `process.cwd()` is the
  project root; `process.platform` and `process.argv` are available.
- `node:path`, `node:url`, `node:fs` (`readFileSync`, `existsSync`, `readdirSync`,
  `statSync`) and `node:os` (`platform`, `homedir`, `tmpdir`, `EOL`, `arch`,
  `type`, `cpus`). Relative `fs` paths resolve against the project root.
- `__dirname` and `__filename` for each module, `import.meta.url`,
  `module.exports` (alone or beside imports), top-level `await`, `URL`,
  `crypto.randomUUID` and a small `Buffer` (`from`, `alloc`, `concat`,
  `toString` as utf8, base64 or hex).

`fs` is read-only, and `statSync` returns only `isFile()`, `isDirectory()` and
`isSymbolicLink()` (no size or times). Any other built-in (`child_process`,
`crypto`, `net`, `zlib`, `fs/promises`, ...), with or without the `node:`
prefix, fails with an error that names it (`require("child_process")` reports
`Module not found: child_process`). The global `crypto.randomUUID()` works; it
is the import that does not. A named import of something the shims lack, such as
`import { createRequire } from "node:module"`, reports esbuild's
`No matching export in "krate-node:module"`. Compute such values in a script
and read them from the environment, or move the logic into a plugin. Packages
from `node_modules` are bundled as usual.

## Option groups

This page is a map; the [Config Reference](/docs/reference/config/) documents
every key and its default.

| Group | Key(s) | Reference |
|-------|--------|-----------|
| Build | `entry`, `outDir`, `pagesDir`, `publicDir`, `minify`, `sourcemap` | [Build](/docs/reference/config/#build) |
| Dev server | `devServer` | [Dev server](/docs/reference/config/#dev-server) |
| Rendering | `viewTransitions`, `ppr`, `ssr`, `output` | [SSR / Streaming](/docs/reference/config/#ssr-streaming) |
| CSS & Tailwind | `tailwind` | [Tailwind CSS](/docs/reference/config/#tailwind-css) |
| Markdown | `markdown` | [Markdown](/docs/reference/config/#markdown) |
| Content | `content`, `pathAliases`, `tsBaseDir` | [Content collections](/docs/reference/config/#content-collections) |
| Server | `server`, `basePath`, `redirects`, `rewrites` | [Server](/docs/reference/config/#server) |
| Security | `csp`, `cors` | [Content Security Policy](/docs/reference/config/#content-security-policy) |
| SEO | `seo`, `robots` | [SEO & robots](/docs/reference/config/#seo-robots) |
| Checks | `checks` | [Quality checks](/docs/reference/config/#quality-checks) |
| Plugins | `plugins` | [Plugins](/docs/reference/config/#plugins) |
