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

A release build embeds the same layout as a zip (`shell.RunEmbedded(fsys, opts)`,
`shell.Main(fsys, register)`): the manifest has no `gohost` section and the Go host
code is registered in process (`RunOptions.GoGlue`). See the release page of the docs.

`krate-shell --doctor` does not load an app: it prints one line of JSON (`shell.DoctorReport`) with the
webview name, version and library paths, the minimum version and whether a display is reachable, and
exits 0 even when the webview is missing. `krate native doctor` runs it.

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
  "permissions": ["window", "dialog", "clipboard", "menu", "notify"],
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
| `window.keepAlive` | Default `false`. Keep the app running with no window open (a tray app): closing the last window releases its webview instead of quitting, and `window.Show` (or reopening from a tray click or second instance) brings it back on the same route. See the memory docs page. |
| `window.path` | Route to open, starting with `/`. Default `/`. |
| `assets` | Built UI directory. Default `dist`. |
| `fallbacks` | Optional. Pages that serve URLs with no file; see below. `file` is relative to `assets`. |
| `hostjs.bundle` | The host bundle, an IIFE that calls `__krateHost.register`. Optional section. |
| `hostjs.map` | Its source map. Optional. |
| `gohost.binary` | The Go host child. `.exe` is appended on Windows when missing. Optional section. |
| `gohost.funcs`, `streams`, `signals` | Ids (`<module id>.<export>`) the child registers. The shell forwards exactly these. |
| `permissions` | Optional list of SDK permission names (`sdk/permission`: `os:info`, `kv`, `db`, `fs`, `fs:read`, `os:secrets`, `os:secrets.read`, `shell:openExternal`, `net`, `window`, `app`, `menu`, `dialog`, `notify`, `clipboard`, `clipboard:read`, `tray`, `shortcut`, `updater`). It gates which `krate/*` SDK ids the page may call; host code (TypeScript and Go) is trusted and never gated. Empty or missing means `os:info`, `kv`, `db`, `window`, `app`, `menu`, `dialog`, `notify` and `clipboard`. A non-empty list grants exactly what it names. An unknown name makes the manifest fail to load. Read once at start. |
| `strictPermissions` | With `true`, an empty or missing `permissions` list grants nothing instead of the defaults. A derived build sets it. |
| `window.permissions` | Optional list narrowing the SDK permissions of the main window's page to a subset of `permissions`; `[]` leaves it nothing. Windows opened with `window.Open` take the same option and inherit the opener's scope. |
| `updater` | Optional `{url, publicKey}`: the update feed URL template (`{target}`, `{version}`) and the base64 Ed25519 public key. Without it `krate/updater.*` answers `unsupported`. See `updater/` and the Updates docs page. |
| `memory` | `"balanced"` (default), `"minimal"` or `"performance"`. The memory profile of the webview and host: `performance` keeps the engine defaults, `balanced` and `minimal` apply the levers described in the Memory docs page (webview features off unless used, cache and pressure settings, QuickJS and Go limits, idle trimming). Anything else makes the manifest fail to load. |
| `features` | Optional `{media, webaudio, webgl, webrtc, spellcheck}` booleans: the webview features the UI uses, written by `krate native build` from a scan of the built UI and `native.features`. Under `balanced` and `minimal` a feature that is `false` is switched off in the webview. A manifest without the block keeps every feature on. |
| `hostjs.memoryMB` | Optional QuickJS heap limit in MiB. Default by profile: 64 (`balanced`), 32 (`minimal`), 256 (`performance`). |
| `debug` | Enables the webview's developer tools even without `--dev`. |

Every path is relative to the app directory and must stay inside it (no
absolute paths, no `..`). Unknown keys are ignored so newer manifests load in
older shells. An id registered by both the host JS and the Go host is an error.

### The SDK in the shell

`shell.Run` registers the Native SDK once per app, with the identity and the
permission set of the manifest, before any host code (host code cannot shadow an
SDK id):

- the data areas (`sdk/all.Register`: `os`, `fs`, `db`, `kv`, `net`,
  `permission`) are registered on the app registry at load;
- the UI areas (`sdk.Host`: window, dialog, clipboard, menu, tray, shortcut,
  notify, app) are attached to the platform when the window system starts (or
  with no platform in browser mode, where they answer `unsupported`) and
  registered on the same registry with the same permission set
  (`sdk/all.RegisterUI`).

An id whose permission is missing fails with the structured error
`{code: "permission_denied", data: {permission: "..."}}`. The Go host child
gets `KRATE_APP_DIR`, `KRATE_APP_ID`, `KRATE_APP_NAME`, `KRATE_APP_VERSION`,
`KRATE_DEV=1` (with `--dev`) and `KRATE_GOHOST_PARENT`, the private socket
through which the SDK calls it makes reach the shell's platform.
`RunOptions.Allowed` replaces the manifest's set for the UI areas (tests).

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
  403. The cookie is named per port (`krate_token_<port>`) so two sessions do
  not overwrite each other. A cookie-authenticated request that a browser marks
  cross-origin (`Origin` other than the page's own, or `Sec-Fetch-Site` other
  than `same-origin`/`none`) gets 403: a page served by another localhost port is
  same-site, so SameSite alone would not stop it from posting to `/__krate/call`.
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
| `query <js>` | Evaluates the rest of the line as a JavaScript expression in the open window, awaits a returned promise and answers `ok <json>` with the JSON-encoded value (`ok null` for `undefined`), or `error <message>` if the expression throws, the page unloads before it answers (a navigation), or nothing answers within 45 s. Wrap statements in an async arrow function: `query (async () => { ...; return x })()`. If the page is still loading, the script is sent again until the page runs it. Needs `--dev`; not available in browser mode. Used by the end-to-end tests. |
| `memory` | Samples the memory of the app's processes and answers `ok <json>`, a `memstat.Report`: `os`, `source` (`smaps_rollup`, `phys_footprint` or `private_usage`), `procs` (`pid`, `name`, `role`, and `rss`, `pss`, `privateDirty`, `privateClean`, `anon`, `swap` in bytes), `total` (the sum) and `notes` (what could not be attributed). Roles: `shell`, `gohost`, `hostchild:<runtime>`, `webview:web`, `webview:network`, `webview:gpu`, `webview:other`. Browser mode reports only the shell and its children (no webview roles). See `../memstat` for what each OS can provide. Needs `--dev`. |
| `hmr <json>` | Delivers one dev-hub message (a JSON object with a `type`: `reload`, `build-error`, `client-error`) to every open `/__krate/hmr` stream. `error` without `--dev` or when the message is not a JSON object. |
| `quit` | Answers `ok`, then exits. |

stdin EOF (the driver died) quits the shell. SIGINT and SIGTERM quit it too.
The same operations are available in process as `Control` (`ReloadHostJS`,
`RestartGoHost`, `Eval`, `Query`, `Memory`, `HMR`, `Quit`, `URL`, `Origin`), passed to
`RunOptions.OnReady`; `ServeControl` implements the text protocol over any
reader and writer.

## Dev endpoints

With `--dev` the app origin serves, besides the host protocol:

- `GET /__krate/hmr` is the frame stream of `hmr` messages, one `{"hmr": <message>}`
  frame each, starting with `{"type":"connected"}`. The driver feeds it through the
  `hmr` control command, so no extra port or token is needed and it works in
  both window and browser mode. The latest `build-error` is replayed to a stream
  that opens after it; a `reload` clears it. `ping` messages keep idle streams alive.
- `GET /__krate/dev/events[?since=<seq>&follow=1]` is the host inspector feed:
  one `{"ev": {"seq", "kind", "fn", "id", "window", "time", "durNs", "code",
  "message"}}` frame per recorded event (calls, stream open/end, signal
  subscriptions and updates; the newest 500 are kept). `follow=1` keeps the
  stream open. Arguments and values are never recorded.
- `GET /__krate/dev/memory` is the `memory` report as a JSON document (the same
  `memstat.Report`), so a page, a script or `curl` (with the browser-mode token) can sample
  the running app. Webview processes come from the platform's optional
  `ProcessInfo` capability (`platform/caps`): descendants named `WebKit*Process`
  on Linux, private `WKWebView` selectors on macOS and WebView2's
  `GetProcessInfos` on Windows. Where a process cannot be named, `notes` says so
  and the role is absent. On macOS and Windows, TestGUIMemory and the CI memory report run it.
- `POST /__krate/dev/client-error` takes the dev overlay's error reports (uncaught
  errors, unhandled rejections, `console.error`, failed script loads) and logs
  them to stderr as `[webview] <window or view id>: <message>` followed by the
  stack. The id comes from the webview that sent the request, not from the body.
- `POST /__krate/inspector` opens the web inspector of the requesting view
  (`caps.InspectorOpener`). It exists only when the developer tools are on (`--dev`
  or `debug: true` in the manifest), where the shell also injects the script that
  posts to it on F12, Ctrl+Shift+I or Cmd+Option+I.

## Go host child

See `../gohost`. The generated child main is `gohost.Serve(glue.Register)`. The
shell starts it with `KRATE_GOHOST_SOCKET` (a unix socket path under the user
cache directory), `KRATE_APP_DIR` and, in dev, `KRATE_DEV=1`, and forwards the
manifest's ids to it with `scheme.Remote`.

## JavaScript host children

An app.json `hostchildren` entry (see `docs/native/app-json`) runs TypeScript
host code on Node, Bun or Deno (or a compiled scriptc executable) as a child
process, the same way the Go host child runs. The shell starts it with
`command` (`{entry}` is the script, `{sockdir}` the socket directory), sets
`KRATE_GOHOST_SOCKET` and the other `KRATE_*` variables, accepts the `ready`
line anywhere on stdout (host code may log while it loads), asks the child for
the ids it registered (`GET /__krate/hostchild/list`) and forwards the union of
those and the manifest's lists with `scheme.Remote`. `reload-hostjs` restarts
the children. See `../hostchild` for the child runtime.

## RunOptions

| Field | Meaning |
| --- | --- |
| `Browser`, `Addr`, `Dev` | As above. |
| `OnReady` | Called on its own goroutine with the `Control` once ready. |
| `ExtraHandler` | `func(w, r) bool`, tried before the app handler (after the token check in browser mode). For tests and embedders. |
| `Logf` | Diagnostics. Default stderr. |
| `Out` | Where the `url` line goes. Default stdout. |
| `Context` | Quits the app when done. |
