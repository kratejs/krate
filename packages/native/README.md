# github.com/kratejs/krate/packages/native

The Go module behind Krate's desktop target.

| Package | Role |
| --- | --- |
| `krate` | Core API for Go host code: `Ctx`, `Signal`, `Errorf` |
| `wire` | Frame codec for the host call protocol (see `PROTOCOL.md`) |
| `scheme` | The in-process `http.Handler` for the app origin: static files, calls, streams, signals |
| `jsruntime` | The embedded QuickJS engine: VM, Web API prelude, Go-backed fetch and crypto, binary transfer. Shared with the compiler |
| `hostjs` | A persistent QuickJS VM that runs an app's TypeScript host code and registers it with `scheme` |
| `platform` | Windows, macOS and Linux windows and webviews, no cgo |
| `gohost` | The Go host child: `Serve` for the generated child main, `Child` for the parent that runs and restarts it |
| `shell` | The shell as a library: app manifest, `Run`, `RunEmbedded` and `Main` (release builds), browser mode, dev control. See `shell/README.md` |
| `sdk/{window,chrome,dialog,clipboard,menu,tray,shortcut,notify,app,media}` | The UI half of the SDK: plain Go functions for host code, forwarded to the shell in dev. `sdk` assembles them for the shell. See the SDK page in the docs |
| `sdk/{badge,screen,power,login,theme,crash}` | The OS integration areas: app icon badge and progress, displays, power and idle, launch at login, the system theme, crash reports and webview crash events. See the OS integration page of the docs |
| `crashreport` | Crash report files (JSON under the app's log directory) and the Go crash hook; no dependencies, used by the shell, the Go host child and `sdk/crash` |
| `cmd/krate-shell` | The prebuilt shell binary. `krate-shell --doctor` prints a JSON report of the system webview and display; it ships in the `@krate/core-<platform>` packages next to `krate` |
| `plugin` | The native plugin runtime: `Plugin`, `Meta`, `Host` (installs a plugin's ops under `plugin/<name>.`, gates them by permission and window scope, runs the lifecycle hooks) and `Context` (what a plugin's `Init` gets). See the Native plugins docs page |
| `memstat` | Per-process memory (PSS and private dirty on Linux, footprint on macOS, private bytes on Windows) with roles for the shell, host children and webview processes; behind the shell's `memory` dev command |
| `platform/probe` | The result type of that webview probe, shared by the per-OS platform packages |

Size, start-up and memory budgets are in `budgets.json`; see `BUDGETS.md`.
The macOS (WKWebView) and Windows (WebView2) memory levers run in the CI native job, which prints the measured numbers; no test asserts what a lever saves. See the "macOS and Windows" section of the Memory docs page.

The module never uses cgo. It cross-compiles from any OS with
`CGO_ENABLED=0`.

## Wiring

Generated glue registers each exported Go function with a `scheme.Registry`
and encodes types with `wire.Encoder` / `wire.Decoder`; no reflection runs on
the call path. `scheme/example_test.go` is a hand-written reference of that
glue. The default Content-Security-Policy allows inline scripts and styles
because prerendered pages carry them; see `scheme.DefaultCSP`.

## Host code in JavaScript

`hostjs` runs a bundled host program (an IIFE that calls
`__krateHost.register(id, fn, "func" | "stream")` and
`__krateHost.registerSignal(id, signal)`) in one QuickJS VM that lives as long
as the app. `hostjs.New` creates it, `Load` evaluates the bundle with its source
map, and `Register` adds everything the bundle registered to a
`scheme.Registry`. Calls, timers and I/O completions run on a single VM
goroutine; `fetch` runs on its own goroutines and never blocks it.
`Reload` swaps the bundle in place for development. See the package comment for
the details and limits.

## Go host in a child process

An app with Go host code runs it in a separate process so that a Go edit
restarts only the child and the window survives. The generated child main is
`gohost.Serve(glue.Register)`. The shell starts it through `gohost.Child`,
registers `scheme.Remote` forwarders for the ids listed in `app.json`, and
restarts it on demand; signals reconnect with backoff, so the page sees the
new child's current values without reloading. The child protocol (unix socket
in `KRATE_GOHOST_SOCKET`, `ready` line, exit on stdin EOF) is in the
`gohost` package comment.

## Release builds

`krate native build --release` generates a main package that links the shell
into one executable: `shell.Main(shell.MustZipFS(appZip), glue.Register)`. The app
directory is embedded as a zip, Go host code is registered in process through
`RunOptions.GoGlue` (no child), and `--browser`, `--addr` and `--version` are
the only flags (`--dev` exists only with the `krate_devcontrol` build tag, which
the end-to-end tests use). Build tags leave code out:
`krate_no_<area>` for `fs`, `db`, `kv`, `net`, `window`, `chrome`, `dialog`, `clipboard`,
`menu`, `tray`, `shortcut`, `notify`, `app`, `badge`, `screen`, `power`, `login` and `crash` (each area registers itself from
`area_<name>.go`, `sdk/area_<name>.go` and `sdk/all/area_<name>.go`), and
`krate_no_hostjs` for QuickJS and the TypeScript host. `KRATE_CPUPROFILE=<file>`
writes a CPU profile on exit. `sdk/ids.json` is the id table the compiler reads
(`go test ./sdk -run IDsJSON -update-ids` rewrites it; copy it to
`packages/compiler/internal/native/capability/ids.json`).

## Views and split panes

A window can hold several webviews as split panes (`platform/caps` `WindowViews`,
SDK ids `krate/window.addView` and friends, the `views` page of the docs). The
layout arithmetic (tree to rectangles, divider hit-testing and drag, minimum
sizes) is pure Go in `platform/caps/layout.go`; Linux nests `GtkPaned`, macOS
nests `NSSplitView`, Windows places several WebView2 controllers from that Go
layout. Every request on the app scheme carries the webview it came from
(`caps.ViewOf`), which the shell maps to a window or view scope; the page's
`x-krate-window` header is ignored in a native window.

## Files

Open-file events (`sdk/app` `OnOpenFile`, `OpenedFiles`, `FilesFromArgs`) and file drops (`sdk/window` `OnFileDrop`) grant what the user opened or dropped to the `fs` scope, like a dialog pick. The platform side is two optional capabilities, `caps.OpenFiles` (macOS delegate) and `caps.FileDrops` (GTK drag signals, a `WKWebView` subclass, WebView2 additional objects). An app with `native.fileAssociations` takes the single-instance lock (`app.json` `singleInstance`). See the Files docs page.

## Plugins

A plugin is a Go package plus a `krate-plugin.json`; `krate native dev` and
`krate native build` generate its glue with the same bindings as Go host code and
link it into a per-app shell: `shell.CLI(args, plugins)` for dev,
`shell.MainPlugins(fsys, glue, plugins)` for a release. `RunOptions.Plugins`
carries the linked plugins; the manifest's `plugins` list says which are
active. A plugin cannot register an id outside `plugin/<name>.` (`Host.Install`
refuses it), and its ops are gated by `plugin:<name>.<permission>` and the
calling window's scope.

## UI half of the SDK

The `sdk` packages cover windows, dialogs, the clipboard, menus, the tray,
global shortcuts, notifications and the app lifecycle. `sdk.Host` registers
them for the shell: local dispatch for code linked into the shell, a private
unix socket (`KRATE_GOHOST_PARENT`) for the Go host child, and the
permission-filtered `krate/<area>.<Name>` ids for the page. Optional platform
capabilities are interfaces in `platform/caps`; an SDK call answers
`krate.ErrUnsupported` when the platform lacks one.

