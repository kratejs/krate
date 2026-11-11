# github.com/kratejs/krate/packages/native

The Go module behind Krate's desktop target.

| Package | Role |
| --- | --- |
| `krate` | Core API for Go host code: `Ctx`, `Signal`, `Errorf` |
| `wire` | Frame codec for the host call protocol (see `PROTOCOL.md`) |
| `scheme` | The in-process `http.Handler` for the app origin: static files, calls, streams, signals |
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
