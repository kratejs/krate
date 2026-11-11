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
> `build`, `dev`, `package`, `sign`, `notarize` and `keygen` work today;
> `profile` and `doctor` print "not implemented yet". Names and options may
> change before release.

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
- [The app directory and app.json](/docs/native/app-json/) - the output of a
  build and the manifest the shell reads.
- [Native SDK: data and system](/docs/native/sdk/) - app directories, files,
  SQLite, key-value storage, secrets and permissions.
- [Packaging](/docs/native/packaging/) - `krate native package`: installers
  and archives for macOS, Windows and Linux, icons and Windows resources.
- [Signing and notarization](/docs/native/signing/) - `krate native sign` and
  `notarize`.
- [Updates](/docs/native/updater/) - in-app updates with signed update files.

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
| `window` | 1280 x 800, `resizable: true`, `titleBar: "native"` | `titleBar` is `native`, `hidden` or `none` |
| `permissions` | the SDK defaults | Any of the [SDK permission names](/docs/native/sdk/#permissions); written to `app.json`. Unknown names fail the config check |
| `deepLinks` | none | URL schemes without `://` |
| `host` | `runtime: "quickjs"` | `runtime` is `quickjs`, `node`, `bun`, `deno` or `scriptc`; `scriptc` needs `experimental.scriptc: true`; `islands` maps a package to `node`, `bun`, `deno` or `quickjs` |
| `release` | `compile: "auto"` | `compile` is `auto`, `always` or `never`; `pgo` is a profile path |
| `bundle` | none | `macos.category`, `windows.publisher`, `windows.installer` (`msix` or `nsis`), `linux.formats` (`deb`, `rpm`, `appimage`) |
| `updater` | none | `url` (https, may contain `{target}`) and `publicKey`, both required |

Unknown keys inside the block warn like the other config blocks. The top-level
`hostFunctions` and `hostDirs` lists mark host code; see
[Host code](/docs/native/host-code/).
