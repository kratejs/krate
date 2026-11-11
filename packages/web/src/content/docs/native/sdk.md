---
title: "Native SDK: Data and System"
order: 6
description: The Krate Native SDK data half - app directories and OS info, scoped files, SQLite, key-value storage, secrets, networking and permissions - for Go host code and for the UI.
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

`app.json` has a `permissions` list. Go host code is trusted and never checked;
permissions decide which ids the UI and TypeScript host code may call.

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

An empty or missing list means the defaults: the non-sensitive set marked
"yes" above. The compiler will later derive the list from the SDK calls an app
can reach and `native.permissions` in the config will narrow it; until then
list what a sensitive feature needs (`tray`, `shortcut`, `clipboard:read`,
`fs`, secrets, `shell:openExternal`). A
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
| `krate/menu.setApp` | `menu` | `items: MenuItem[]` | nothing |
| `krate/menu.popup` | `menu` | `window: string, x: number, y: number, items: MenuItem[]` | nothing |
| `krate/menu.events` | `menu` | none (stream) | `MenuEvent` |
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
