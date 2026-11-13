# Krate Native security review (round 5)

Scope: the permission and isolation model of `packages/native` as of branch
`native` at eecdafa0 (shell, scheme, sdk and its areas, platform layers, gohost,
hostchild, internal/hosttransport, the updater) and the compiler's permission
derivation (`packages/compiler/internal/native/capability`, config validation),
read against the guarantees in the docs (`native/release.md` per-window scopes,
`views.md`, `remote.md`, `media.md`, `processes.md`, `files.md`).

The attacker models used:

- **A page**: script running in an app view or window, possibly one with a narrow
  scope (`permissions: [...]`), that wants more of the SDK than its scope gives.
- **Remote content**: a web page in a remote view or window.
- **A local process of the same user** that can start the app's executable (the
  macOS case matters: code signature, TCC consent and keychain ACLs are tied to
  the app, not to the user).
- **Another local user**, and **the network** (update feed, remote pages).

Severities: critical (remote or page code execution outside the webview without
user action), high (a page defeats the scope model or runs native code), medium
(cross-scope data leak, or impersonation of the app by a local process),
low (needs unusual configuration or a same-user attacker for little gain; or
defense in depth).

## Findings

### 1. High: a view can squat the next window id and lose its scope

- **Where**: `sdk/internal/bridge/env.go`, `Windows.Reserve` and `Windows.add`
  (window ids `w<N>` were handed out without checking view ids);
  `sdk/window/views_ops.go` `IDAddView` (a page may name its view).
- **Exploit**: a page with a narrow scope (say `["window"]`) learns the next
  window number (open and close a probe window), adds a view to its own window
  with `id: "w<N+1>"`, then calls `window.open`. The new window got the same id
  `w<N+1>`: its primary view could not register (duplicate) and both shared one
  permission-scope key. Closing that window ran `Unscope("w<N+1>")`, which
  removed the scope the view relied on. An id without a scope has the app's full
  permission set (`permission.Set.AllowsFor`), so the view the page controls
  could now call every SDK id the app grants (`fs`, `os:secrets.read`, ...).
  Reproduced through the real shell: after the close, the view's four `kv.get`
  calls all returned `ok` where `permission_denied` was expected.
- **Fix**: `nextWindowLocked` skips ids held by a view, a window or a claim.
- **Tests**: `sdk/all/scope_test.go` `TestViewCannotSquatTheNextWindowID`;
  GUI `shell/gui_scope_test.go` `TestGUIViewCannotSquatAWindowID` (page
  `dist/squat/` in `shell/testapp_test.go`). Both fail before the fix.

### 2. High: concurrent `addView` with one id strips the winner's scope

- **Where**: `sdk/window/views_ops.go` `IDAddView` and `sdk/remote/ops.go`
  (`openWindow`, `addView`): `IDTaken(id)` was checked, then `Scope(id)`, then
  `AddView(id)`; the loser of the registration called `Unscope(id)`.
- **Exploit**: the call endpoint runs the calls of one batch concurrently. A
  scoped page sends two `krate/window.addView` calls with the same `id` in one
  request. Both pass `IDTaken`, both set the scope, one registers, the other
  fails and calls `Unscope(id)`, which deletes the scope of the view that did
  register. That view is now unscoped: the app's full permission set. The test
  reproduces it on the first iteration.
- **Fix**: `Windows.Claim` checks and reserves an id in one step; `Reserve` and
  `ReserveView` claim too; `IDTaken` counts claims; registration clears the claim
  and failure paths `Release` it (and unscope). Explicit ids in `addView`,
  `remote.openWindow` and `remote.addView` use `Claim`.
- **Test**: `sdk/all/scope_test.go` `TestConcurrentViewIDsKeepTheirScope`
  (50 rounds; fails before the fix, passes with `-race`).

### 3. High: page-supplied worker environment runs arbitrary code

- **Where**: `sdk/worker/ops.go` `IDStart` passes `opts.Env` from the caller to
  `Supervisor.Start`, and `shell/workers.go` appends it after the shell's own
  variables (in Go's `exec`, the last duplicate wins).
- **Exploit**: a page with only the `worker` permission calls
  `startWorker("indexer", { env: { NODE_OPTIONS: "--import=data:text/javascript,..." } })`.
  Node honors `--import` with a `data:` URL in `NODE_OPTIONS`, so the worker runs
  attacker JavaScript with the user's full privileges: no file needed. With a
  file, `LD_PRELOAD`/`DYLD_INSERT_LIBRARIES` do the same for Go and `exec`
  workers. `KRATE_GOHOST_PARENT` could also point the worker at another endpoint.
  A name like `A=B` smuggled a second variable.
- **Fix**: `worker.ValidateEnv` (called by `Supervisor.Start` for every caller)
  refuses non-identifier names, NUL bytes, `KRATE_*`, `NODE_*` except `NODE_ENV`,
  `BUN_*`, `DENO_*`, `LD_*`, `DYLD_*`, `PATH`, `PATHEXT`, `COMSPEC`, `GODEBUG`,
  `SSL_CERT_FILE`/`DIR` and the usual interpreter hooks (`PYTHON*`, `PERL5*`,
  `RUBY*`, `JAVA_TOOL_OPTIONS`, `BASH_ENV`, ...), case-insensitively. Runtime
  flags belong in the declared `command`. Docs: `native/processes.md`.
- **Test**: `sdk/worker/supervisor_test.go` `TestStartRefusesLoaderAndShellEnv`.
- **Residual**: an `exec` worker may have its own environment hooks no list can
  know. Page-supplied `args` are appended after the entry (script arguments for
  Node and Bun; program arguments for `go`/`exec`, which the app defines).

### 4. Medium: another view could open a stream it did not start

- **Where**: `scheme/stream.go` `handleStream` matched the stream id only.
- **Exploit**: stream ids are a counter (`s1`, `s2`, ...). The producer runs with
  the permissions of the view that made the call. A view with an empty scope
  polls `GET /__krate/stream/s<N>` and takes the stream before its owner opens
  it: it reads `window.events`, `remote.requests`, `media.requests`,
  `port.listen` messages, `worker.events` or plugin streams it could never start,
  and the owner gets a 404. Reproduced through the real shell.
- **Fix**: a stream records whether its window is a native identity; one started
  from a native view opens only from the same native view. Without a native
  identity (browser mode, the private sockets) behavior is unchanged.
- **Tests**: `scheme/identity_test.go` `TestStreamCannotBeOpenedByAnotherView`;
  GUI `shell/gui_stream_test.go` `TestGUIStreamCannotBeTakenByAnotherWindow`
  (page `dist/hijack/`). Both fail before the fix.

### 5. Medium: plugin signal gates trusted the page's header

- **Where**: `scheme/signals.go` `handleSignals` called the gate with
  `r.Header.Get("x-krate-window")` instead of the native identity.
- **Exploit**: a narrowly scoped view subscribes to a permission-gated plugin
  signal with `x-krate-window: main` (or no header: the empty window has no
  scope, so `AllowsFor("")` is true) and reads it.
- **Fix**: the gate gets `s.windowOf(r)`, like calls.
- **Test**: `scheme/identity_test.go` `TestSignalGateSeesTheNativeIdentity`.

### 6. Medium: the prebuilt-shell distributable took `--dev` and any app directory

- **Where**: `shell/cli.go` `CLI`. `krate native build` (and the `--release`
  fallback) ships `krate-shell` under the product name with `app/` next to it;
  this binary accepted `--dev` and an app directory as its first argument. The
  compiled release executable only takes `--dev` with the `krate_devcontrol`
  build tag, so the two release forms disagreed.
- **Exploit**: a same-user process starts `MyApp.app/Contents/MacOS/MyApp --dev`
  with a pipe on stdin: the dev control channel runs script in the app's pages
  (`eval`, `query`), fakes file drops (`drop`, which grants `fs` access), opens
  developer tools and honors `KRATE_GOHOST_DEBUG` (a Delve server for the Go host). Or it runs
  `MyApp /tmp/evil` with its own `app.json`, host code and Go host binary. Both
  act under the app's code signature: macOS keychain items readable without a
  prompt, the camera and microphone consent of the app (children inherit TCC
  responsibility).
- **Fix**: `checkLaunch`: an executable with an app directory next to it refuses
  another app directory, and refuses `--dev` unless the app was built with
  `native.debug`. A bare `krate-shell` (dev, `krate native test`, the memory
  tool) is unchanged. Docs: `native/packaging.md`.
- **Tests**: `shell/cli_test.go` `TestDistributedShellRefusesDevAndForeignApps`;
  the docs-site e2e (`packages/compiler/cmd/krate/native_docs_e2e_test.go`) now
  checks that the packaged and installed executables refuse `--dev` and drives
  the window check through the bare shell with the packaged app directory.
  `cmd/krate-shell/main_test.go` no longer puts its test app next to the binary.

### 7. Low: single-instance socket could fall back to shared `/tmp` (fixed)

- **Where**: `sdk/app/instance.go` `socketPath`.
- **Issue**: when `$XDG_RUNTIME_DIR` (or the cache path) is too long for a socket
  path, the lock socket went to `os.TempDir()`. On Linux that is shared: another
  user could create `/tmp/krate-<uid>-<hash>.sock` first, receive every later
  launch's arguments (file paths) and make each launch exit as a "second
  instance".
- **Fix**: a private `~/.krate-run` (0700) is tried first; `/tmp` stays the last
  resort. Test: `sdk/app/app_test.go` `TestLongPathAvoidsTheSharedTempDir`.

### 8. Low: `--browser` in release executables exposes the SDK to whoever starts them (not fixed)

- **Where**: `shell/embedded.go` `RunMainPlugins` and `shell/cli.go`.
- **Issue**: a same-user process can start a release executable with `--browser`,
  read the session token from stdout and call the SDK ids the app's permissions
  grant, as the app. Unlike finding 6 this runs no new code in the app and no
  page in the app's webview (no camera or microphone consent); the residual is
  data behind the app's identity, chiefly `os:secrets.read` keychain items on
  macOS.
- **Why not fixed**: `--browser` is a documented and tested feature of release
  executables (`native/release.md`, `krate native profile --browser`, the e2e
  tests). Recommendation: a `native.release.browser: false` switch (default off
  for apps that use `os:secrets`).

### 9. Low: automation endpoints are reachable by any local web content (not fixed)

- **Where**: `platform/linux/automation.go` (`WEBKIT_INSPECTOR_HTTP_SERVER`),
  `platform/windows/automation_windows.go` (`--remote-debugging-port`).
- **Issue**: only with `krate native dev` or `native.debug` plus
  `native.automation` (`shell.go` `runNative`; a page cannot turn it on). Then a
  loopback port controls the app's pages. Remote views and the user's browser can
  send subresource requests and WebSockets to loopback (the remote allowlist
  governs navigations only). Chromium's CDP checks the WebSocket `Origin`; the
  WebKit inspector server is not documented to.
- **Recommendation**: never ship `native.debug` with `native.automation`; the docs
  already call it dev-only.

### 10. Low: worker permission narrowing is not a sandbox (documented)

- A worker is a process of the user. It can reach the shell's trusted private
  socket (`<cache>/krate/native/<pid>/sdk.sock`, mode 0700 directory, same uid),
  which serves the ungated SDK. Narrowing protects against a worker's own code
  using more of the SDK than it declares, not against a compromised worker.
  `native/processes.md` now says so.

### 11. Low: smaller notes (not fixed)

- **`fs` grants are app-wide**, not per window: a file granted from a dialog,
  drop or open in one window is readable by every window with `fs:read`. The
  scope keeps the check per call (`fs` permission), not per grant.
- **Windows drops**: the drop shim posts `File` objects with
  `postMessageWithAdditionalObjects`; a page can post a `File` from an
  `<input type=file>` the same way, which the user picked, so it grants nothing
  the user did not choose.
- **Window chrome endpoint** (`/__krate/chrome`): any view, even with an empty
  scope, may start a drag, resize or maximize of its own window. By design (drag
  regions are HTML) and limited to its own window.
- **Remote message origin on Linux** is the view's URL when the message is
  handled, so a message sent just before a navigation commits can carry the next
  origin (documented in `remote.md`).
- **Packaging**: Flatpak always gets `--socket=pulseaudio` (which also allows
  recording), the snap always gets `home`, and MAS gets
  `files.user-selected.read-write` by default. Narrowing them to the derived
  permissions is a follow-up.
- **Signing secrets** (`KRATE_NOTARY_PASSWORD`, `KRATE_WINDOWS_PFX_PASSWORD`)
  reach `notarytool`/`signtool`/`osslsigncode` on argv, visible to other local
  users through the process list while signing runs; logs redact them
  (`internal/native/pkg/pkg.go`). Prefer a notarytool keychain profile and a
  certificate store thumbprint on shared machines.
- **Worker SDK socket directory** falls back to `os.TempDir()/wsdk` when the user
  has no cache directory (`shell/workers.go`); another user who created that
  directory first could replace the socket. Needs `$HOME` and
  `$XDG_CACHE_HOME` unset.

## Checked and sound

- **Native identity of requests**: every platform tags scheme requests with the
  originating webview (Linux `webkit_uri_scheme_request_get_web_view`, macOS
  `startURLSchemeTask`, Windows `WebResourceRequested` and the WebView2 message
  stream shim), with view 0 for an unknown one; `shell.identify` maps unknown and
  remote views to the empty scope `?unknown-view`. Calls and cancels used it
  already; streams and signal gates now do too.
- **Scope arithmetic** (`sdk/permission`): explicit lists must be a subset of the
  caller's scope (`ScopeChild`), inheritance copies the caller's scope, remote
  windows and views get `[]`, worker scopes are checked against the app's set,
  view ids cannot contain `:` or `?` so they cannot collide with `worker:<id>` or
  `?unknown-view`.
- **Dev-only endpoints**: `/__krate/dev/*` (query results), the dev tools,
  `/__krate/hmr` and the memory panel need `RunOptions.Dev`; `/__krate/inspector`
  needs Dev or `native.debug`; the compiled release takes `--dev` only with the
  `krate_devcontrol` tag; the dev control channel (`drop`, `eval`, `emit`) exists
  only with `--dev`. `KRATE_GOHOST_DEBUG` (Delve) is honored only in dev.
- **App-origin lock (Linux)**: app views allow navigations, including frames, only
  to `krate://app`, `blob:` of the app and `about:blank`; new windows are refused.
  So foreign content cannot be framed in an app view and post blind requests to
  `/__krate/call`.
- **Remote content**: separate `WebKitWebContext` per partition without the app
  scheme (`newRemoteWidget`), a second check in `darwin/scheme_darwin.go`
  `startTask`, the policy (`caps.RemotePolicy.Decide`: http/https only, no
  credentials in URLs, `app.localhost` and subdomains refused as URL, allowed
  origin and target, wildcard needs a registrable suffix and matches subdomains
  only, `mailto`/`tel` only to the system handler), popups load in place, `create`
  returns no webview, `NavigateRemote` re-applies the policy, permissions denied
  and downloads cancelled per partition. The message channel: random 128-bit
  token in a closure of the top-frame script, constant-time compare, JSON and
  size checks, origin taken from the engine, `targetOrigin` re-checked inside the
  page by `DeliverToRemoteScript` (closes the race with a navigation), JS string
  escaping covers `</script>`, U+2028 and U+2029. Dev evals skip remote views.
- **Message ports**: only the holder may post, listen or close; `transfer`
  requires `port:receive` of the target (worker or view scope); host code is the
  `""` owner, which a native page cannot be.
- **Loopback TCP transport**: 128-bit token, `subtle.ConstantTimeCompare`,
  passed in the environment (`KRATE_GOHOST_TOKEN`, the parent address), not
  argv or logs; the listener is `127.0.0.1` only and a cross-origin browser
  request cannot add the custom header without a preflight the guard answers 401.
  Unix socket directories are created 0700. Browser mode: 192-bit token,
  host-header check against DNS rebinding, `Origin`/`Sec-Fetch-Site` check for
  cookie-authenticated requests.
- **File grants**: paths are made absolute, resolved through symlinks before the
  root check (grant and use), `..` cannot escape a root, a granted file covers
  only itself; the scope documents its TOCTOU limit against local processes.
  Grants come only from user actions (dialogs, drops, launches and second
  instances over a 0600 socket) and, in dev, the control channel.
- **Automation and extensions**: `automation.Configure` is called only by the
  shell with Dev or `native.debug`; ids answer unsupported otherwise. Extensions
  load by declared name; a page cannot pass a path.
- **Updater**: Ed25519 over a domain-separated message binding app id, target,
  version, SHA-512 and size (`krate-update-v1`), patches with their own prefix
  binding from and to versions (`krate-patch-v1`), every patched file and base
  hash verified, fallback to the full download; newer-version check before
  download and in `Check`; HTTPS only unless configured insecure; patch entry
  paths are clean and relative; archive extraction refuses links out of the
  tree; channel names are validated and changing them still needs signed
  artifacts with a higher version, so a page with `updater` cannot downgrade.
  The rollback state file is the user's own; tampering with it needs a same-user
  attacker who could replace the app anyway.
- **Media**: requests from a view whose scope lacks `media` or `location` are
  denied before the policy; remembered decisions are per app (saved) or per view
  id (in memory, dropped when the view's window closes); remote views deny in
  their own handler; the prompt text comes from the requested kinds, not from
  the page. A per-view decision of a removed side view stays in memory until its
  window closes and applies to a later view given the same id by the app, which
  still needs `media` in its scope (low, not fixed).
- **Compiler derivation**: permissions come from the SDK ids the code reaches;
  an unknown or partial use takes the whole area (more permissions, never fewer
  checks at run time); the implied-permission table matches the runtime's.
- **MAS entitlements** do not include `disable-library-validation` or
  `allow-dyld-environment-variables`; JIT entitlements are added only for a
  bundled JavaScript host runtime.

## Not verified here

macOS and Windows code was read and compiled (`go vet` and `golangci-lint` for
`GOOS=darwin` and `GOOS=windows`), not run. The fixes in this review are in
platform-independent code (`scheme`, `sdk/internal/bridge`, `sdk/window`,
`sdk/remote`, `sdk/worker`, `sdk/app`, `shell`), so they apply on every OS; the
GUI tests that prove them run on Linux (WebKitGTK).
