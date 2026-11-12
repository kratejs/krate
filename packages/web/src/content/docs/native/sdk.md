---
title: "Native SDK: Data and System"
order: 9
description: The Native SDK for files, SQLite, key-value storage, secrets, networking and the permissions that gate them, from Go and from TypeScript with @krate/app.
---

# Native SDK: Data and System

> **Preview.** Part of [Krate Native](/docs/native/), which is under construction.
> Ids and permission names are fixed; more may be added.

The SDK is a set of Go packages under `packages/native/sdk`. Go host code
imports them and calls plain functions. The UI and TypeScript host code reach
selected operations through host ids named `krate/<area>.<name>`, wrapped by
`@krate/app/os`, `@krate/app/fs`, `@krate/app/db`, `@krate/app/kv` and
`@krate/app/permission`. The shell registers the SDK for every app. The
windows, dialogs, clipboard, menus, tray, shortcuts, notifications and
lifecycle have [their own page](/docs/native/sdk-ui/); they share the
permission list, the denial error and the id table below.

```ts
import { appDataDir, info } from "@krate/app/os";
import * as kv from "@krate/app/kv";
import * as db from "@krate/app/db";

await kv.set("theme", "dark");
await db.exec("CREATE TABLE IF NOT EXISTS notes(id INTEGER PRIMARY KEY, body TEXT)");
const { lastInsertId } = await db.exec("INSERT INTO notes(body) VALUES(?)", "hello");
const rows = await db.query<{ id: number; body: string }>("SELECT * FROM notes");
console.log(await appDataDir(), (await info()).os);
```

## Permissions

(Native plugins define their own permissions, `plugin:<plugin>.<permission>`; see
[Native plugins](/docs/native/plugins/#permissions).)

`app.json` has a `permissions` list. Go and TypeScript host code are trusted and
never checked (a TypeScript call through `__krateHost.call` reaches the ungated
implementation); permissions decide which ids the UI may call.

| Permission | Default | Grants |
|------------|---------|--------|
| `os:info` | yes | App directories and OS information |
| `kv` | yes | The key-value store |
| `db` | yes | The SQLite database |
| `fs` | no | Writing files inside the allowed folders; implies `fs:read` |
| `fs:read` | no | Reading files inside the allowed folders |
| `os:secrets` | no | Setting and deleting secrets |
| `os:secrets.read` | no | Reading secrets; `os:secrets` does not imply it |
| `shell:openExternal` | no | Opening http, https and mailto URLs in the default handler |
| `net` | no | Reserved for a network policy; no UI id uses it yet |
| `window` | yes | Opening and controlling windows |
| `app` | yes | Quitting, relaunching, capability checks, second-instance events |
| `menu` | yes | The application menu and context menus |
| `dialog` | yes | Native file and message dialogs |
| `notify` | yes | Desktop notifications |
| `clipboard` | yes | Writing the clipboard |
| `clipboard:read` | no | Reading the clipboard (sees what the user copied elsewhere) |
| `tray` | no | A system tray icon |
| `shortcut` | no | Global keyboard shortcuts |
| `share` | no | The system share menu for text, URLs and files |
| `updater` | no | Checking for, downloading and applying app updates, and relaunching; see [Updates](/docs/native/updater/) |
| `page` | no | Print, find in page, zoom and capture the app's own views; see [Web contents](/docs/native/webcontents/) |
| `downloads` | no | Choosing where page downloads go, watching and cancelling them; see [Web contents](/docs/native/webcontents/#downloads) |
| `cookies` | no | Reading, writing and deleting cookies, clearing site data and the cache; see [Web contents](/docs/native/webcontents/#cookies-and-site-data) |
| `remote` | no | Views and windows with web pages that have no access to the app, navigating them and the message channel; see [Remote content](/docs/native/remote/) |
| `remote:intercept` | no | Block, redirect and add headers to the requests of remote views, and watch them |
| `remote:network` | no | User agent and proxy of remote partitions, certificate decisions, logins, client certificates and clearing remote data |

An empty or missing list means the defaults: the non-sensitive set marked
"yes" above. `krate native build` writes the list for you: it
[derives](/docs/native/release/#derived-permissions) the permissions from the
`@krate/app/<area>` imports and `krate/<area>.<op>` ids the UI and TypeScript
host code use, and `native.permissions` in the config is an allowlist that fails
the build (KR7210) when the code needs something it does not list. A
non-empty list grants exactly what it names (plus what a name implies), so an
app that lists only `fs` loses `kv` and `db` unless it lists them too. Unknown
names are an error when the app loads. Permissions are read once at start;
changing them needs a restart.

A call to an id whose permission is missing rejects with a `KrateError`:

```json
{ "code": "permission_denied", "message": "...", "data": { "permission": "os:secrets.read" } }
```

This holds for every id, data and UI alike (`krate/window.open` without
`window` is a `permission_denied`, not a `not_found`). An id that does not exist at all is `not_found`, so the UI can tell a missing
permission from a typo. `isPermissionDenied(e)` and `deniedPermission(e)` from
`@krate/app/permission` read this, and `query(name)` (host id
`krate/permission.query`, always allowed) asks whether a permission is granted.

## Ids

All ids are `krate/<area>.<operation>` with a lowerCamel operation name
(`krate/os.info`, `krate/kv.get`, `krate/window.open`). Your own Go functions keep
their Go names (`src/lib/tasks.List`).

| Id | Permission | Arguments | Result |
|----|------------|-----------|--------|
| `krate/os.appDataDir` | `os:info` | none | `string` |
| `krate/os.cacheDir` | `os:info` | none | `string` |
| `krate/os.configDir` | `os:info` | none | `string` |
| `krate/os.logDir` | `os:info` | none | `string` |
| `krate/os.tempDir` | `os:info` | none | `string` |
| `krate/os.info` | `os:info` | none | `OsInfo` |
| `krate/os.openExternal` | `shell:openExternal` | `url` | nothing |
| `krate/os.secretSet` | `os:secrets` | `key`, `value` | nothing |
| `krate/os.secretGet` | `os:secrets.read` | `key` | `string` or `null` |
| `krate/os.secretDelete` | `os:secrets` | `key` | nothing |
| `krate/fs.readFile` | `fs:read` | `path` | `Uint8Array` |
| `krate/fs.readText` | `fs:read` | `path` | `string` |
| `krate/fs.readDir` | `fs:read` | `path` | `DirEntry[]` |
| `krate/fs.stat` | `fs:read` | `path` | `FileInfo` |
| `krate/fs.exists` | `fs:read` | `path` | `boolean` |
| `krate/fs.writeFile` | `fs` | `path`, `data` | nothing |
| `krate/fs.writeText` | `fs` | `path`, `text` | nothing |
| `krate/fs.mkdir` | `fs` | `path` | nothing |
| `krate/fs.remove` | `fs` | `path`, `recursive?` | nothing |
| `krate/fs.rename` | `fs` | `from`, `to` | nothing |
| `krate/db.exec` | `db` | `sql`, `params?` | `{ rowsAffected, lastInsertId }` |
| `krate/db.query` | `db` | `sql`, `params?` | array of row objects |
| `krate/db.transaction` | `db` | `statements` | array of exec results |
| `krate/kv.get` | `kv` | `key` | value or `null` |
| `krate/kv.set` | `kv` | `key`, `value` | nothing |
| `krate/kv.delete` | `kv` | `key` | nothing |
| `krate/kv.keys` | `kv` | `prefix?` | `string[]` |
| `krate/updater.check` | `updater` | none | `{ version, notes, pubDate, size, critical, required, delta, downloadSize }` or `null` |
| `krate/updater.download` | `updater` | none (stream) | `{ done, total }` progress events |
| `krate/updater.apply` | `updater` | none | nothing |
| `krate/updater.relaunch` | `updater` | none | nothing |
| `krate/updater.markHealthy` | `updater` | none | nothing |
| `krate/updater.status` | `updater` | none | `{ version, channel, unconfirmed, rollback }` |
| `krate/updater.ackRollback` | `updater` | none | nothing |
| `krate/updater.setChannel` | `updater` | channel: string | nothing |
| `krate/permission.query` | none | `name` | `boolean` |
| `krate/window.open` | `window` | `options: WindowOptions` | `string` |
| `krate/window.list` | `window` | none | `string[]` |
| `krate/window.state` | `window` | `window: string` | `WindowState` |
| `krate/window.close` | `window` | `window: string` | nothing |
| `krate/window.setTitle` | `window` | `window: string, title: string` | nothing |
| `krate/window.setSize` | `window` | `window: string, width: number, height: number` | nothing |
| `krate/window.setPosition` | `window` | `window: string, x: number, y: number` | nothing |
| `krate/window.setAlwaysOnTop` | `window` | `window: string, on: boolean` | nothing |
| `krate/window.setFullscreen` | `window` | `window: string, on: boolean` | nothing |
| `krate/window.setFrameless` | `window` | `window: string, on: boolean` | nothing |
| `krate/window.minimize` | `window` | `window: string` | nothing |
| `krate/window.maximize` | `window` | `window: string` | nothing |
| `krate/window.restore` | `window` | `window: string` | nothing |
| `krate/window.focus` | `window` | `window: string` | nothing |
| `krate/window.show` | `window` | `window: string` | nothing |
| `krate/window.hide` | `window` | `window: string` | nothing |
| `krate/window.setPreventClose` | `window` | `window: string, on: boolean` | nothing |
| `krate/window.navigate` | `window` | `window: string, path: string` | nothing |
| `krate/window.events` | `window` | none (stream) | `WindowEvent` |
| `krate/window.fileDrops` | `window` | none (stream) | `FileDropEvent` |
| `krate/window.thisView` | `window` | none | `string` |
| `krate/chrome.state` | `window` | `window: string` | `ChromeState` |
| `krate/chrome.setTitleBar` | `window` | `window: string, style: "native" | "hidden" | "none"` | nothing |
| `krate/chrome.setBackdrop` | `window` | `window: string, name: Backdrop` | nothing |
| `krate/chrome.setVibrancy` | `window` | `window: string, material: Vibrancy | ""` | nothing |
| `krate/chrome.setTitleBarOverlay` | `window` | `window: string, overlay: TitleBarOverlay | null` | nothing |
| `krate/chrome.setTrafficLightPosition` | `window` | `window: string, x: number, y: number` | nothing |
| `krate/chrome.startDrag` | `window` | `window: string` | nothing |
| `krate/chrome.toggleMaximize` | `window` | `window: string` | nothing |
| `krate/chrome.setDocumentEdited` | `window` | `window: string, on: boolean` | nothing |
| `krate/chrome.setRepresentedFile` | `window` | `window: string, path: string` | nothing |
| `krate/chrome.attachSheet` | `window` | `window: string, child: string` | nothing |
| `krate/chrome.endSheet` | `window` | `window: string` | nothing |
| `krate/chrome.setTabbing` | `window` | `window: string, identifier: string, mode: TabbingMode` | nothing |
| `krate/chrome.mergeAllWindows` | `window` | `window: string` | nothing |
| `krate/chrome.moveTabToNewWindow` | `window` | `window: string` | nothing |
| `krate/chrome.tabs` | `window` | `window: string` | `string[]` |
| `krate/chrome.setThumbButtons` | `window` | `window: string, buttons: ThumbButton[]` | nothing |
| `krate/chrome.share` | `share` | `window: string, items: ShareItems` | nothing |
| `krate/chrome.setJumpList` | `app` | `tasks: JumpTask[]` | nothing |
| `krate/chrome.clearJumpList` | `app` | none | nothing |
| `krate/chrome.setActivationPolicy` | `app` | `policy: "regular" | "accessory" | "prohibited"` | nothing |
| `krate/chrome.setDockVisible` | `app` | `on: boolean` | nothing |
| `krate/chrome.events` | `window` | none (stream) | `ChromeEvent` |
| `krate/dialog.openFile` | `dialog` | `options: OpenOptions` | `string[]` |
| `krate/dialog.openFolder` | `dialog` | `options: OpenOptions` | `string[]` |
| `krate/dialog.saveFile` | `dialog` | `options: SaveOptions` | `string | null` |
| `krate/dialog.message` | `dialog` | `options: MessageOptions` | `number` |
| `krate/clipboard.writeText` | `clipboard` | `text: string` | nothing |
| `krate/clipboard.writeImage` | `clipboard` | `png: Uint8Array` | nothing |
| `krate/clipboard.writeFiles` | `clipboard` | `paths: string[]` | nothing |
| `krate/clipboard.clear` | `clipboard` | none | nothing |
| `krate/clipboard.readText` | `clipboard:read` | none | `string` |
| `krate/clipboard.readImage` | `clipboard:read` | none | `Uint8Array | null` |
| `krate/clipboard.readFiles` | `clipboard:read` | none | `string[]` |
| `krate/clipboard.write` | `clipboard` | `data: ClipboardData` | nothing |
| `krate/clipboard.writeFormat` | `clipboard` | `name: string, data: Uint8Array` | nothing |
| `krate/clipboard.readHtml` | `clipboard:read` | none | `string` |
| `krate/clipboard.readRtf` | `clipboard:read` | none | `string` |
| `krate/clipboard.formats` | `clipboard:read` | none | `string[]` |
| `krate/clipboard.readFormat` | `clipboard:read` | `name: string` | `Uint8Array | null` |
| `krate/menu.setApp` | `menu` | `items: MenuItem[]` | nothing |
| `krate/menu.popup` | `menu` | `window: string, x: number, y: number, items: MenuItem[]` | nothing |
| `krate/menu.events` | `menu` | none (stream) | `MenuEvent` |
| `krate/menu.setContextMenu` | `menu` | `window: string, view: string, options: ContextMenuOptions` | nothing |
| `krate/menu.contextEvents` | `menu` | none (stream) | `ContextMenuEvent` |
| `krate/tray.set` | `tray` | `options: TrayOptions` | nothing |
| `krate/tray.remove` | `tray` | none | nothing |
| `krate/tray.events` | `tray` | none (stream) | `TrayEvent` |
| `krate/shortcut.register` | `shortcut` | `handle: string, accelerator: string` | nothing |
| `krate/shortcut.unregister` | `shortcut` | `handle: string` | nothing |
| `krate/shortcut.events` | `shortcut` | none (stream) | `{id: string}` |
| `krate/notify.send` | `notify` | `notification: Notification` | nothing |
| `krate/notify.close` | `notify` | `id: string` | nothing |
| `krate/notify.events` | `notify` | none (stream) | `NotificationEvent` |
| `krate/app.quit` | `app` | none | nothing |
| `krate/app.relaunch` | `app` | none | nothing |
| `krate/app.supports` | `app` | `capability: string` | `boolean` |
| `krate/app.events` | `app` | none (stream) | `SecondInstance` |
| `krate/app.openedFiles` | `app` | none | `string[]` |
| `krate/app.openFileEvents` | `app` | none (stream) | `OpenFileEvent` |
| `krate/page.print` | `page` | `view?: string` | nothing |
| `krate/page.printToPdf` | `page` | `view: string, options?: PdfOptions` | `Uint8Array` |
| `krate/page.find` | `page` | `view: string, text: string, options?: FindOptions` | nothing |
| `krate/page.stopFind` | `page` | `view: string` | nothing |
| `krate/page.findEvents` | `page` | none (stream) | `FindEvent` |
| `krate/page.zoom` | `page` | `view: string` | `number` |
| `krate/page.setZoom` | `page` | `view: string, factor: number` | nothing |
| `krate/page.capture` | `page` | `view: string` | `Uint8Array` |
| `krate/downloads.configure` | `downloads` | `options: DownloadOptions` | nothing |
| `krate/downloads.list` | `downloads` | none | `DownloadInfo[]` |
| `krate/downloads.cancel` | `downloads` | `id: number` | nothing |
| `krate/downloads.events` | `downloads` | none (stream) | `DownloadEvent` |
| `krate/cookies.list` | `cookies` | `url?: string` | `Cookie[]` |
| `krate/cookies.get` | `cookies` | `name: string, url?: string` | `Cookie | null` |
| `krate/cookies.set` | `cookies` | `cookie: CookieInput, url?: string` | nothing |
| `krate/cookies.delete` | `cookies` | `name: string, url?: string` | nothing |
| `krate/cookies.clearSiteData` | `cookies` | `options?: ClearOptions` | nothing |
| `krate/remote.open` | `remote` | `options: RemoteWindowOptions` | window id |
| `krate/remote.addView` | `remote` | `window: string, options: RemoteViewOptions` | view id |
| `krate/remote.navigate` | `remote` | `view: string, url: string` | nothing |
| `krate/remote.post` | `remote` | `view: string, targetOrigin: string, data: unknown` | nothing |
| `krate/remote.messages` | `remote` | stream | `RemoteMessage` |
| `krate/remote.events` | `remote` | stream | `RemoteEvent` |
| `krate/remote.capabilities` | `remote` | none | `RemoteCapabilities` |
| `krate/remote.setRules` | `remote:intercept` | `partition: string, rules: RemoteRule[]` | nothing |
| `krate/remote.requests` | `remote:intercept` | stream | `RemoteRequest` |
| `krate/remote.setUserAgent` | `remote:network` | `partition: string, userAgent: string` | nothing |
| `krate/remote.setProxy` | `remote:network` | `partition: string, proxy: RemoteProxy` | nothing |
| `krate/remote.setClientCertificate` | `remote:network` | `partition, host, certFile, keyFile: string` | nothing |
| `krate/remote.clearData` | `remote:network` | `partition: string, options?: RemoteClearOptions` | nothing |
| `krate/remote.certificates` | `remote:network` | stream | `RemoteCertificateError` |
| `krate/remote.answerCertificate` | `remote:network` | `id: number, allow: boolean` | nothing |
| `krate/remote.auth` | `remote:network` | stream | `RemoteAuthChallenge` |
| `krate/remote.answerAuth` | `remote:network` | `id: number, user: string, password: string, ok: boolean` | nothing |

`packages/native/sdk/ids.go` is the source of truth for this table; a Go test
checks it against the registrations, the TypeScript wrappers and this page.

## os

Go: `package sdkos` (`packages/native/sdk/os`).

```go
dir, err := sdkos.AppDataDir()            // also CacheDir, ConfigDir, LogDir, TempDir
info := sdkos.GetInfo()                   // OS, Arch, OSVersion, Hostname, Locale, AppID, AppVersion
err = sdkos.OpenExternal(ctx, "https://example.com")
err = sdkos.SecretSet(ctx, "api-token", token)
v, err := sdkos.SecretGet(ctx, "api-token") // sdkos.ErrSecretNotFound when missing
```

Each directory is named after the `appId` and created on first use with mode
0700:

| Directory | Linux | macOS | Windows |
|-----------|-------|-------|---------|
| App data | `$XDG_DATA_HOME/<id>` (`~/.local/share`) | `~/Library/Application Support/<id>` | `%APPDATA%\<id>` |
| Cache | `$XDG_CACHE_HOME/<id>` (`~/.cache`) | `~/Library/Caches/<id>` | `%LOCALAPPDATA%\<id>\Cache` |
| Config | `$XDG_CONFIG_HOME/<id>` (`~/.config`) | `~/Library/Application Support/<id>/config` | `%APPDATA%\<id>\Config` |
| Log | `$XDG_STATE_HOME/<id>/log` (`~/.local/state`) | `~/Library/Logs/<id>` | `%LOCALAPPDATA%\<id>\Logs` |
| Temp | `$XDG_RUNTIME_DIR/<id>`, else `<tmp>/<id>-<uid>` | `<tmp>/<id>` | `<tmp>\<id>` |

A Go host child process learns the app from `KRATE_APP_ID`, `KRATE_APP_NAME`
and `KRATE_APP_VERSION`, which the shell sets; a Go host that is not started by
the shell calls `sdkos.Configure`.

`OpenExternal` accepts only `http`, `https` (with a host) and `mailto` URLs;
`sdkos.AllowScheme` adds more. It starts `xdg-open`, `open` or
`rundll32 url.dll,FileProtocolHandler` and returns once the handler has started.

### Secrets

Secrets are stored in the OS credential store, scoped to the app id and the
key:

| Platform | Store |
|----------|-------|
| Linux | A Secret Service provider (gnome-keyring, KWallet, KeePassXC) over the session D-Bus, with a built-in pure Go D-Bus client. A locked keyring asks the user to unlock it. |
| macOS | The login Keychain, generic passwords, through Security.framework. |
| Windows | The Credential Manager, generic credentials named `<appId>/<key>`; a value holds at most 2560 bytes. |

Where there is no store (a Linux machine without a Secret Service provider or a
session bus), the calls fail with `krate.ErrUnsupported` in Go and code
`unsupported` in the UI. Check first with `krate.Supports("os.secrets")`.
Capabilities registered by the SDK: `os.info`, `os.openExternal`,
`os.secrets`, `fs`, `db`, `kv`, `net`.

## fs

Go: `package fs` (`packages/native/sdk/fs`). These functions are for paths that
come from the UI; Go host code uses the standard library directly.

A path may be absolute, or relative to the app data directory. It is cleaned,
resolved through symlinks and must then lie inside an allowed root:

- the app directories (data, cache, config, log, temp), and
- paths granted at run time with `fs.Grant(path)`. The dialog SDK calls it when
  the user picks a file or folder. A granted folder covers its subtree; a
  granted file covers just that file.

Anything else fails with code `permission_denied` and data
`{ "permission": "fs", "reason": "outside_scope", "path": "..." }`, including
`..` traversal and symlinks that point outside. `exists` on such a path fails
too, rather than answering. `remove` and `rename` act on a symlink itself, never
its target, and an allowed root cannot be removed or moved. Reads are limited to
64 MiB. The check and the use are not atomic, so the scope protects against a
buggy or hostile page, not a hostile local process.

Other error codes: `not_found`, `already_exists`, `access_denied` (the OS said
no), `is_directory`, `too_large`.

## db

Go: `package db` (`packages/native/sdk/db`), on `modernc.org/sqlite` (no cgo).

```go
d, err := db.Open("app")                       // app.db in the app data directory
res, err := d.Exec(ctx, "INSERT INTO t(a) VALUES(?)", 1)
rows, err := d.Query(ctx, "SELECT * FROM t")    // []map[string]any; QueryRows keeps column order
err = d.Transaction(ctx, func(tx *db.Tx) error { _, err := tx.Exec(ctx, "..."); return err })
```

`db.Default()` is the shared `app.db` that the UI ids and `kv` use. The
database opens in WAL mode with a 5 second busy timeout, foreign keys on and
write transactions that take the lock up front. The shell and a Go host child
process can use the same file at the same time.

Values are `int64`, `float64`, `string`, `[]byte` (blob) and `nil`; a `bool`
binds as 0 or 1 and a date as an ISO 8601 string. In the UI a blob is a
`Uint8Array`. Integers beyond 2^53 lose precision in JavaScript.

SQL from the UI may not use `ATTACH`, `DETACH` or `VACUUM` (code
`forbidden_sql`), because they open or write arbitrary files. `transaction`
takes `{ sql, params? }` statements and runs them all or not at all; its error
names the failing statement. Error codes: `sql_error`, `constraint`, `busy`.

## kv

Go: `package kv` (`packages/native/sdk/kv`), a `kv(key, value)` table in `app.db`.

```go
s, _ := kv.Default()
err := s.SetString(ctx, "theme", "dark")
v, ok, err := s.GetString(ctx, "theme")
keys, err := s.Keys(ctx, "recent/")
```

Values are stored as wire frames, so JSON values, bytes, dates, maps and sets
round-trip unchanged from the UI. `SetWith`/`GetWith` take wire encoder and
decoder callbacks for other types. `get` answers `null` for a missing key.

## net

Go: `package sdknet` (`packages/native/sdk/net`). The webview has `fetch`, so
there is no UI id.

```go
client := sdknet.Client()                     // 60 s timeout, bounded dial/TLS/header waits
req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
err := sdknet.WithSecret(ctx, req, "api-token", "Authorization")
```

`WithSecret` reads a secret and sets the header, so the secret never passes
through the UI. A client from `Client` does not forward that header to another
host on redirect.
