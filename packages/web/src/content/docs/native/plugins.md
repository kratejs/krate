---
title: Native Plugins
order: 11
description: Ship reusable native capabilities as plugins: a Go package and a manifest, generated TypeScript, declared permissions, enabled with one line of config.
---

# Native Plugins

> **Preview.** Part of [Krate Native](/docs/native/), which is under construction.
> Verified on Linux; macOS and Windows are [untested](/docs/guides/compatibility/#krate-native).

A plugin is reusable native code: a Go package that runs inside the shell
process, a `krate-plugin.json` manifest that says what it offers and which
permissions it defines, and a TypeScript module the compiler generates from the
Go code. An app turns one on with one line:

```ts
// krate.config.ts
export default defineConfig({
  native: {
    appId: "com.acme.app",
    plugins: ["./native-plugins/system-info"],
  },
});
```

```tsx
import { info, cpuLoad, load } from "krate/plugin/system-info";

const sys = await info();                        // { os, arch, hostname, cpus, goVersion }
for await (const pct of cpuLoad(500)) draw(pct);  // a stream
<p>CPU {load()}%</p>                              // a host signal
```

There is no glue to write: the ops are generated exactly like
[Go host code](/docs/native/go-host/), with the same codecs, types, streams and
signals. `examples/native-plugin` is a complete app with a real plugin
(`system-info`: OS, CPU and memory information and the CPU load).

## Using a plugin

`native.plugins` lists where the plugins are:

| Entry | Meaning |
|-------|---------|
| `"./native-plugins/hello"` | A directory with a `krate-plugin.json` (relative to the project, or absolute; on Windows `.\native-plugins\hello` and `C:\path` forms work too, and paths with spaces do not). It is a Go module of its own, or lives inside the app's module. |
| `"@acme/krate-sqlite"` | An npm package that ships a plugin: `node_modules/@acme/krate-sqlite/krate-plugin.json`, or the directory its `package.json` names in `"krate": {"plugin": "go/krate-plugin.json"}`. |
| `"go:github.com/acme/krate-sqlite@v1.2.0"` | A Go module, fetched with `go mod download`. Without `@version` the app's `go.mod` decides (it has to require the module). The manifest is at the module root. |

Import the plugin's ops from `krate/plugin/<name>`. The `krate native init`
tsconfig already includes `.krate/native/plugins`, where the build writes the
types (`<name>.d.ts` and an ambient `modules.d.ts`); add the directory to
`include` in an older project.

An app with plugins needs the **Go toolchain**, because plugins are Go code
linked into the shell. `krate native doctor` reports it as required as soon as
`native.plugins` is set, and the prebuilt `krate-shell` is not used for such an
app (see [Dev and release builds](#dev-and-release-builds)). A plugin written
for a newer Krate than the one you run is an error that says so (`KR7254`).

## Permissions

A plugin declares permissions in its manifest and gates each op with one. They
are named `plugin:<plugin>.<permission>` (for `system-info`:
`plugin:system-info.read` and `plugin:system-info.monitor`) and work like the
[SDK permissions](/docs/native/sdk/#permissions):

- A build **derives** the plugin permissions from the code: only the permissions
  of the ops your pages import or name (`"plugin/<name>.<op>"`) join the list in
  `app.json`. `krate native check` prints them with the plugin and the call
  sites, and `dist-native/report.json` lists them.
- `native.permissions` is an allowlist for them too: a derived permission it
  lacks fails the build (`KR7210`), and a name no enabled plugin declares is
  `KR7255`.
- A denied call fails with `permission_denied` and the permission in `data`.
- **Per-window scopes apply.** A window narrowed with `native.window.permissions`
  or the `permissions` option of `window.open` can only call the plugin ops
  whose permission it keeps (an op with no permission is always allowed). A
  host signal is checked when the page subscribes.
- Go host code and TypeScript host code are trusted, as with the SDK: they reach
  an op through its ungated implementation (`__krateHost.call("plugin/<name>.<op>")`).

A plugin can only register ids in its own namespace, `plugin/<name>.`. The
shell collects what a plugin registers before it forwards anything and refuses
the plugin (the app does not start) when an id lies outside the namespace or
names an op the manifest does not list.

A plugin is trusted code, like any dependency you link into a Go program: it
runs with the privileges of the app. The namespace and the permissions limit
what the **page** can reach through it; they are not a sandbox for the plugin.

## Writing a plugin

```sh
krate native plugin init sqlite          # ./native-plugins/sqlite
krate native plugin init sqlite ./tools/sqlite
```

This writes a Go module with an example op and stream, a test and a manifest:

```json
{
  "name": "system-info",
  "version": "0.1.0",
  "krate": "0.5.0",
  "go": "github.com/kratejs/krate-plugin-system-info",
  "permissions": [
    { "name": "read", "description": "read the operating system, CPU count and memory" },
    { "name": "monitor", "description": "watch the CPU load over time" }
  ],
  "ops": [
    { "name": "info", "permission": "read" },
    { "name": "cpuLoad", "go": "CPULoad", "permission": "monitor" },
    { "name": "load", "permission": "monitor" }
  ]
}
```

| Field | Meaning |
|-------|---------|
| `name` | Lowercase words joined by hyphens, 2 to 40 characters. It is the namespace: the op `info` is the host id `plugin/system-info.info`. Two enabled plugins cannot share a name. |
| `go` | The import path of the Go package, the directory that holds the manifest: the module path of the nearest `go.mod` plus the directory inside it. A mismatch is `KR7253`. |
| `version`, `description` | Informational. `version` shows in `report.json`. |
| `krate` | The oldest Krate version the plugin works with (`"0.5.0"` or `">=0.5.0"`). |
| `ts` | An optional TypeScript entry that the plugin's npm package ships next to the generated module (for helpers built on the ops). |
| `permissions` | `{name, description}`; names are lowerCamel (`read`, `readAll`), like the SDK ids and permission parts. The description appears in the permission table. |
| `ops` | `{name, go, permission, manual}`. `name` is lowerCamel like the SDK ops (`info`, `cpuLoad`): it is the id suffix and the export of the generated module. It binds to the exported Go function or `*krate.Signal[T]` variable with the first letter upper-cased (`info` is `Info`); set `go` when the Go name differs (`"go": "CPULoad"` for `cpuLoad`, since an acronym cannot be derived). An id that is not lowerCamel is refused at load. `permission` names one of the declared permissions, or is omitted for an op that is always allowed. Duplicates and unknown permissions are `KR7251` and `KR7252`. |

The manifest is authoritative: an exported Go function that is not listed is not
reachable from a page. Signatures follow the
[Go host code rules](/docs/native/go-host/#signatures): an optional first
`krate.Ctx`, arguments, and a result, `error`, `(T, error)`, a channel for a
stream. A signature the bindings cannot encode is reported with the Go error
and the position in the plugin (`KR7180` to `KR7185`).

### The plugin context

A plugin that needs the platform, lifecycle hooks or a data directory exports
`Init`:

```go
import "github.com/kratejs/krate/packages/native/plugin"

func Init(c *plugin.Context) error {
    c.OnStart(func() error { /* the platform is up; main thread */ return nil })
    c.OnWindowCreated(func(id string) {})
    c.OnWindowClosed(func(id string) {})
    c.OnQuit(func() {})
    dir, _ := c.DataDir() // <app data dir>/plugins/<name>
    return nil
}
```

The window hooks fire for windows only. A view added to a window with
`window.addView` (a split pane) is not a window and does not trigger them.

`Init` runs once while the shell loads, before the platform exists; do anything
that needs a window, a dialog or the main thread from `OnStart` or after. The
context offers:

- `Dispatch(fn)`: run `fn` on the main thread and return at once (queued until the
  platform starts; on its own goroutine in browser mode).
- `App()`, `Windows()`, `Window(id)`, `WindowControls(id)`, `Dialogs()`,
  `Menus()`, `Clipboard()`, `Tray()`, `Notifier()`: the platform and its optional
  capabilities from `platform/caps`. A missing capability answers
  `krate.ErrUnsupported`. On Linux the tray and notifications are drawn over
  D-Bus by the `sdk/tray` and `sdk/notify` packages, which a plugin can import
  like host code does; the SDK areas a plugin imports stay linked in release
  builds.
- `AppID()`, `AppName()`, `AppVersion()`, `Dev()`, `Logf(...)`, `AppDataDir()`,
  `DataDir()`.
- `Func(op, f)` and `Stream(op, f)`: register a hand-written op for an op the
  manifest declares with `"manual": true`. The id and the permission come from
  the manifest; anything else is refused. The generated module types such an op
  loosely (`(...args: unknown[]) => Promise<unknown>`).

Hooks run in the order the plugins are listed, and quit hooks in reverse. A
hook that fails or panics is logged; the app keeps running.

### Distributing a plugin

- **As a Go module.** Tag the module and apps list
  `"go:github.com/you/krate-plugin-x@v1.0.0"`. The plugin's `go.mod` requires
  `github.com/kratejs/krate/packages/native` at a released version.
- **Inside an npm package.** Ship the plugin directory (`krate-plugin.json`,
  `go.mod`, the Go sources) in the package; apps list the package name. Add
  `"krate": {"plugin": "<dir>"}` to `package.json` when the manifest is not at
  the package root.
- **In the repository.** A directory next to the app, as in
  `examples/native-plugin`.

## Dev and release builds

Plugins are Go code, so an app with plugins gets a **shell built for it**. The
build writes a Go module, `.krate/native/pluginshell`, with the generated glue of
each plugin, a registry package and a main, and requires the native module and
the plugin modules (replaced by directory, or by version for a `go:` plugin).

| | Prebuilt `krate-shell` (no plugins) | App with plugins |
|---|---|---|
| `krate native dev` | the shell next to `krate` | a shell built with `go build`, cached in `.krate/native/bin/shell-<key>` by the plugin set, the plugins' sources, the Krate version and the platform; every op is linked |
| editing a plugin | not applicable | a change to a local plugin's Go code, `go.mod` or manifest restarts `krate native dev` (the shell has to be rebuilt; the Go host child alone cannot) |
| `krate native build` | the prebuilt shell next to `app/` | one executable (Go host code, if any, is linked in too); `--layout shell` gives `app/` next to the per-app shell |
| `krate native build --release` | one executable | one executable that links **only the plugin ops the code uses** |

- In dev, the permissions of the plugin ops your code uses are added to the
  SDK defaults when `native.permissions` is empty; a configured list is used as
  written. A release build derives everything.
- A release (and a derived `krate native build`) leaves out the ops no code
  reaches and the permissions only they need. A namespace import
  (`import * as p from "krate/plugin/x"`) that escapes, a dynamic `import()` and
  an unknown `"plugin/x.op"` id count as the whole plugin. Hand-written
  (`manual`) ops register from `Init` and are always linked; their permission
  still has to be derived. `report.json` lists, per plugin, the ops it kept
  and the ones it left out.
- `--shell` and `$KRATE_SHELL` do not apply to an app with plugins: native dev
  refuses them. A prebuilt shell that opens an app directory that lists plugins
  says that it does not link them instead of failing on unknown permissions.
- With `native.release.compile: "never"` (or no `go` command) there is no
  prebuilt layout for such an app: the build fails with `KR7212`.
- The first build of a shell compiles the native module and the plugins, which
  takes a while; later builds hit the Go build cache.

## Diagnostics

| Code | Meaning |
|------|---------|
| `KR7250` | A plugin has no `krate-plugin.json`, or the npm package is not installed. |
| `KR7251` | The manifest is malformed or a field is invalid (bad name, op or permission name, unknown permission). |
| `KR7252` | Duplicate plugin name, op or permission. |
| `KR7253` | The `go` import path does not match the package, the plugin is not in a Go module, or a `go:` module cannot be resolved. |
| `KR7254` | The plugin needs a newer Krate. |
| `KR7255` | A plugin permission in `native.permissions` or `native.window.permissions` that no enabled plugin declares. |
| `KR7256` | `go build` of the shell for the plugins failed (the output is shown). |
| `KR7257` | An op the manifest lists is not an exported function or signal of the Go package (mark it `manual` if the plugin registers it from `Init`). Signatures the bindings cannot encode keep the Go host codes `KR7180` to `KR7185`. |

See [Diagnostic codes](/docs/reference/diagnostics/).
