# krate-plugin-demo-go

A community Krate plugin written in Go, demonstrating:

- A Go plugin source (`main.go`) using the `github.com/kratejs/krate/packages/compiler/pluginsdk` SDK.
- The `BeforeBuild`, `AfterParse` (editing the live AST), and `AfterRender`
  build hooks.
- The request-time `ServeRequest` and `ServeResponse` serve hooks.
- A static ES-module descriptor (`index.js`, `export default`, package `"type": "module"`) that reports `runtime: "go"`, its
  own `module` URL, and per-platform binaries in `bin/`.

## Layout

```
main.go     Plugin source; calls plug.Serve("krate-plugin-demo-go", plug.Hooks{...})
go.mod      Standalone module (replaces github.com/kratejs/krate/packages/compiler to the local SDK)
index.js    Descriptor factory Krate runs to discover runtime, module, + binaries
bin/        Per-platform plugin binaries (built, not committed)
```

## Build

From the repo root, cross-compile every platform declared in `index.js`:

```
node scripts/build-go-plugin.mjs examples/plugins/krate-plugin-demo-go
```

Or a single platform: `node scripts/build-go-plugin.mjs <dir> linux-amd64`.
(The example also ships an npm `build` script; run `npm run build` inside the
plugin directory.)

Output binaries land in `bin/` (gitignored). The following `go build` commands
show the equivalent manual invocation, one per platform:

```sh
GOOS=windows GOARCH=amd64 go build -o bin/krate-plugin-demo-go-windows-amd64.exe .
GOOS=darwin  GOARCH=amd64 go build -o bin/krate-plugin-demo-go-darwin-amd64   .
GOOS=darwin  GOARCH=arm64 go build -o bin/krate-plugin-demo-go-darwin-arm64   .
GOOS=linux   GOARCH=amd64 go build -o bin/krate-plugin-demo-go-linux-amd64    .
GOOS=linux   GOARCH=arm64 go build -o bin/krate-plugin-demo-go-linux-arm64    .
```

The paths above must match the `binaries` map in `index.js`.

## Install

Ship the built `bin/` plus `index.js` as an npm package, then wire it into
`krate.config.ts`:

```ts
import demoGoPlugin from 'krate-plugin-demo-go';
export default { plugins: [demoGoPlugin()] };
```

Krate discovers the manifest, picks the binary for the host `GOOS-GOARCH`,
spawns it via HashiCorp go-plugin, and keeps it alive across dev hot-reloads.
