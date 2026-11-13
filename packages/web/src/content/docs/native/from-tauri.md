---
title: Migrating from Tauri
order: 23
description: Move a Tauri v2 app to Krate Native - commands, events, capabilities, plugins, tauri.conf.json, sidecars, the updater and bundler targets, side by side.
---

# Migrating from Tauri

> **Experimental.** Part of [Krate Native](/docs/native/), which is under construction.
> This guide describes what exists in this release. Where Krate Native has no
> equivalent of a Tauri feature, the tables say so.

Tauri and Krate Native share the main idea: your UI runs in the **system
webview** (WebKitGTK, WKWebView, WebView2) and talks to a small native process,
so there is no bundled browser. The web-compatibility notes you already have
for Tauri (CSS and JavaScript support per OS, WebView2 installation) apply
unchanged.

The differences are in the native side and the tooling:

- The native side is **Go** (or TypeScript on an embedded QuickJS, Node, Bun or
  Deno), not Rust. There is no Rust in Krate Native, and Rust crates cannot be
  linked into it; port the code to Go, or run it as a separate program.
- The UI is TSX compiled by Krate, prerendered to static HTML with a small signal
  runtime, not an arbitrary frontend served by a dev server. A React, Vue or
  Svelte frontend has to be rewritten as Krate pages (or kept as a web app and
  shown in a [remote view](/docs/native/remote/), which gets no access to the
  app).
- There is no JavaScript `invoke` call to write: the page imports host code and
  calls it, and the compiler generates the call and its types.
- Permissions are derived from the code instead of being written in capability
  files.

If your Tauri frontend is plain TypeScript or Preact, most of it moves over
as it is. The [Electron guide](/docs/native/from-electron/) has a complete
worked port; its walkthrough applies here too, because the shape of the work is
the same: host functions, a host signal for shared state, SDK calls for the
desktop pieces, `krate native test` for tests. The repository example is
[`examples/native-from-electron`](https://github.com/kratejs/krate/tree/main/examples/native-from-electron).

## Concepts

| Tauri v2 | Krate Native |
|----------|--------------|
| Rust core (`src-tauri/src`) | **Host code**: Go (`.go` files, always host code) or TypeScript marked `@host` or named `*.host.ts` ([Host code](/docs/native/host-code/), [Go host code](/docs/native/go-host/)) |
| `#[tauri::command]` and `invoke()` | An exported Go function or host function; the page imports and calls it |
| `invoke_handler(generate_handler![...])` | Nothing. Exported functions of host modules the UI imports are registered by the build. |
| Events (`emit`, `listen`, `emit_to`) | A host **signal** for state, a host **stream** for a sequence, `view.post` between views; see [Events](#events-and-channels) |
| `tauri::ipc::Channel` | A stream: a Go function that returns `<-chan T`, or a TypeScript `async function*` |
| `State<T>`, `app.manage()` | Package-level variables in Go (guard them with a mutex); module-level variables in TypeScript host code |
| `AppHandle`, `Manager` | Package functions: `window.Open`, `menu.SetApp`, `app.Quit` ... in Go; `@krate/app/<area>` in the UI |
| Webview window labels (`main`) | Window ids: the first window is `main`; `open()` returns a handle with the new id |
| `WebviewWindow` | A window; extra webviews in a window are [views](/docs/native/views/) |
| `tauri.conf.json` | The `native` block of `krate.config.ts` ([The native config block](/docs/native/#the-native-config-block)) |
| Capabilities and permissions (`capabilities/*.json`) | [Derived permissions](/docs/native/release/#derived-permissions) plus an optional allowlist, `native.permissions`, and per-window `permissions` |
| Plugins (crates with guest JS bindings) | The built-in SDK areas, and [native plugins](/docs/native/plugins/): a Go package, a `krate-plugin.json` and a generated TypeScript module |
| Sidecars (`externalBin`) | Go host code starting a program with `os/exec`, or a [worker](/docs/native/processes/#workers) for programs that speak the host child protocol |
| `tauri dev` | `krate native dev` ([Development](/docs/native/dev/)) |
| `tauri build` | `krate native build`, `krate native package` ([Packaging](/docs/native/packaging/)) |
| `tauri-driver` (WebDriver), `mockIPC` | `krate native test` and `@krate/app/driver` ([Testing](/docs/native/testing/)) |
| Mobile targets (iOS, Android) | none |

## Commands become host calls

A Tauri command and its call:

```rust
#[tauri::command]
fn add_task(title: String, state: tauri::State<'_, Tasks>) -> Result<Task, String> {
    state.add(title).map_err(|e| e.to_string())
}
```

```ts
import { invoke } from '@tauri-apps/api/core';
const task = await invoke<Task>('add_task', { title: 'Buy milk' });
```

The same in Krate Native with Go host code:

```go
// src/lib/tasks.go
package lib

import "github.com/kratejs/krate/packages/native/krate"

func Add(title string) (Task, error) {
    if title == "" {
        return Task{}, krate.Errorf("bad_args", "title is empty")
    }
    return store.add(title), nil
}
```

```tsx
import { Add } from '../lib/tasks.go';
const task = await Add('Buy milk');
```

or with TypeScript host code:

```ts
// src/lib/tasks.host.ts
export async function addTask(title: string): Promise<Task> { /* ... */ }
```

| Tauri | Krate Native |
|-------|--------------|
| Arguments as one object of named fields | Positional arguments; a struct or object for several values |
| `Result<T, E>` with `E: Serialize` | `(T, error)` in Go; a thrown `Error` in TypeScript. The page receives a rejected `KrateError` with a `code`, a `message` and optional `data`; `krate.Errorf(code, ...)` sets the code |
| `async fn` command | Every call is already asynchronous in the page |
| `serde` types | Go structs map to objects (honoring `json` tags), slices to arrays, `map[string]T` to records, `[]byte` to `Uint8Array`, `time.Time` to `Date`, pointers to `T \| null` |
| Generated bindings (`tauri-specta`) | Generated for you: `.krate/native/types/*.d.ts` |
| `window: tauri::Window` parameter | An optional first parameter `ctx krate.Ctx`; it identifies the calling window |
| Command in a plugin | An op of a native plugin |
| `invoke` with an unknown command | `not_found`; a missing permission is `permission_denied`, so the page can tell them apart |

The ids of calls are `<module id>.<export>` (`src/lib/tasks.Add`). Only host
code the UI imports is registered. A host call never runs at build time:
during prerendering a call or a signal read yields no value, and a top-level
`await` of one is the build error `KR7102`. Keep host results in a signal that
a handler or an effect sets.

## Events and channels

| Tauri | Krate Native |
|-------|--------------|
| `app.emit("count", n)` to all windows, `listen("count", fn)` | A host signal: `var Count = krate.NewSignal(0)` and `Count.Set(n)` in Go, `hostSignal(0)` in TypeScript. The page reads `Count()`, which is tracked like a runtime signal, and updates when the host sets a new value. Before the first value arrives a signal reads as `undefined`. |
| A stream of events | A stream: Go `func Watch(ctx krate.Ctx) <-chan int`, TypeScript `async function*`. The page consumes it with `for await`, and stops by breaking out or aborting. |
| `emit_to("label", ...)` to one window | None for host-to-one-window pushes. Signals are shared. Between views use [`view.post`](/docs/native/views/#messages) (`from` is set by the shell); a message from host code has `from` `""`. |
| `window.emit` from page to page | `view.post(data)` to another view, or a [port](/docs/native/processes/#message-ports) |
| `Channel<T>` for progress | A stream (the updater's `download(onProgress)` is one) |
| `once`, `unlisten` | The function returned by a helper such as `onMenu` or `onClick` stops that subscription |
| Built-in events (`tauri://focus`, `tauri://close-requested`, drag and drop) | `onEvent` of `@krate/app/window` (`close-requested`, `closed`, `focus`, `blur`, `released`, `restored`); `onFileDrop`; `onOpenFile` |

A page subscribes after it hydrates, so an event sent before that is not seen.
State that must be correct on load belongs in a signal (which replays its current
value), not in a one-off event.

## Capabilities become derived permissions

In Tauri, `src-tauri/capabilities/default.json` lists the permissions each window
has:

```json
{ "identifier": "main", "windows": ["main"],
  "permissions": ["core:default", "fs:allow-read-text-file", "dialog:allow-open", "notification:default"] }
```

In Krate Native nothing like that is written. The build reads which SDK modules
your UI and TypeScript host code import (`import { readText } from '@krate/app/fs'`
needs `fs:read`) and writes exactly the permissions that code needs to `app.json`.
Then:

- **Allowlist.** `native.permissions` is optional. When set, the build fails
  (`KR7210`) if the code needs a permission it does not list. Use it the way you
  used capability files: as the reviewed list.
- **Per window and view.** `native.window.permissions` (the main window), the
  `permissions` option of `open()` and `addView()` narrow a page's scope. A window
  can only open windows with a subset of its own scope.
- **Scopes.** Tauri's `fs` scope globs (`$APPDATA/**`) have no config here. Page
  file access is fixed: the app directories (`appDataDir()`, `cacheDir()`,
  `configDir()`, `logDir()`, `tempDir()`) and any path or folder the user picks
  in a dialog or drops on a window. Anything else rejects with `permission_denied`
  and `reason: "outside_scope"`. Go and TypeScript host code are trusted and not
  scoped, so a command that reads an arbitrary path is a host function.
- **Deny by default.** A permission nothing imports is never granted. The
  sensitive ones (`fs`, `clipboard:read`, `os:secrets`, `shell:openExternal`,
  `tray`, `shortcut`, `power:inhibit`, `login`, `crash`) are also off when no
  list is given to dev builds.
- **CSP.** There is no `app.security.csp` key. The page's Content Security Policy
  is set by the shell and forbids `eval`.
- **Remote content.** Tauri's `remote` capability URLs (letting a remote URL call
  commands) have no counterpart, on purpose. A remote view has an empty scope and
  talks to the app only through an opt-in message channel with origin checks.

`krate native check` prints which import needs which permission. A call without
its permission rejects with `permission_denied` and `data.permission`.

## Plugins

| Tauri plugin | Krate Native |
|--------------|--------------|
| `fs` | `@krate/app/fs` (`readFile`, `readText`, `readDir`, `stat`, `exists`, `writeFile`, `writeText`, `mkdir`, `remove`, `rename`); scoped as above |
| `dialog` | `openFile`, `openFolder`, `saveFile`, `message`, `confirm` from `@krate/app/dialog`; `[]` or `null` when cancelled |
| `notification` | `send`, `close`, `onAction`, `onEvent` from `@krate/app/notify`; macOS needs an app bundle |
| `clipboard-manager` | `@krate/app/clipboard`: text, HTML, RTF, images, files and named formats |
| `global-shortcut` | `register(accelerator, fn)` from `@krate/app/shortcut` |
| `shell` (`open`) | `openExternal(url)` from `@krate/app/os` (`http`, `https`, `mailto`) |
| `shell` (spawn, sidecar) | Go `os/exec` in host code; [workers](/docs/native/processes/#workers) |
| `store` | `@krate/app/kv` |
| `sql` | `@krate/app/db` (SQLite: `exec`, `query`, `transaction`) |
| `http` | `fetch` in the page, `net/http` in Go host code |
| `os` | `info()` (`os`, `arch`, `osVersion`, `hostname`, `locale`) from `@krate/app/os` |
| `process` (`exit`, `relaunch`) | `quit()`, `relaunch()` from `@krate/app/app` |
| `updater` | `@krate/app/updater`; see [Updater](#updater) |
| `window-state` | Go `window.SaveState` and `window.RestoreState`; none in the UI module |
| `deep-link` | `native.deepLinks`; `onOpenURL` from `@krate/app/app` |
| `single-instance` | Go `app.SingleInstance`; `onSecondInstance`. Apps with `native.fileAssociations` take the lock automatically |
| `autostart` | `getLoginItem()`, `setLoginItem()` from `@krate/app/login` |
| `positioner` | `centerWindow`, `displayOfWindow` from `@krate/app/screen`; `setPosition` |
| `stronghold` | `secretSet`, `secretGet`, `secretDelete` from `@krate/app/os`: the OS credential store, not an encrypted vault file |
| `log` | none |
| `cli`, `upload`, `websocket` | none: use a browser API in the page, or Go |
| `opener` | `openExternal` |
| `biometric`, `barcode-scanner`, mobile plugins | none |
| Tray (`TrayIconBuilder`) | `set`, `remove`, `onEvent` from `@krate/app/tray`; PNG bytes for the icon |
| Menu (`MenuBuilder`) | `setApp(items)`, `popup`, `setContextMenu` from `@krate/app/menu`; no predefined items, so write Edit entries yourself |
| Window API (`getCurrentWindow()`) | `main()`, `get(id)`, `open()` from `@krate/app/window` |
| Webview printing, zoom, find | `@krate/app/page`; `@krate/app/downloads`; `@krate/app/cookies` |
| Custom plugin in Rust | [Native plugin](/docs/native/plugins/) in Go: `krate native plugin init <name>` |

A Tauri plugin has a Rust half (commands, permissions TOML) and a guest
JavaScript half. A Krate plugin is one Go package plus `krate-plugin.json`;
ops are called as `plugin/<name>.<op>`, the TypeScript module `krate/plugin/<name>`
is generated, and each permission is `plugin:<name>.<permission>`. A plugin makes
the app build with a per-app shell, which needs the Go toolchain.

## Configuration

| `tauri.conf.json` | `krate.config.ts` |
|-------------------|-------------------|
| `productName` | `native.productName` |
| `version` | `native.version` |
| `identifier` | `native.appId` |
| `build.devUrl`, `beforeDevCommand`, `frontendDist`, `beforeBuildCommand` | none. Krate compiles the pages itself; `entry`, `pagesDir` and `outDir` are top-level config keys. `krate native dev` replaces the dev server. |
| `app.windows[0].width`, `height`, `resizable`, `title` | `native.window.width`, `height`, `resizable`; the title is the product name |
| `app.windows[0].decorations: false` | `native.window.titleBar: "none"` or `"hidden"` ([Window chrome](/docs/native/window-chrome/)) |
| `transparent` | `native.window.transparent`; `backdrop` (Windows 11), `vibrancy` (macOS) |
| `alwaysOnTop`, `fullscreen`, `maximized`, `x`, `y` | options of `open()`, or the `Window` methods; the main window's size comes from the config |
| `url` of a window | `path`, a route of the app |
| More windows in config | `open()` at run time |
| `app.trayIcon` | `tray.set()` |
| `app.security.csp`, `capabilities` | see [Capabilities](#capabilities-become-derived-permissions) |
| `app.withGlobalTauri` | none; there is no global, import what you use |
| `bundle.icon` | `native.icons`: one PNG, the packager writes `.icns`, `.ico` and the Linux sizes |
| `bundle.targets` | `bundle.windows.installer` (`nsis` or `msix`), `bundle.linux.formats` (`deb`, `rpm`, `appimage`, `flatpak`, `snap`), `--format` |
| `bundle.category`, `publisher` | `bundle.macos.category`, `bundle.windows.publisher` |
| `bundle.resources` | files in the project's public directory ship with the UI; other files must sit next to the executable in `dist-native/<os>-<arch>/`, which `package` ships |
| `bundle.externalBin` | none; see [Sidecars](#sidecars) |
| `bundle.windows.webviewInstallMode` | `bundle.windows.webview2`: `download`, `embed`, `fixed`, `none` (with `webview2Path`) |
| `bundle.macOS.entitlements`, `signingIdentity` | `krate native sign --entitlements <file> --identity <name>`; `KRATE_MACOS_IDENTITY` |
| `bundle.linux.deb`, `rpm`, `appimage` options | `bundle.linux.summary`, `description`, `license`, `homepage` |
| `plugins.updater.pubkey`, `endpoints` | `native.updater.publicKey`, `url` |
| `plugins.deep-link.desktop.schemes` | `native.deepLinks` |
| file associations (`bundle.fileAssociations`) | `native.fileAssociations` |
| Cargo features, `Cargo.toml` | `go.mod` (needs `github.com/kratejs/krate/packages/native`) |

Krate warns about config keys it does not know, so a pasted `tauri.conf.json`
section shows up at once. Memory is configured separately (`native.memory`:
`balanced`, `minimal`, `performance`; see [Memory](/docs/native/memory/)).

## Sidecars

Tauri bundles an executable (`externalBin`), starts it through the shell plugin
and reads its output. In Krate Native:

- **A program of your own that talks to the app**: write it as a [worker](/docs/native/processes/#workers).
  A worker is a separate process started by name (`native.workers`), with its
  own narrowed permissions, logs, a restart policy and two-way messages. It can
  be a TypeScript module on Node, Bun or Deno (`entry`), a Go `main` package that
  calls `worker.Serve` (`go`), or a prebuilt executable that serves the host child
  protocol (`exec`).
- **An existing third-party executable** (ffmpeg, a database): start it with
  `os/exec` from Go host code, stream its output through a `<-chan T`, and put the
  binary next to the executable in `dist-native/<os>-<arch>/`, which `package`
  ships. There is no sidecar naming convention (`name-x86_64-unknown-linux-gnu`)
  and no `Command.sidecar` call; build the path yourself from `os.Executable()`.
  The shell plugin's scope rules do not exist, so validate arguments in your own
  code.
- **A Node-based sidecar** that only exists because the Rust side could not do a
  job (an image library, say): choose `native.host.runtime: "node"` or an
  `islands` entry and keep the code as TypeScript host code
  ([Host runtimes](/docs/native/runtimes/)). The SDK modules (`kv`, `fs`, `db`)
  are not wired into Node, Bun and Deno children yet; call them from the UI or
  from QuickJS host code.

Workers are copied to `host/workers` in a release build. A TypeScript worker
needs Node, Bun or Deno on the user's machine, unless you bundle one with
`native.host.bundle`; use a Go worker for a self-contained app.

## Updater

| Tauri updater | Krate Native |
|---------------|--------------|
| `tauri signer generate` | `krate native keygen --key-out krate-updater.key` (Ed25519; prints the public key) |
| `plugins.updater.pubkey`, `endpoints` with `{{target}}`, `{{arch}}`, `{{current_version}}` | `native.updater.publicKey` and `url`, which may contain `{target}` (`linux-amd64`, `darwin-arm64`, `windows-arm64`, `darwin-universal`), `{version}` and `{channel}` |
| `TAURI_SIGNING_PRIVATE_KEY` at build | `KRATE_UPDATER_PRIVATE_KEY` or `KRATE_UPDATER_PRIVATE_KEY_FILE` for `krate native package` |
| `latest.json` (static or from a server) | `latest-<target>.json` per platform, written by `package --update-base-url <url> --notes ...`; you host it |
| `check()`, `downloadAndInstall()`, `relaunch()` | `check()`, `download(onProgress)`, `apply()`, `relaunch()`, or `update(onProgress)` for all three |
| Update artifact (`.app.tar.gz`, `.AppImage.tar.gz`, NSIS/MSI) | the `.zip` (macOS, Windows), the `.tar.gz` (Linux), or the AppImage |
| none | Delta updates (`--delta-from`), staged rollouts (`--rollout`), channels, and automatic rollback of a version that does not start properly |

The feed and the signature scheme are not compatible with Tauri's: a Tauri app
cannot update itself into a Krate app. Ship the Krate version as a fresh install,
or as a final Tauri release that opens a download page.

## Bundler targets

| Tauri | Krate Native |
|-------|--------------|
| `deb`, `rpm`, `appimage` | `deb` (default), `rpm`, `appimage` (needs `appimagetool`); plus `tar.gz`, `flatpak`, `snap` |
| `nsis` | `nsis` (needs `makensis`) |
| `msi` (WiX) | none; use `msix` |
| `app`, `dmg` | `app`, `dmg` (macOS only), `zip` |
| universal macOS | `--target darwin/universal` |
| Mac App Store | `--target mas` ([Stores](/docs/native/distribution/#mac-app-store)) |
| Microsoft Store | `msix-store` |
| cross-compilation | `krate native build --target <os>/<arch>` uses `CGO_ENABLED=0`; formats that need a platform tool still need it |
| signing and notarization | `krate native sign`, `notarize`, `package --sign --notarize` ([Signing](/docs/native/signing/)) |

A release is one executable with the app embedded (`krate native build
--release`), or the prebuilt `krate-shell` renamed to the product name with an
`app/` directory next to it. A TypeScript-only app needs no Go toolchain to build
or package once a shell is available (`@krate/core-<platform>` packages ship one).

## Testing and debugging

| Tauri | Krate Native |
|-------|--------------|
| `tauri-driver` with WebdriverIO or Selenium (Linux and Windows) | `krate native test` (Linux, macOS, Windows) with `launch()` from `@krate/app/driver` and Node's test runner |
| `mockIPC`, `mockWindows` | Call the real app; `page.call("src/lib/tasks.Add", ...)` calls a host function or an SDK id as the page would. Inject tray, menu and shortcut events with `app.emit` |
| `cargo test` for commands | `go test` for Go host code; the shell loads the same package |
| Right-click, Inspect Element / `devtools` feature | F12 or Ctrl+Shift+I (Cmd+Option+I) in dev builds; `native.debug: true` for release |
| `RUST_BACKTRACE`, `lldb` | `krate native dev --debug-go` runs the Go host under Delve; `--debug-host` attaches the Node inspector |
| Hot reload of the frontend | The same, and Go and TypeScript host code reload in place |

## Things that are different

- **No Rust.** Port commands to Go (typed, compiled into the shell in a release)
  or TypeScript. Crates have no equivalent; most have a Go library.
- **Prerendered TSX pages, not an arbitrary frontend.** There is no
  `frontendDist`. Server rendering, API routes, middleware and server actions
  are errors in a native build.
- **No IPC code in the page.** There is no `invoke` string, no `@tauri-apps/api`,
  no `window.__TAURI__`. You import the function.
- **No granular permission files.** The set of permissions is the set of SDK
  modules you import, plus the scopes above. There are no `allow-`/`deny-`
  command-level permissions and no configurable path scopes.
- **Event pushing is by state.** Host-to-page notifications are signals and
  streams; there is no named, untyped event bus.
- **Menus have no predefined items**, and there is one application menu.
- **Mobile is not supported.**
- **One engine per OS, as in Tauri.** WebKitGTK, WKWebView and WebView2 differ;
  test on all three. Linux (WebKitGTK) is the most tested platform in Krate
  Native, and the macOS and Windows code paths run in the CI native job; see the
  [compatibility matrix](/docs/guides/compatibility/#krate-native).
- **Size and start.** A Krate release executable on Linux is about 16 MB
  (11 MB stripped) and shows a window in about 0.2 s; Tauri binaries are
  typically smaller; the two have not been measured side by side. The footprint
  numbers and how they were measured are in
  [Getting started](/docs/native/getting-started/#footprint-linux-measured).

## Migration checklist

1. Run `krate native init` and move your UI into `src/pages`.
2. For every `#[tauri::command]`, write a Go function or a TypeScript host function
   with the same behavior; replace each `invoke` with an import.
3. Replace `emit`/`listen` with host signals and streams.
4. Replace plugin calls using the [plugin table](#plugins); port custom plugins to
   Go native plugins.
5. Move `tauri.conf.json` values into `native` in `krate.config.ts`.
6. Run `krate native check` and set `native.permissions` to the reviewed list.
7. Decide how each sidecar runs ([Sidecars](#sidecars)).
8. Port tests to `krate native test`.
9. Set up `krate native package`, signing and the update feed.
10. Test on all three operating systems.
