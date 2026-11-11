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
| `cmd/krate-shell` | The prebuilt shell |

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
