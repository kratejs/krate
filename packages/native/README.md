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
