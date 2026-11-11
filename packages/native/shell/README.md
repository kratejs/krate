# shell

The Krate Native shell as a library. `cmd/krate-shell` is a thin main around
`shell.Run`; release builds link the same pieces into one process.

`shell.Run(appDir, opts)` loads the app directory, builds a `scheme.Registry`
(TypeScript host code through `hostjs`, Go host code through a `gohost` child
process), serves the built UI with `scheme.New`, opens the window and blocks
until quit. In native mode it must be called from the main goroutine of a
process that called `runtime.LockOSThread` in an `init` function.

## App directory

```
app/
  app.json                  the manifest
  dist/                     built UI (the manifest's "assets")
  host/host.js              bundled TypeScript host code (IIFE)
  host/host.js.map          its source map
  gohost/krate-gohost       Go host child executable (krate-gohost.exe on Windows)
```

Release layout: the app directory sits next to the shell binary
(`<dir of exe>/app`), or at `../Resources/app` inside a macOS `.app`
(`Contents/MacOS/<exe>`). `krate-shell <appDir>` takes any directory.

## app.json

```json
{
  "appId": "com.acme.app",
  "productName": "Acme",
  "version": "1.0.0",
  "window": {"title": "Acme", "width": 1280, "height": 800, "resizable": true, "path": "/"},
  "assets": "dist",
  "fallbacks": [{"pattern": "/tasks/[id]", "file": "tasks/[id]/index.html"}],
  "hostjs": {"bundle": "host/host.js", "map": "host/host.js.map"},
  "gohost": {
    "binary": "gohost/krate-gohost",
    "funcs": ["src/lib/tasks.List"],
    "streams": ["src/lib/tasks.Watch"],
    "signals": ["src/lib/tasks.Count"]
  },
  "debug": false
}
```

| Key | Meaning |
| --- | --- |
| `appId` | Required. Reverse-DNS id (`[A-Za-z0-9._-]`, starting with a letter or digit). Names the platform data directory. |
| `productName` | Display name. Default `appId`. |
| `version` | Informational. |
| `window.title` | Default `productName`. |
| `window.width`, `window.height` | Default 1024 x 768. |
| `window.resizable` | Default `true`. |
| `window.path` | Route to open, starting with `/`. Default `/`. |
| `assets` | Built UI directory. Default `dist`. |
| `fallbacks` | Optional. Pages that serve URLs with no file; see below. `file` is relative to `assets`. |
| `hostjs.bundle` | The host bundle, an IIFE that calls `__krateHost.register`. Optional section. |
| `hostjs.map` | Its source map. Optional. |
| `gohost.binary` | The Go host child. `.exe` is appended on Windows when missing. Optional section. |
| `gohost.funcs`, `streams`, `signals` | Ids (`<module id>.<export>`) the child registers. The shell forwards exactly these. |
| `debug` | Enables the webview's developer tools even without `--dev`. |

Every path is relative to the app directory and must stay inside it (no
absolute paths, no `..`). Unknown keys are ignored so newer manifests load in
older shells. An id registered by both the host JS and the Go host is an error.

### Fallback patterns

Patterns use Krate's route segment syntax and are matched after the plain file
lookup (`name`, `name/index.html`, `name.html`) fails:

| Segment | Matches |
| --- | --- |
| `static` | exactly that text |
| `[param]` | one non-empty segment |
| `[...rest]` | one or more segments; last segment only |
| `[[...rest]]` | zero or more segments; last segment only |

When several patterns match, the most specific wins: static segments beat
`[param]`, which beats catch-alls, compared left to right. Equal patterns keep
manifest order. Without a match the shell serves `404.html` if present.

## Browser mode

`--browser` (`RunOptions.Browser`) opens no window. It serves the same handler
on `127.0.0.1:<port>` (`--addr`, default `127.0.0.1:0`) and prints
`url http://127.0.0.1:<port>/?krate_token=<token>` on stdout. This is for
Playwright and devtools in dev.

- A random per-session token is required on every request. Visiting any URL
  with `?krate_token=<token>` sets an HttpOnly, SameSite=Strict cookie and
  redirects (302) to the same URL without the token. The cookie, or an
  `X-Krate-Token` header, authorizes everything else; any other request gets
  403.
- The `Host` header must be `127.0.0.1`, `localhost` or `[::1]` on the
  listener's port (this stops DNS rebinding); anything else gets 403.

## Dev control channel

`krate-shell --dev` reads newline-terminated commands on stdin and answers
each with exactly one line on stdout, `ok` or `error <message>` (newlines in a
message become ` | `). stdout carries nothing else except two lines before the
first reply:

```
url <url>      browser mode only
ready          the window is open (or the listener is up)
```

Logs, the Go host's output (prefixed `[gohost] `) and the host JS console go
to stderr.

| Command | Effect |
| --- | --- |
| `reload-hostjs` | Re-reads `app.json` and the host bundle and swaps it into the running VM (`hostjs.Host.Reload`). A bundle that throws leaves the old code running and answers `error`. New ids are registered. |
| `restart-gohost` | Re-reads `app.json`, stops the Go host child (stdin close, kill after 2 s) and starts the binary it names now. Windows stay open; open signals reconnect on their own and the page sees the new child's values. Starts the child if none ran. |
| `eval <js>` | Runs the rest of the line as JavaScript in every open window, without waiting for a result. `error` in browser mode or with no window. |
| `quit` | Answers `ok`, then exits. |

stdin EOF (the driver died) quits the shell. SIGINT and SIGTERM quit it too.
The same operations are available in process as `Control` (`ReloadHostJS`,
`RestartGoHost`, `Eval`, `Quit`, `URL`, `Origin`), passed to
`RunOptions.OnReady`; `ServeControl` implements the text protocol over any
reader and writer.

## Go host child

See `../gohost`. The generated child main is `gohost.Serve(glue.Register)`. The
shell starts it with `KRATE_GOHOST_SOCKET` (a unix socket path under the user
cache directory), `KRATE_APP_DIR` and, in dev, `KRATE_DEV=1`, and forwards the
manifest's ids to it with `scheme.Remote`.

## RunOptions

| Field | Meaning |
| --- | --- |
| `Browser`, `Addr`, `Dev` | As above. |
| `OnReady` | Called on its own goroutine with the `Control` once ready. |
| `ExtraHandler` | `func(w, r) bool`, tried before the app handler (after the token check in browser mode). For tests and embedders. |
| `Logf` | Diagnostics. Default stderr. |
| `Out` | Where the `url` line goes. Default stdout. |
| `Context` | Quits the app when done. |
