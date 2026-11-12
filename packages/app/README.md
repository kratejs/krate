# @krate/app

Client runtime and host bindings for Krate desktop apps. UI code (TSX in the
system webview) imports host functions; the compiler rewrites those imports into
calls into this package. See `packages/native/PROTOCOL.md` for the wire format.

- `@krate/app`: `call`, `stream`, `hostSignal.signal(name)`, `KrateError`, `app`.
- `@krate/app/client`: `createClient({ fetch?, origin?, windowId?, createSignal? })`,
  the transport used by generated stubs.
- `@krate/app/host`: `hostSignal(initial)` for host-side TS code.
- `@krate/app/testing`: `mockHost(moduleId, impl)`, `resetHostMocks()`,
  `createFakeHost(registry)` (an in-memory fetch speaking the protocol).

## Signals

`signal(name)` returns a callable getter (`Count()`) with `.subscribe(fn)`. The
value is `undefined` until the first frame arrives. To make reads reactive,
pass the runtime's factory once at startup:

```ts
import { createSignal } from "@krate/runtime";
import { createClient, setClient } from "@krate/app";
setClient(createClient({ createSignal }));
```

`@krate/runtime` is not imported by this package, so there is no dependency
cycle and no hard dependency.

## Codec notes

The wire format, including the escape rule for all four tags (`$b`, `$d`, `$m`,
`$s`), is specified in `packages/native/PROTOCOL.md`; `src/wire.ts` implements
it. The golden vectors in `packages/native/wire/testdata` are shared with the Go
codec and run by `test/shared-vectors.test.ts`. A Go `map` arrives as a plain
object, not a `Map`; a `Map` or `Set` argument is accepted by Go as an object
(string keys) / array. Calls with binary arguments are never deduplicated.

## SDK modules

These wrap the Go SDK (`packages/native/sdk`). Each call is a host id
`krate/<area>.<name>`; `packages/native/sdk/ids.go` lists every id with its
permission and types, and a Go test checks these files against it. The app's
`permissions` list in `app.json` decides which ids work; a call without its
permission rejects with `KrateError` code `permission_denied` and
`data.permission`. See the docs page "Native SDK: data" for the tables.

- `@krate/app/os`: `appDataDir`, `cacheDir`, `configDir`, `logDir`, `tempDir`,
  `info`, `openExternal`, `secretSet`, `secretGet`, `secretDelete`.
- `@krate/app/fs`: `readFile`, `readText`, `readDir`, `stat`, `exists`,
  `writeFile`, `writeText`, `mkdir`, `remove`, `rename` (scoped to the app
  directories and paths the user granted in a dialog).
- `@krate/app/db`: `query`, `exec`, `transaction` on the app's SQLite database.
- `@krate/app/kv`: `get`, `set`, `delete`, `keys`.
- `@krate/app/permission`: `query`, `isPermissionDenied`, `deniedPermission`.

## Desktop SDK (UI half)

`@krate/app/updater` (`check`, `download`, `apply`, `relaunch`, `update`) needs the
`updater` permission and `native.updater` in the config.

`@krate/app/window`, `/dialog`, `/clipboard`, `/menu`, `/tray`, `/shortcut`,
`/notify` and `/app` call the host functions `krate/<area>.<Name>` and subscribe
to the streams `krate/<area>.events` (`onMenu(id, fn)`, `onClick`, `onEvent`,
`onCloseRequested`, `onAction`, `onSecondInstance`). Each id is gated by the
same `permissions` list as the data modules (a missing permission rejects with
`permission_denied`; `tray`, `shortcut` and `clipboard:read` are off unless
listed); see the SDK pages in the Native docs.

`@krate/app/chrome` controls window chrome: title bar styles, backdrops and vibrancy, the
Windows caption buttons, traffic lights, document state, sheets, native tabs, thumbnail buttons,
jump lists, the share menu and the dock. Calls a platform lacks reject with `unsupported`; see
the Window chrome page in the Native docs.

`@krate/app/window` also splits a window into several webviews: `main().addView({ path,
side, size, minWidth, permissions })` returns a `View` (`remove`, `focus`, `navigate`,
`post`), `window.setLayout(split(...))` and `window.layout()` set and read the pane
tree, `onViewEvent` reports added, navigated, removed and closed views, and
`onViewMessage` receives what another view `post`ed. See the Views page of the Native docs.

