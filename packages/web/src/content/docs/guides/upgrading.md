---
title: Upgrading
order: 17
description: Keep Krate, @krate/runtime, and @krate/components in sync and upgrade safely.
---

# Upgrading

Krate ships as several packages that must stay on the same minor:

- `@krate/core` - the CLI + Go compiler
- `@krate/runtime` - signals, DOM, router, server renderer
- `@krate/components` - the built-in component library

## Upgrading a project

```sh
npm install @krate/core@latest @krate/runtime@latest @krate/components@latest
```

`@krate/core` declares an `@krate/runtime` peer; a version mismatch prints a
warning. Upgrade them together.

Then rebuild and check:

```sh
krate build
krate check
```

## From 0.4 to 0.5

0.5 changes several defaults and hardens the compiler and the servers. Most
projects need one edit (`tsconfig.json`); the rest is a list of things to check.
In order, with the one that bites first at the top:

| # | What changed | How you notice | What to do |
|---|--------------|----------------|------------|
| 1 | tsconfig `baseUrl` is honoured | `cannot resolve import "@/..." (matched tsconfig path alias)` | `krate fix --apply` |
| 2 | Tailwind output follows v4 | Rings, shadows, blurs, outlines and colours look different; a reset appears | [Rename classes](#tailwind-output-follows-v4), or `tailwind.preflight: false` |
| 3 | `krate dev` binds `127.0.0.1` | Connection refused from a container, WSL or another machine | `krate dev --host 0.0.0.0` |
| 4 | `krate start` no longer builds | `no build output found in dist` | Run `krate build` first, or use `krate serve` |
| 5 | `tailwind.config.*` runs in the config sandbox, not under `npx tsx` | A Tailwind config that used a Node API fails to load | [Check the API subset](#config-files-run-in-a-sandbox) |
| 6 | Output layout | A deploy script cannot find `dist/manifest.json` | Read `dist/.krate/manifest.json` |
| 7 | New hard errors | A build that passed on 0.4 fails | [Per-code table](#new-build-errors) |
| 8 | `bin/krate` is gone from `@krate/core` | `krate: command not found`, or a script runs `node_modules/@krate/core/bin/krate` | Use `krate`, `npx krate` or `node_modules/.bin/krate` |
| 9 | Server hardening | Middleware runs on more URLs; proxy headers read differently | [See below](#server-behaviour-changes) |

### `tsconfig.json`: `baseUrl` is strict

This is the change that cost people hours, so it comes first.

0.4 ignored `baseUrl` when it resolved `paths` aliases such as `@/*`. The 0.4
project template shipped with

```json
"baseUrl": "./src",
"paths": { "@/*": ["./src/*"] }
```

which `tsc` reads as `src/src/*` and the 0.4 compiler read as `src/*`. From 0.5
Krate resolves `paths` exactly as `tsc` does: **targets are relative to
`baseUrl`**. With the template above every `@/` import fails:

```text
Error (src/pages/index.tsx): bundling page src/pages/index.tsx:1:1: cannot resolve import "@/components/Counter" (matched tsconfig path alias "@/*")
    hint: no file exists at the alias target; targets resolve relative to compilerOptions.baseUrl (/app/src). If baseUrl is "./src" and the targets also start with ./src/, run `krate fix --apply` to set baseUrl to "."
```

The fix is to make `baseUrl` the project root:

```json
"baseUrl": ".",
"paths": { "@/*": ["./src/*"] }
```

`krate fix` does this for you when it is unambiguous: `baseUrl` names a
directory, every `paths` target starts with that directory, and the targets
exist under the project root but not under `baseUrl`.

```sh
krate fix            # dry run: tsconfig.json:13:16  KR7125  "./src" -> "."
krate fix --apply
```

Comments and formatting in `tsconfig.json` are kept. The edited file also
builds on 0.4, so you can make the change before upgrading and roll back
without undoing it. If you keep `baseUrl: "./src"` on purpose, write the
targets relative to it (`"@/*": ["./*"]`). Note that this changes meaning
silently: on 0.4 `"./*"` meant the project root, on 0.5 it means `src/`.

Related changes: a bare import that no `paths` entry and no package satisfies is
now tried against `baseUrl` (as `tsc` does with `baseUrl` alone); a
`paths`/`baseUrl` written with backslashes is normalised; an alias import of an
asset (`import logo from "@/assets/logo.png"`) works like a relative one. See
[TypeScript Setup](/docs/guides/typescript-setup/).

### Tailwind output follows v4

With `tailwind.enabled`, the generated CSS now follows Tailwind v4 semantics,
and a reset is included by default. The same page differs like this (measured
with 0.4.0 and 0.5 on one page, `shadow rounded border ring bg-blue-500 blur`):

| | 0.4 | 0.5 |
|---|---|---|
| Colours | hex (`#3b82f6`) | `oklch(62.3% 0.214 259.815)` |
| Reset (Preflight) | none | included in `@layer base`: `margin`/`padding`/`border` zeroed on every element, headings unstyled, lists without markers, `button` and form controls inherit font and colour, images are `display: block` |
| `ring` | 3px, `blue-500` at 50% | 1px, `currentcolor` (`ring-3` is the old width) |
| `shadow-sm` | `0 1px 2px` | `0 1px 3px` (what `shadow` was); `shadow-xs` is the old `shadow-sm` |
| `blur-sm`, `rounded-sm` | 4px, `0.125rem` | 8px, `0.25rem`; `blur-xs` and `rounded-xs` are the old values |
| `outline-none` | `outline: 2px solid transparent` | `outline-style: none`; `outline-hidden` keeps the old accessible behaviour |
| Layers | none | `@layer theme, base, components, utilities`, plus `@property` blocks |

What to do:

- To keep the old look, rename classes: `shadow-sm` to `shadow-xs`, `shadow` to
  `shadow-sm`, `blur-sm` to `blur-xs`, `rounded-sm` to `rounded-xs`,
  `ring` to `ring-3 ring-blue-500/50`, `outline-none` to `outline-hidden`.
- To drop the reset, set `tailwind: { preflight: false }` in `krate.config.ts`.
  There is no switch that restores Tailwind v3 output as a whole.
- **Unlayered CSS beats Tailwind utilities** because utilities live in a layer. A
  global `* { margin: 0; padding: 0 }` now overrides `p-*` and `m-*`; move it
  into `@layer base { ... }`.

### `krate dev` binds 127.0.0.1

0.4 listened on all interfaces; 0.5 listens on loopback only (the browser on the
same machine is unaffected). In Docker, WSL2, a devcontainer or a remote
machine, the host's browser can get "connection refused" and the log says
`http://localhost:3000` without mentioning the bind address. Bind all
interfaces explicitly:

```sh
krate dev --host 0.0.0.0
```

or once in `krate.config.ts`:

```ts
export default defineConfig({ devServer: { host: "0.0.0.0" } });
```

In Docker also publish the port (`-p 3000:3000`). With `0.0.0.0`, the dev
server is reachable by anyone on the network, so use it only on a trusted one.
In WSL2, `localhost` forwarding from Windows may work with the default bind;
use the override when it does not, or when you browse from another device. `krate serve` and `krate start`
still bind all interfaces (`server.host` restricts them).

### `krate start` and `krate serve`

In 0.4 `start` was an alias of `serve` and rebuilt first. In 0.5 `krate serve`
builds and serves, and `krate start` only serves an existing `dist/`. A
`"start": "krate start"` script on a fresh checkout now stops with `no build
output found`. Run `krate build` before it (the usual Dockerfile does), or
change the script to `krate serve`.

### Config files run in a sandbox

`krate.config.ts` has been evaluated inside the `krate` binary (esbuild plus an
embedded JavaScript engine, not Node) since 0.4. In 0.4 that engine had no Node
modules at all, so `import path from "node:path"` failed; 0.5 adds `path`,
`url`, `os`, read-only `fs`, `process.env`, `process.cwd()`, `process.argv`,
`process.platform`, `Buffer`, `URL` and `crypto.randomUUID()`. A config that
loaded on 0.4 loads on 0.5. `child_process`, `net`, `zlib`, `fs/promises` and
similar still do not exist and fail with an error that names the module. The full
list is in [Configuration](/docs/configuration/#what-a-config-file-can-use).

What does change is where `npx tsx` was used. With `tailwind.executeConfig`,
0.4 ran `tailwind.config.*` under `npx --yes tsx`, a full Node, so it could use
any Node API; 0.5 runs it in the same sandbox. And `generateStaticParams`,
API routes on a Node sidecar and the SSR renderer no longer start `tsx` at all:
they run on plain Node 20+, so they no longer need `npx` or a download. If a Tailwind config or plugin needs more than the sandbox offers,
compute the value in a script and read it from the environment, or move the
logic into a plugin.

### Build output

- The manifest moved from `dist/manifest.json` to `dist/.krate/manifest.json`
  (`dist/.krate/server-manifest.json` beside it for server builds). Update
  scripts that read it. A static host or an `rsync` rule that skips dot
  directories skips `dist/.krate/`; static sites do not need it (`krate deploy`
  strips it), server builds do.
- `404.html` loads a small hydration script (`/__404/index.<hash>.js`) so
  client-side navigation works from it, and carries
  `<meta name="robots" content="noindex">`. A host or CSP that assumed the page
  is JavaScript-free must allow the script.
- Pages carry markers the runtime needs: `<script type="application/json"
  id="__krate_route">` and `<!--k:...-->` comments, so HTML is about 15% larger
  (the starter's `index.html` went from 2111 to 2452 bytes), and
  `window.__KRATE_CFG__` and the view-transition `<style>` moved to the start of
  `<head>`. Tests that compare HTML byte for byte need new baselines.
- Pages that are only static no longer load the shared runtime chunk. Pages
  with an island or an action form do, as before.
- A `<Image width={n}>` ships variants of that size (`width={100}` gives 100w and
  200w) instead of six fixed 640-2000w files.

### New build errors

0.4 had no diagnostic codes and let these through; they are errors now. The
full list is in [Diagnostics](/docs/reference/diagnostics/).

| Code | Cause | Fix |
|------|-------|-----|
| (no code) | An import that resolves to no file or package, or an alias target that does not exist | Install the package, fix the path, or see the `baseUrl` section above |
| `KR7123` | Importing `.scss`, `.sass`, `.less` or `.styl` (0.4 dropped the styles silently) | Compile to CSS in a prebuild step, or use CSS modules or Tailwind |
| `KR7124` | `import { signal } from "@krate/runtime"`: a name the package does not export (it failed in the browser) | Use the Krate name (`createSignal`, ...); see the runtime API |
| `KR8801` | `<Image src="/missing.jpg">` for a file not in `public/` | Add the file or fix the path |
| `KR7121` | `src/pages/data.json.ts`-style endpoint threw at build time (0.4 built an empty HTML page for it) | Fix `GET`, or move it to `src/api` |
| `KR8013` | A hook of an npm React library (zustand, SWR, ...) in a layout or an async server page | Call it from a component on a synchronous page; it runs as an island |
| `KR3003`, `KR3007` | A class component with an unsupported `static` member, or `this` in a static | Rewrite the class as a function component |
| `KR7115` | A tier decorator (`@server`, `@client`, ...) where it cannot apply | Move it to a function, class or the default export |
| `KR9001`, `KR9002` | Two `404`/`error` files for one segment, or one in a `@slot` | Keep one per directory |

Config is validated too: a value of the wrong type (`devServer.port: "3000"`)
and a missing `pagesDir` are errors; an unknown key is a warning. Content
collections reject two entries with one slug (`a.md` and `a.mdx`) and a
non-ISO `date`. `krate build ./typo` fails instead of creating the directory.
`KR7120` (a file in `src/pages/api`) and `KR7125` (above) are not build errors
by themselves; the first warns, the second only appears in `krate fix`.

### `spawn .../bin/krate EACCES` on `0.5.0-beta.2`

The `0.5.0-beta.2` platform packages were published without the executable bit,
so `krate` fails with `EACCES` on every Linux and macOS install, including Docker.
Windows is not affected. Upgrade to a later release, where the wrapper also
restores the bit itself, or stay on `beta.2` and run this after installing:

```sh
chmod +x node_modules/@krate/core-*/bin/krate
```

### `bin/krate` is gone from `@krate/core`

The `@krate/core` package no longer contains a compiler binary. In
`0.5.0-beta.1` the tarball carried a stray single-platform `bin/krate` next to
the launcher (`bin/krate.js`), which made a clean install fail on Linux with
`spawn .../bin/krate EACCES`; the release now fails if a binary would be
published. The compiler comes from the `@krate/core-<platform>` package that npm
installs for your machine, and the launcher finds it under `node_modules`.
Nothing changes for `krate ...`, `npx krate` and package scripts. A script that
executes `node_modules/@krate/core/bin/krate` by path must call `krate` instead.

On npm 10 `node_modules/.bin/krate` could be missing after an install, because
the platform packages each declared `bin` and npm removes the shared link when
it rolls back the ones that do not match the machine. They no longer declare it;
the fix reaches you with the next release of the platform packages. Until then
run `npm rebuild @krate/core` or use `npx krate`. `krate version` reports the
native binary and warns when it differs from the npm wrapper.

### Server behaviour changes

These close holes; check them if you run `krate serve` behind a proxy or rely on
the old behaviour.

- Middleware without a `matcher` runs for every URL, including ones that end in a
  static extension (`/api/files/report.pdf`); only a real static file skips it.
- With `server.trustProxy`, the client IP is the `X-Forwarded-For` entry
  `server.trustedProxies` hops (default 1) from the right, not the leftmost entry.
- The Node, Bun and Deno API sidecar answers only the Krate host; hitting its
  port directly is refused.
- Request headers are capped at 64 KiB; action cookies are `Secure` over https;
  the no-JS flash cookie is signed; CORS responses carry `Vary: Origin`.
- A crashed SSR renderer restarts and pages answer `503` until it is back,
  instead of serving the empty build-time shell.
- A middleware or config rewrite to a page path without a trailing slash
  (`/feedback`) is served in place instead of answering `301`.

### Code blocks follow light and dark mode

`<Code>` and fenced code used to be dark in every mode. The default is now `github`
in light mode and `krate-dark` (`github-dark`'s syntax colours on Krate's neutral
surface) in dark mode, so code on a light page turns light. To keep one theme in
both modes, set it as a string:

```ts
export default defineConfig({ codeTheme: "github-dark" });
```

`codeTheme: { light, dark }` picks one for each mode. An unknown theme name is now
reported as `KR7150`.

### Component tiers are enforced

- `serverDirs` and `runtimeDirs` with relative paths never matched anything. They
  do now, so components in those directories become server or runtime components
  and get the checks below.
- A hook, an `onClick` or a class `setState` in a `@server`, `@static` or `@runtime`
  component used to build and do nothing. It now fails with `KR7110`: remove the tier,
  or move the interactive part into a client child.
- Tiers that contradict each other, such as `// @runtime` above `@server` or `@runtime`
  in a `*.server.tsx` file, fail with `KR7115`.

### Your own `Link`, `Head` or `Image` wins

A project component named like a built-in (`Link`, `Head`, `Script`, `Style`, `Icon`,
`Image`, `Suspense`, `Slot`) used to be replaced by it without warning. Your component
is now used. If you relied on the built-in, import it from `@krate/runtime`.

### New in 0.5

- **`@krate/ts-plugin`** is a new package: a TypeScript language-service plugin
  that silences the `tsc` errors tier decorators (`@server`, `@client`, ...)
  cause. The scaffold adds it to `devDependencies` and
  `"plugins": [{ "name": "@krate/ts-plugin" }]` to `tsconfig.json`, with a
  `"typecheck": "krate typecheck"` script. Upgraded projects work without it; add
  it if you use decorators.
- `krate fix`, `krate config`, `krate migrate nextjs`, `krate typecheck` and
  `krate start` are new commands, and `dev`, `serve` and `start` take `--port`
  and `--host`.
- `create-krate-app` pins `@krate/*` to its own version instead of `latest`.

## Prereleases

Prereleases are published under a dist-tag (e.g. `beta`):

```sh
npm install @krate/core@beta
```

Pin exact versions in production. Prereleases may change config or output
between builds.

## Breaking-change checklist

When upgrading across a minor:

1. Read the release notes (GitHub Releases for the tag) and `CHANGELOG.md`, which
   lists Breaking changes first for each release.
2. Re-read [Configuration](/docs/reference/config/) for renamed/removed keys -
   Krate now warns on unknown config keys.
3. Run `krate fix` (a dry run) for source and `tsconfig.json` fixes it can make
   safely, then `krate fix --apply`. Regenerate types: `krate types`.
4. Run `krate build && krate check && krate typecheck`.
5. Diff `dist/` if output shape matters to you.

## Version compatibility

| Package | Requirement |
|---------|-------------|
| Node (npm wrapper, `generateStaticParams`, SSR and API sidecars; not needed for the config file) | 20+ |
| Bun or Deno (optional sidecar runtimes) | current |
| Go (to build the compiler from source) | see `packages/compiler/go.mod` |
| Browsers | see [Browser Support](/docs/core-concepts/browser-support/) |

## Rolling back

Pin the previous version explicitly:

```sh
npm install @krate/core@0.4.0 @krate/runtime@0.4.0 @krate/components@0.4.0
```

If a build broke after an upgrade, delete stale caches and rebuild:

```sh
rm -rf dist .krate
```

Going back to 0.4 after `krate fix --apply` needs no undo: `"baseUrl": "."`
builds on both.

## Component library: namespaced parts

`@krate/components` uses a namespaced compound API: `Tabs.Trigger`,
`Breadcrumb.List`, `Table.Row`, `Steps.Step`, `Form.Field`, `FileTree.Folder`,
and so on. The older flat exports (`TabsTrigger`, `BreadcrumbList`, `TableRow`,
`Step`, `FormField`, `FileTreeItem`, ...) still work as **deprecated aliases**, but
prefer the namespaced form - it is what the docs and examples use.
