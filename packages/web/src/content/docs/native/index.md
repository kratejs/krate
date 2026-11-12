---
title: Krate Native (Preview)
order: 1
description: Build a desktop app from a Krate project - TSX UI in a system webview, host code in Go or TypeScript.
sidebar:
  label: Native (Preview)
  order: 6
  collapsible: true
---

# Krate Native (Preview)

> **Preview.** Krate Native is under construction. `krate native init`, `check`,
> `dev`, `build` (also `--release`), `profile`, `package`, `sign`, `notarize` and
> `keygen` and `doctor` work today. Names and options may change before release.
> The tested platform is Linux; macOS and Windows are [untested](/docs/guides/compatibility/#krate-native).

Krate Native compiles a Krate app into a desktop app. The UI is TSX shown in the
system webview. Code that must run in the native process, such as file access,
a database or the operating system, is **host code**: Go files, or TypeScript
functions you mark. The UI imports host code and calls it like an async function.

There is no server in a native app, so there is no SSR: every route is
prerendered at build time.

- [Getting started](/docs/native/getting-started/) - `krate native init` and
  `krate native build`, and what a build produces.
- [Host code](/docs/native/host-code/) - the six ways to mark host code, their
  precedence, conflicts, inline function rules and the native target's
  restrictions.
- [Go host code](/docs/native/go-host/) - call exported Go functions, streams
  and signals from the UI.
- [Host runtimes](/docs/native/runtimes/) - run TypeScript host code on Node,
  Bun, Deno or scriptc, islands, release bundling.
- [Build functions](/docs/native/build-functions/) - run a function at build
  time and bake its result into the app.
- [The app directory and app.json](/docs/native/app-json/) - the output of a
  build and the manifest the shell reads.
- [Development](/docs/native/dev/) - `krate native dev`, hot reload and
  browser mode.
- [Native SDK: data and system](/docs/native/sdk/) - app directories, files,
  SQLite, key-value storage, secrets and permissions.
- [Native SDK: windows and desktop](/docs/native/sdk-ui/) - windows, dialogs,
  clipboard, menus, tray, shortcuts and notifications.
- [Views and split panes](/docs/native/views/) - several webviews in one window,
  draggable dividers, per-view scopes and view-to-view messages.
- [Release builds](/docs/native/release/) - one executable per platform, SDK
  shaking, derived permissions, per-window scopes and profile-guided builds.
- [Native plugins](/docs/native/plugins/) - reusable native capabilities as a Go
  package with a manifest, generated TypeScript and declared permissions;
  `native.plugins` turns one on.
- [Packaging](/docs/native/packaging/) - `krate native package`: installers
  and archives for macOS, Windows and Linux, icons and Windows resources.
- [Signing and notarization](/docs/native/signing/) - `krate native sign` and
  `notarize`.
- [Updates](/docs/native/updater/) - in-app updates with signed update files.
- [Memory](/docs/native/memory/) - memory profiles, shared web process,
  tray-style background mode and measured numbers.
- [Testing your app](/docs/native/testing/) - `krate native test` and the
  `@krate/app/driver` module: drive pages, host calls and native events from
  Node tests, headless in CI.

## Quick start

```typescript
// krate.config.ts
import { defineConfig } from "@krate/core";

export default defineConfig({
  native: {
    appId: "com.acme.app",
    productName: "Acme",
    window: { width: 1280, height: 800 },
  },
});
```

```sh
krate native check
```

`krate native check` loads the config, decides which modules and functions are
host code, reports conflicts and native-target errors, and prints the host
modules it found.

## The `native` config block

| Key | Default | Notes |
|-----|---------|-------|
| `appId` | none | Reverse-DNS id such as `com.acme.app` |
| `productName`, `version` | none | `version` is semver |
| `platforms` | `windows`, `macos`, `linux` | |
| `icons` | none | Path of the source icon |
| `window` | 1280 x 800, `resizable: true`, `titleBar: "native"` | `titleBar` is `native`, `hidden` or `none`; `permissions` [narrows](/docs/native/release/#per-window-scopes) the main window's page; `keepAlive: true` keeps the app running with no window open, for tray apps ([background mode](/docs/native/memory/#windows-and-background-mode)) |
| `permissions` | no restriction | An optional allowlist of [SDK permission names](/docs/native/sdk/#permissions). A build [derives](/docs/native/release/#derived-permissions) the permissions the code needs, writes them to `app.json`, and fails (KR7210) if one is not listed. Unknown names fail the config check |
| `deepLinks` | none | URL schemes without `://` |
| `host` | `runtime: "quickjs"` | `runtime` is `quickjs`, `node`, `bun`, `deno` or `scriptc`; `scriptc` needs `experimental.scriptc: true`; `islands` maps a package to `node`, `bun`, `deno` or `quickjs` |
| `release` | `compile: "auto"`, `pgo: "native.pgo"` | `compile` is `auto`, `always` or `never` ([release builds](/docs/native/release/#compile-modes)); `pgo` is the CPU profile `krate native profile` writes and `--release` uses |
| `bundle` | none | `macos.category`, `windows.publisher`, `windows.installer` (`msix` or `nsis`), `windows.webview2` (`download`, `embed`, `fixed` or `none`) with `windows.webview2Path`, `linux.formats` (`deb`, `rpm`, `appimage`) |
| `updater` | none | `url` (https, may contain `{target}`) and `publicKey`, both required |

Unknown keys inside the block warn like the other config blocks. The top-level
`hostFunctions` and `hostDirs` lists mark host code; see
[Host code](/docs/native/host-code/).
