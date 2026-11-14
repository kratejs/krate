# Changelog

Krate uses [GitHub Releases](https://github.com/kratejs/krate/releases) for the
detailed changelog. This file records the release train at a glance: each release
lists what breaks first, then what is new, fixed and security-relevant. Upgrading
from 0.4? Read [Upgrading](https://krate.js.org/docs/guides/upgrading/#from-04-to-05).

## Unreleased

### Krate Native

- Migration guides: Migrating from Electron and Migrating from Tauri (docs native/from-electron and
  native/from-tauri) with concept maps, full API tables, security, packaging, update, testing and
  debugging comparisons, and an honest list of what has no equivalent. `examples/native-from-electron`
  holds a small Electron app (`electron/`, pinned versions) and its port to Krate Native, with a
  `krate native test` suite that runs through the real shell. A real find while
  porting: a host signal reads `undefined` in the page until the host pushes its first value, so a page
  that reads `settings().theme` throws; the guide shows the guard.
- Accessibility of the shell's own UI, checked through AT-SPI (Linux, GUI tests), NSAccessibility and AX
  (macOS) and UI Automation (Windows). Fixed: offscreen windows are hidden from assistive technology
  (Linux, macOS); message dialog titles reach the accessible name (Linux); split-pane dividers and panes
  are named (Linux); tray, notification action and thumbnail button names are never empty; macOS menu bar
  items carry their title, the status button has a label and alerts honor `default`; Windows message boxes
  name which button means what, honor `default` and fall back to the app name as title; drag regions no
  longer turn presses on ARIA widgets and focusable elements into window moves. See docs/native/accessibility
  and packages/native/ACCESSIBILITY.md.
- Benchmarks of Krate Native against Electron and Tauri v2 on the same small app (cold start to first
  contentful paint, idle memory over the whole process tree, installer and on-disk size):
  `packages/benchmark/native/bench.mjs`, the informational `Native benchmark` workflow on Linux, macOS
  and Windows, `packages/native/cmd/krate-memtree`, and docs/native/benchmarks with Linux numbers.
- `krate native test` runs a project's `*.native.test.ts` files against the real shell with Node's
  test runner. The new `@krate/app/driver` module launches the app and drives pages (`evaluate`,
  `click`, `fill`, `press`, `text`, `waitFor`, `errors`, host `call`), several windows and views, and
  injected tray, menu and shortcut events. The shell's dev control channel gained `views`,
  `query-in` and `emit`. See docs/native/testing.
- Updater: delta updates, staged rollouts, channels and rollback. `krate native package --delta-from <dir>`
  writes signed pure-Go bsdiff patches from the newest earlier releases (install tree, macOS bundle,
  AppImage; unchanged and renamed files cost nothing) and lists them under `patches` in the feed; the
  client verifies the patch signature, every installed base hash and every written file, and falls back
  to the full download on any failure (1.0.0 to 1.1.0 of `examples/native-tasks`: 16 KB to 115 KB for a
  6.6 MB update file). Feeds take `rollout` (percentage, start, ramp), `critical` and `min_version`,
  decided by a stable bucket from a per-install random id; `krate native rollout` changes or halts them
  after release without re-signing. `{channel}` in the feed URL, `native.updater.channel`,
  `updater.setChannel()` and `package --channel`. A freshly updated version must confirm a healthy start
  (the shell does after the first page, or `updater.markHealthy()` with `confirm: "app"`); after a crash
  or timeout the next launch restores the previous version, reports it through `updater.status()` and
  does not offer that version again. New ids `krate/updater.markHealthy`, `.status`, `.ackRollback`,
  `.setChannel`; new `native.updater` keys `channel`, `maxStarts`, `healthTimeout`, `confirm`;
  `krate native patch` writes a patch for files the packager does not feed. All new feed fields are
  ignored by older clients, and old feeds work unchanged. A configured `native.updater` now links the
  updater area even when no code calls it.
- Rich clipboard formats: `clipboard.write({ text, html, rtf })` puts several representations on
  the clipboard together (Electron's `clipboard.write({ text, html })`); `readHtml`, `readRtf`,
  `formats`, `readFormat` and `writeFormat` read and write by MIME name on all three platforms
  (GTK targets, `public.html`/`public.rtf`, Windows `HTML Format` with the `CF_HTML` header and
  `Rich Text Format`).
- Webview context menu: `native.window.contextMenu` (`default`, `none`, `custom`) and
  `menu.setContextMenu` per app, window or view. `custom` delivers a context event (kind, link,
  image and selection, spelling suggestions) that `menu.onContextMenu` answers with items drawn by
  the native menu code; `extend` adds items to the engine's menu. `krate:` item ids run the
  built-in editing, spelling and Inspect Element actions.
- `krate native dev --debug-go` builds the Go host with `-N -l` and runs it under a headless Delve
  (`--debug-addr`, `--debug-config vscode`); `--debug-host` starts node, bun and deno host children
  with `--inspect`. A missing `dlv` is diagnosed with its install command.
- The `@krate/core-<platform>` packages ship `krate-shell` next to `krate`, cross-compiled with
  `CGO_ENABLED=0` for all seven platforms. The compiler finds the shell in this order: `--shell`,
  `$KRATE_SHELL`, next to the running `krate` executable, a build from a Krate checkout.
- `krate native build` writes a runnable `dist-native/<os>-<arch>/<productName>` with `app/` next
  to it when a shell is available, so a TypeScript-only app needs no Go toolchain to build and
  package. `krate native build --release` falls back to the same layout without Go.
- Window chrome: `native.window.titleBar` hidden and none now work on every OS, with drag regions
  (`data-krate-drag`, `-webkit-app-region`, `app-region`), a native move and resize, double-press
  maximize, `transparent`, Windows 11 `backdrop` (Mica, Mica Alt, Acrylic), `titleBarOverlay`, macOS
  `vibrancy` and `trafficLightPosition`, document state, sheets, native tabs, the share menu
  (new `share` permission), `bundle.macos.agent` (`LSUIElement`) with run-time dock control, Windows
  thumbnail toolbar buttons and jump list tasks, through `@krate/app/chrome` and `sdk/chrome`. Linux
  is tested; macOS and Windows are untested until the CI native job passes. See the Window chrome
  page and `examples/native-chrome`.
- `krate native doctor` reports the Go toolchain, the shell, the system webview (through the new
  `krate-shell --doctor`), packaging and signing tools and the host runtime, and exits 1 when
  something the config needs is missing.
- Native plugins: a Go package plus a `krate-plugin.json` that ships reusable native capabilities.
  `native.plugins` enables local directories, npm packages and Go modules; ops are called as
  `plugin/<name>.<op>` (lowerCamel op names like the SDK ids, `info` or `cpuLoad`, bound to the exported Go
  function by capitalizing the first letter or by the manifest's `go` field) through a generated, typed `krate/plugin/<name>` module (calls, streams,
  signals) and gated by `plugin:<name>.<permission>`, which a build derives from the code, scopes
  per window and shakes per op in release builds. The app runs under a per-app shell built with Go
  (`krate native doctor` reports the toolchain as required). `krate native plugin init <name>`
  scaffolds one; `examples/native-plugin` is a complete app. New diagnostics `KR7250` to `KR7257`.
- Files: `native.fileAssociations` becomes macOS document types (with exported UTIs where macOS has
  none), MSIX file type associations, NSIS ProgID and registry entries (removed on uninstall) and a
  Linux `MimeType` with a shared-mime-info file in the deb, rpm and AppImage. `onOpenFile` and
  `openedFiles` (`@krate/app/app`, Go `app.OnOpenFile`) deliver the files an app is launched with,
  handed over by a second launch (apps with file associations take the single-instance lock
  automatically) or sent by macOS, and grant them to the `fs` scope like a dialog pick;
  `onFileDrop` (`@krate/app/window`, Go `window.OnFileDrop`) reports the real paths of files
  dropped on a view while the page keeps its HTML5 drop events. `krate-shell` takes files after the
  app directory. See `examples/native-files` and docs native/files.
- Camera, microphone, screen capture and location with app-controlled permissions: the page's
  `getUserMedia`, `getDisplayMedia` and geolocation requests go to the SDK (`@krate/app/media`,
  Go `sdk/media`) instead of the engine's policy. A `native.media` policy per kind (`prompt`,
  `allow`, `deny`), remembered decisions per app and per view, a request handler
  (`onRequest`) and a native prompt decide; a window or view scope without the new `media` or
  `location` permission is denied, and the build derives both from the UI code. An allowed request
  also goes through the operating system's consent (macOS TCC, the Windows consent store, the Linux
  Camera portal). `native.media.usage` writes the macOS usage descriptions, packaging adds the
  device entitlements and MSIX capabilities, and `KR7290` warns when a description is missing.
  `media.sources` lists screens and windows on macOS and Windows; no engine accepts a chosen source.
- Native web contents controls per view: the print dialog and PDF output (`@krate/app/page`
  `print`, `printToPdf`), find in page with match counts, zoom (Ctrl or Cmd plus, minus and 0 keep
  working), a PNG capture of a view, downloads the page starts (`@krate/app/downloads`: a save
  dialog or a configured folder, progress, completion and cancel events) and cookies and site data
  (`@krate/app/cookies`). New permissions `page`, `downloads` and `cookies`; see [Web
  contents](https://krate.js.org/docs/native/webcontents/). WebKit keeps no cookies for the app's
  custom scheme, so the cookie calls take an http(s) URL on Linux and macOS.
- Electron's advanced process features, where the system webviews allow them (docs native/processes):
  worker processes (`native.workers`, `@krate/app/worker`, Go `sdk/worker`: a TypeScript module on Node, Bun
  or Deno, a Go program or an executable, started by name, with per-worker narrowed SDK permissions, logs in
  the app log, a restart policy and crash events, and two-way messages), message ports between views, windows
  and workers (`@krate/app/port`: a channel's ends are handed to scopes that hold `port:receive`; the engines
  cannot share a real `MessagePort`, so the port is shell-backed), offscreen rendering (`@krate/app/offscreen`:
  a never-shown window plus PNG or BGRA snapshots from WebKitGTK, WKWebView and WebView2 at up to 60 a second
  with dropped-frame counts; about 230 snapshots a second of an animated 800x600 page on the Linux sysroot),
  automation (`native.automation`, dev and `native.debug` only: CDP on WebView2 with in-process commands, the
  WebKit remote inspector on WebKitGTK, Safari inspection on WKWebView) and browser extensions
  (`native.extensions`: WebView2 and macOS 15.4+, unsupported on WebKitGTK). New permissions `worker`, `port`,
  `port:receive`, `offscreen`, `automation`, `extension`. See `examples/native-procs`.
- Remote content: web pages in views and windows that cannot reach the app (`@krate/app/remote`, Go
  `sdk/remote`, new permissions `remote`, `remote:intercept` and `remote:network`, derived from the code
  like the other SDK ids). A remote view has its own web context, configuration or profile with no handler
  for the app scheme, no bridge and an empty permission scope; it may navigate only to the origin it
  started on and an allowlist (everything else opens in the system browser or is dropped, popups load in
  place); permissions are denied and downloads cancelled. Storage is a partition: the default remote one
  (never the app's), `persist:name` or an in-memory `name`. The app talks to a page only over an opt-in
  message channel with origin checks in both directions. Declarative request rules per partition (block,
  redirect, headers, with URL patterns and a request stream) with capability reporting, a user agent and
  proxy per partition, certificate errors that end the load unless the app accepts the host, HTTP logins
  answered by the app, and client certificates from PEM files on Linux. WebKitGTK is tested; WKWebView and
  WebView2 compile and have GUI tests that CI runs. Windows has no per-partition proxy; subresource
  redirects and headers are not possible on WebKit. See docs/native/remote and `examples/native-remote`.
  A real find on the way: `@krate/app` modules that import another module and export a function under a
  name the other declares (`onEvent`) were linked to the wrong function, so the remote module exports
  `onRemoteEvent`.
- Krate Native rows in the compatibility matrix (Linux verified, macOS and Windows untested) and
  size, cold-start and memory budgets in `packages/native/budgets.json`.
- Native OS integration SDK areas, each a Go package and a `@krate/app/<area>` module: `badge`
  (app icon badge, progress and urgent flag: NSDockTile, ITaskbarList3, Unity LauncherEntry),
  `screen` (displays, a window's display, centering, display changes), `power` (battery, idle,
  suspend, resume, lock, sleep blockers), `login` (launch at login), `theme` (system light, dark and
  high contrast, with native title bars following it) and `crash` (JSON crash reports for Go panics
  and webview process crashes under the app's log directory, a crash event, and automatic view
  reload). New permissions `badge`, `screen`, `power`, `theme` (granted by default) and
  `power:inhibit`, `login`, `crash`. Linux is verified against private D-Bus buses, a GTK window and
  the real shell; macOS and Windows are written and compiled, with GUI tests that CI runs.
- `krate native check` and native builds warn about oversized images, large inlined `data:` URLs,
  web fonts, compositing CSS effects and long lists without windowing (KR7270 to KR7274; thresholds
  and a silence list in `native.lean`). Native builds no longer preload lazy chunks, ship no
  script or config block on pages without client code, and write no `.br`/`.gz` siblings.
  `krate native dev` has a Memory panel (PSS, private dirty, DOM nodes, JS heap where available).
- Windows: clicking a toast after the app has quit starts it again. Toasts use protocol activation
  with a per-app URL scheme (registered under `HKCU` at run time, by the NSIS installer and in the
  MSIX manifest, now with `uap3:Protocol` so the URL reaches the executable); a new process hands the
  click to the running instance or, when none runs, delivers it to the page as the first
  notification event. The tray icon is verified to come back on `TaskbarCreated`.
- Windows: the NSIS installer's WebView2 check follows Microsoft's registry locations, and
  `native.bundle.windows.webview2` (`download`, `embed`, `fixed`, `none`, with `webview2Path`)
  chooses how a missing runtime is handled. A shell that cannot find the runtime shows a message box
  with the download link instead of exiting silently.
- Deno host children: recent Deno versions need `unix:<socket path>` in `--allow-net` to listen on
  the child's socket; the dev command now carries `{socket}`, which the shell fills in per run
  (`deno compile` gets plain `--allow-net` for unix targets). A new end-to-end test runs Node, Bun and
  Deno children through the real shell.
- CI: the `native` job also runs on `ubuntu-24.04-arm` and `windows-11-arm`; tools with no arm64
  build are skipped with a notice (`optional_runtimes`, `optional_tools`) and never fail
  `KRATE_REQUIRE_E2E` falsely. Packaging smoke steps sign with throwaway keys (gpg, a self-signed
  macOS identity in a temporary keychain, a self-signed Authenticode certificate) and verify them.
- Fixed: `krate native sign` on Linux signed only the `.deb`; the `.rpm`, `.tar.gz` and AppImage names
  were never matched. Found by signing the real packages with gpg in the new CI smoke step.
- Native budgets accept entries for any platform (`darwin-arm64`, `windows-amd64`, ...) with the
  same measured-plus-headroom rule and fail when a recorded entry is exceeded; platforms without
  entries print a paste-ready `BUDGET-RECORD` block.
- A top-level `viewTransitions` no longer reaches the desktop app (it hung the docs site's window on
  WebKitGTK); `native.viewTransitions: "auto"` opts in.
- Linux: request body streams are only touched on the main thread. WebKit shares their FormData
  with the UI-side request and it is not thread safe; the old code crashed the process (SIGSEGV in
  `gtk_main`) about once in a few hundred requests under load.

### Breaking

- `outDir` values that are or contain the project root, pages directory or public directory
  (such as `".."`) fail with `KR8350` before anything is deleted. `krate build` and
  `krate clean` share the check; with `outDir: "."`, `krate clean` used to delete the project.
- CSS scope handles read by browser code or passed to an npm island fail with `KR8330`, and a
  CSS getter or setter read by a build-time component fails with `KR8331`. Both used to crash
  at runtime.
- `krate doctor` exits 1 when the config does not load or the cache is unwritable.
- `process.env` in API routes, middleware and build-time evaluators is now the shell plus
  `.env` files with the shell winning, on every runtime. Code that relied on a shell variable
  being absent will see it.
- Shadcn components built on `radix-ui` need Preact 11; current shadcn passes `ref` as a prop.
- Under a base path, `<Link>`, `navigate()`, `krateRouter` and `redirect()` add the prefix
  themselves, including when the path already starts with it. Remove a prefix you wrote by hand,
  and stop passing `location.pathname` to them.
- Under a base path, config `redirects` match and send paths under it, and a root-relative
  `Location` from an API route, middleware, `redirect()` or a server action gets the prefix.
  Remove a prefix you wrote into a destination or `Location` by hand.
- A path in `site.url` now sets the base path. To keep a site at the root behind a proxy that
  strips the prefix, set `basePath: "/"`.
- Build-time expression folding (the JS-engine fallback and `getCollection()` chains) runs in a
  QuickJS VM with only the ECMAScript built-ins, `structuredClone` and `console`: no `fetch`,
  `crypto`, timers, `process` or `require`. There `localeCompare`, `toLocale*String` and
  `Date.prototype.toString` (`String(date)`, template literals) throw, so an expression or a
  collection sorted with them stays a run-time value instead of being folded with the build
  machine's locale and time zone. An expression that reads `globalThis` stays a run-time value
  too.
- `krate migrate nextjs --apply` moves the original `app/` (or `src/app/`), a migrated Next
  `pages/` tree and a Next 16 `proxy.ts` into the backup, so Krate does not build them next to
  the migrated copies; `krate migrate rollback` moves them back. Project modules that routes
  import (through relative paths or tsconfig `paths` such as `@/`) are rewritten in place, with
  backups, so `--apply` needs `--force` when any are planned.
- `krate migrate nextjs` no longer writes `NEXT_PUBLIC_*` values from `.env*` into source. It
  renames the reads to `KRATE_PUBLIC_*` and lists the variables to set for the build.
- Pages under an async layout render per request: an async `_layout.tsx` and everything below it
  run at request time, so a static page beneath one becomes server-rendered (the build lists each).
  `KR7101` now means a `template.tsx` or `error.tsx` below an async layout, and `KR8013` fires only for
  a React-library hook outside `"use client"` or a compiled component in a synchronous static layout.
- Node server bundles (API routes, server actions, SSR pages, per-request metadata routes) now bundle
  every import that resolves outside `node_modules` (workspace packages, tsconfig `paths` such as `@/`)
  instead of leaving it external, where plain Node could not load it.
- Deployed servers read the key that seals inline server-action arguments only from
  `KRATE_ACTION_SECRET`; it is never written into a deploy bundle. Set it in the deployment's
  environment when inline actions capture values (the deploy warns).
- A QuickJS API route that times out returns 504 instead of 500, and `request.signal` is aborted.
- Page config moved from an inline `window.__KRATE_CFG__` script to a
  `<script type="application/json" id="__krate_cfg__">` block, written only when needed. Code that read
  `window.__KRATE_CFG__` must read the block.

### Added

- Async layouts: `_layout.tsx` may be async, read the session or database, and call `redirect()` or
  `notFound()` to guard its subtree.
- Compiled React components (lucide icons, Radix and shadcn primitives) render inside async pages and
  layouts: stateless ones become HTML with no JavaScript, stateful ones are server-rendered and hydrated
  as islands. One copy of each package is shared by every island and boundary on a page, so module
  stores (sonner's `toast()` with a layout `<Toaster />`, zustand, jotai) are page singletons. A
  `ThemeProvider` that only adds a `<script>` is lifted out of a layout.
- Real-React islands for libraries built on `react-reconciler` (`@react-three/fiber`, drei,
  `react-konva`): detected automatically or listed in `react.islands`, client-rendered with
  `client:fallback` text, and bundled with the project's own `react` and `react-dom` only on pages that
  use them. `"use client"` modules that import such a library run on real React.
- Server actions: exports built by a wrapper (`export const save = createAction(...)`), default and
  renamed exports and named re-exports; each export is checked to be a function when the module loads.
  Inline `"use server"` functions in server components and async layouts are hoisted into action
  modules, and the values they capture are sealed (AES-256-GCM, bound to the action) in the action URL.
- Metadata routes (`robots.ts`, `sitemap.ts`, `manifest.ts`) with `dynamic = "force-dynamic"` or
  `revalidate` run per request on `krate serve`, dev and the node, vercel and netlify targets, with
  `headers()` and `cookies()`. `KR7134` warns when that is set inside a dynamic segment.
- `serverExternalPackages`: packages kept external in Node server bundles. A `.node` native addon is
  detected and its package made external automatically, and the route runs on the Node sidecar.
- `krate deploy --target node` traces external packages (with their dependencies and the installed
  platform binary) into a minimal `node_modules` next to `server.mjs`, and `outputFileTracingIncludes`
  copies files the server reads at runtime. See the new self-hosting guide.
- `envDir` loads `.env` files from another directory, such as a monorepo root.
- `export const maxDuration = N` in an API route sets its timeout on every runtime, the Vercel function
  config included (default 30 s).
- `after()` from `@krate/runtime/server` runs work after the response is sent (`waitUntil` on serverless
  targets). `request.nextUrl` is available in API routes on every runtime.
- `@krate/runtime/testing`: `callRoute` and `runAction` call an API route or server action in a unit
  test without a server. See the new testing guide.
- Cloudflare: `import { env } from "cloudflare:workers"` and `getPlatform()` / `getCloudflareContext()`
  reach every binding and `ctx` per request; the exports of `src/cloudflare.ts` (or `deploy.worker`),
  such as `scheduled`, `queue` and Durable Object classes, are merged into the generated Worker.
- `trailingSlash: "always" | "never" | "ignore"`, honoured by `krate serve` and every server target.
- `cssImportRoots`, and CSS `@import` of `.css` files inside the workspace root or through
  `node_modules`, so a sibling monorepo package can share tokens.
- `<Link basePath={false}>` and `<a data-krate-no-base>` link to pages outside the base path; the
  `KR7144` hint points at them.
- `<html>` and `<body>` attributes: `site.htmlAttributes` and `site.bodyAttributes` in the config, and
  `<html className="dark" />` / `<body className="..." />` inside `<Head>` per layout or page (merged
  site, layouts, page; per request on SSR pages). The Next migrator moves root-layout `<html>` and
  `<body>` attributes there.
- `Head`, `Link`, `Image`, `Script`, `Style`, `Icon`, `Suspense` and `SyntaxHighlight` are exported from
  `@krate/runtime` and work in any component module, islands included.
- A 404, 403 or 401 page that is SSR, async or uses runtime components renders per request with its
  status on `krate serve`, dev and the server targets; `output: "static"` still prerenders it.
- `KR8353` warns when a `<Head>` value cannot be computed at build time, so crawlers would not see it.
  Head values may use `URL` and `encodeURIComponent`.
- `krate migrate nextjs` maps `next/font/local` and the `geist` fonts, keeps `maxDuration` in API
  routes, drops `runtime` and `preferredRegion`, maps `after()`, types `NextRequest` and adds cookie
  methods to the `NextResponse` shim, and translates `serverExternalPackages` and
  `outputFileTracingIncludes`.
- Compatibility matrix rows for sonner, cmdk, embla-carousel-react, @dnd-kit, @tanstack/react-table,
  next-themes and `motion/react` (tested in Chromium with the real packages), and for the real-React
  libraries.
- `@krate/auth`: authentication in the style of NextAuth, configured from the new `auth` key in
  `krate.config.ts`. 56 built-in OAuth and OpenID Connect providers, credentials, email magic
  links and Resend, JWT or database sessions, a Drizzle adapter for SQLite, PostgreSQL and MySQL,
  TOTP two-factor authentication with backup codes, passkeys (ES256, EdDSA and RS256), route
  protection from `auth.protect` rules, and a browser client. It runs in the embedded runtime, so
  API routes and middleware need no Node.js process.
- `auth` is validated at load time: unknown keys warn, invalid values fail the build, secrets in
  provider options are rejected with a pointer to `AUTH_<PROVIDER>_SECRET`, and database
  sessions, two-factor authentication and passkeys fail without `auth.adapter.dialect`.
- The serializable part of `auth` is delivered to API routes, server actions and middleware as
  `globalThis.__KRATE_AUTH_CONFIG__`, so packages that stay external in the Node sidecar read it
  too.
- The embedded QuickJS runtime installs each group of Web APIs the first time code reads one of
  its globals, so creating a VM costs about 1.4 ms instead of 3.2 ms. Config and
  Tailwind evaluation, `generateStaticParams`, metadata and isolation-mode API requests get
  faster. Pooled API and middleware workers preload the groups the build found in their
  sources (all of them for builds without that list).
- The API runtime compatibility reference cites the QuickJS vs Node differential corpus case
  by case. Rows whose cases record a difference from Node are partial and name it: `URL`,
  `Headers`, `Blob`, `atob`, `Event` and `crypto.getRandomValues` moved from supported.
- API routes and middleware built for the embedded QuickJS runtime are checked against its
  Web API surface: a global it lacks (`TransformStream`, `WebSocket`, `Buffer`,
  `crypto.subtle.encrypt`, ...) is a `KR7160` warning at the first use, once per file or
  dependency. Run-time `NotSupportedError` messages carry the same code. The supported surface
  is listed in the new API runtime compatibility reference, generated from the ledger the
  compiler checks against.
- `logs: { enabled: true }` saves each `dev`, `build`, `serve`, `start`, `check` and `deploy`
  run's console output to `.krate/logs/<timestamp>.log` (`logs.dir`, `logs.keep`, default 20).
- `krate upgrade [version|tag]` moves the project's `@krate/*` packages to the latest or a given
  version with the project's package manager (`--dry-run`, `--global` for the installed CLI).
- A once-a-day "new version available" line, from a cached registry lookup. Off with
  `updateNotifier: false`, `CI`, `KRATE_NO_UPDATE_NOTIFIER`, a non-terminal stderr or `--json`.
- `krate doctor --fix` aligns an older `@krate/core` with the binary, adds `.krate` and the output
  directory to `.gitignore` and removes a `.krate` cache left by another version.
- `krate explain <code>` prints the cause, fix and docs link of a diagnostic code (`--json`).
- `krate dev --open`, a LAN URL when the server listens on the network, `--qr` to add a QR
  code for it, and `--repl` for `r`, `o`, `c` and `h` terminal shortcuts (followed by Enter). A
  restart no longer opens another browser tab.
- `krate build` prints a per-route gzip size table with the change since the last build
  (`--no-summary` hides it).
- `krate info` prints a paste-ready environment report for bug reports (`--json`).
- `krate clean --dist`, `--cache` and `--all`, and the space each removal freed.
- The dev overlay shows a failing SSR renderer, API sidecar or middleware with the last 40
  console lines, and keeps showing it to tabs that connect before it recovers.
- `site.url` says where the site lives: `https://example.com/example/a` serves it under
  `/example/a` with no `basePath`.
- `basePath: false` on a redirect rule matches and sends paths outside the base path, as in
  Next.js.
- `KR7144` warns about a plain `<a href="/x">` or `<img src="/x">` under a base path.
- `krate deploy --target gh-pages` warns about a `CNAME` file together with a base path, because
  GitHub Pages serves a custom domain at its root.

- `KR8360` warns when a conditional renders the same component in both branches but the build
  cannot keep one instance: render-prop children, a differing `ref` or spread, a side-effecting
  test, a lazy component, or a wrapper element built by each branch.
- MCP: `move_file` updates the imports that point at the file, and its own, in one undoable
  transaction. `delete_file` warns about remaining importers.
- MCP: `format_file` runs the project's prettier or biome, opted in with `"format"` in
  `allowTasks`. It used the compiler's printer, which put a whole JSX page on one line.
- MCP: checks that name an element report a source line, marked `approximate`, and diagnostic IDs
  are unique. `get_diagnostics` and `audit_page` refuse an unknown route, and `audit_page` counts
  follow its filters.
- MCP: every tool scrubs the project path from its output, on Windows too.
- MCP: `create_page` accepts catch-all routes, reports project-relative paths, and its templates
  build. `browser_eval` honours `timeoutMs` for the expression. `dev_server` stops the server
  gracefully. `get_logs` accepts `since: "10m"`. Installed versions are found in workspaces.
- MCP: 28 new tools and 5 new prompts, for 60 tools and 13 prompts: live dev server state and
  errors, page and bundle explanations, project metadata, `env_check` (names only), audits,
  guided upgrades, and browser checks. `dev_server`, `typecheck`, `page_check` and
  `browser_eval` need `dev`, `typecheck` or `browser` in `allowTasks`. See the MCP docs for the
  comparison with Next.js.
- `krate dev` writes `.krate/dev-server.json` and `.krate/dev.log`, and serves `/__krate/state`.
- `KR8351` for a CSS-signal trigger or content that has no partner, and `KR8352` with a specific
  hint for build-time "cannot evaluate" errors.
- A guide to using shadcn/ui with Krate.
- CI covers Node 24 and 26, and runs the install smoke test on Linux, macOS, Windows and Alpine.

### Krate Native (experimental)

Krate Native compiles a Krate app into a desktop app: the TSX UI runs in the system webview and
host code runs in the native process. Names and options may change between releases.

- `krate native init | check | dev | build | profile | package | sign | notarize | keygen`.
  `build` writes an app directory for the prebuilt shell; `build --release [--target os/arch,...]`
  links one executable per platform (`dist-native/<os>-<arch>/<productName>`, `report.json`) with
  only the SDK areas the app reaches, Go host code registered in process and the UI in an embedded
  archive. `--package` packages every target. `native.release.compile` and `native.release.pgo`
  work, and `krate native profile` collects a CPU profile.
- Host code in Go or marked TypeScript (`.host.ts` files, `// @host`, `@host` functions), called from the
  UI as async functions, streams and signals, on QuickJS in the shell, or in child processes on
  Node, Bun, Deno or (experimental) scriptc, with per-module islands. A release compiles each child
  into an executable under `host/` next to the app. `@build` and `//krate:build` functions run
  once during the build and their results are baked into the prerendered UI.
- The SDK (`@krate/app/<area>` in the UI and host code, `sdk/<area>` in Go): files, SQLite,
  key-value storage, secrets, network, windows, dialogs, clipboard, menus, tray, shortcuts,
  notifications and updates, under permissions that the build derives from the code, checks
  against the `native.permissions` allowlist (`KR7210`) and narrows per window.
- Packaging and updates: icons, `.app`, `.zip`, `.dmg`, MSIX, NSIS, `.syso` resources, `.deb`,
  `.rpm`, AppImage; code signing and notarization; an Ed25519-signed updater (`native.updater`,
  `@krate/app/updater`, the `updater` permission).
- New diagnostics `KR7200` to `KR7245`. Documentation under `/docs/native/`.

### Fixed

- `export * from "./x"` in a `"use client"` module no longer fails with KR7114 when `./x` is a local module: it is expanded into the target's runtime exports (a types-only target contributes none), and a component imported through a `"use client"` module (a barrel re-exporting a directive-less file) is a client component for that import, as in Next.js. The remaining KR7114 case (a package or a cycle) prints its hint without doubled quotes.
- Inline `"use server"` actions declared inside a callback of an async server component (a `.map` row
  action capturing the row) no longer fail with KR7001. The error for a capture in a non-async component
  now names the variable, and a value that cannot be sealed reports the action's `file:line:col`.
- Inline `"use server"` actions in a sync component (`function DeleteButton({ id })`) that captures a prop
  now work when the page renders per request (async page or layout, `dynamic = 'force-dynamic'`, ISR or
  SSR); KR7001 remains only for a prerendered page, with a message saying so.
- `lazy(() => import("./X").then((m) => m.X))` now builds like the `({ default: m.X })` form instead of
  failing with KR4001, and `krate migrate nextjs` drops the whole `next/dynamic` options object even
  when it contains `loading: () => <div>{...}</div>` or other nested braces.
- A page whose `export const metadata` is a function call (`pageMetadata(...)`) and that has
  module-level JSX no longer
  fails build-time metadata evaluation with "React is not defined", so its `<title>` and meta tags are kept; the KR7122
  warning now names `metadata` or `generateMetadata` and points at the export's line.
- A component-local `const` holding plain JSX (`const intro = <p>Hello <b>there</b></p>`) no longer
  makes a static page ship the runtime and an empty hydration script; JSX with handlers, refs,
  spreads or components still hydrates.
- An SSR 404 page (and any page rendered per request) whose dynamic attributes read server values
  (`href={env.HOME}`, `className={...}`) no longer loads the runtime and a hydration script that
  carried the server code, including on Cloudflare. The router also removes its inline
  speculation-rules script when a header CSP blocks it.
- An `error.tsx` below an async layout (the shape `krate migrate nextjs` produces for a Next.js route
  group) no longer fails the build with `KR7101`: it is the page's error boundary, rendering inside the
  layout (status 500, no-store) when the page throws. Only a `template.tsx` below an async layout still raises `KR7101`.
- `krate migrate nextjs` places the imports it adds (`after` from `@krate/runtime/server`, `lazy`, the
  NextResponse shim) below the file's directive prologue, so `"use server"` stays the first statement.
- The `@radix-ui/react-slot` shim exports `createSlot` and `createSlottable`, which recent Radix
  collection, primitive and dialog packages import.
- Unknown extensionless URLs (`/nope`, `/a/b/c`) answer with the site's 404 page instead of a bare
  `404 page not found`, on the Cloudflare, Vercel, Netlify and node targets and in `krate serve`,
  whatever the request's `Accept` header says; with `trailingSlash: "always"` an unknown `/nope` first
  redirects to `/nope/`, like a page.
- A page rendered per request (SSR, ISR, an async page, the SSR 404/403/401 page) that has no state, event
  handler, client island or server action form loads no runtime and no hydration script, and
  expressions on such a page that read no signal are no longer re-run in the browser, so server code
  (`return env`, process env reads) stops reaching the client script and blanking server-rendered text.
- A component-local `const` with a build-time value (`const label = 'x' + 'y'`) no longer makes a fully
  static
  page ship the runtime; it is baked into the HTML like a module-scope constant.
- Two modules that export the same name and are both re-exported by a barrel (`export { dup } from
  './a'`) keep their own binding: each importer calls its own `dup` instead of whichever module came
  last. `KR7116` now only reports clashes that cannot be renamed (`export *`, the page module,
  `import()`, "use client"/"use server" modules, alias exports).
- KR7116 (a name exported by two modules that cannot be renamed) is reported once per build instead of
  on every page, names the importing modules, and no longer fires for a package export that is bundled
  apart from the project.
- KR7101 no longer says "a error", and the diagnostic points at line 1, column 1 of the offending file
  instead of 0:0.
- `site.htmlAttributes` and `site.bodyAttributes` accept JSX names: `className` is written as `class`,
  `htmlFor` as `for`.
- The build-time `data-krate-no-base` marker (from `<Link basePath={false}>` or a plain anchor) is no
  longer left in the emitted HTML; the link stays unprefixed.
- `.ts`, `.mts` and `.cts` files are never lexed as JSX, so `a < b || c > d` in a plain TypeScript file
  (including a workspace package outside the project root) no longer fails the build.
- `import "server-only"` works in every server context (build-time passes, metadata routes, middleware,
  actions, route handlers), and CommonJS dependencies that `require` Node built-ins load in Node bundles
  through a `createRequire` banner.
- One build reports every server-action (`KR7004`), metadata-route (`KR7130`) and page error instead of
  stopping at the first.
- JSX held in module-level or component-local constants, and JSX passed as a prop (single, array,
  reading the caller's values, into a stateful component), is rendered into the HTML. It used to come
  out as an empty slot or fail with `KR8352`.
- A constant holding JSX, or an array or object of JSX, passed as a prop (`<Card body={intro} />`,
  `<LegalDoc sections={sections} />`) is rendered into the HTML instead of failing with `KR8352`.
- A `fetch(..., { cache: "no-store" })` in client code no longer turns the page into SSR.
- SVG types accept `xmlns`, `xlink:*`, `xml:space`, filter and gradient elements and event handlers.
- `<Head>` inside a component no longer throws `Head is not defined` on SSR pages, and Head content from
  island components reaches prerendered pages (the last `<title>` wins, as at request time).
- A prerendered page whose only stateful component sits in `<Head>` ships no hydration JavaScript.
- `/404` and `/404.html` answer with the 404 page and status 404 instead of redirecting.
- A symlinked workspace package with TypeScript source loads in the Node server output.
- A middleware or API bundle whose last top-level statement evaluates to a BigInt no longer fails
  to load with "Do not know how to serialize a BigInt". Bundled crypto libraries produce one.
- The generated types of a Go host signal say `T | undefined`, as for `@host` TypeScript signals: it
  reads `undefined` until the host sends the first value, which pages typed as `T` crashed on.
- An aliased export (`export { remoteOnEvent as onEvent }`) and another module's declared export of
  the same name no longer meet in the page scope: the page's `onEvent` imported from one module
  used to call the other one. The declaring module moves to a private name.
- Two client navigations in quick succession no longer leave an unhandled "Transition was skipped"
  rejection on the page: the router also handles the `ready` promise of a view transition the
  newer navigation skipped.
- Module constants are declared in the order their initializers need: the name after a dot
  (`getClient().signal`) no longer counts as a read of a constant called `signal`. That false
  dependency put `const signal = hostSignal.signal` from `@krate/app` before `hostSignal`, so
  pages importing `@krate/app/window` threw on load (the `examples/native-views` views never
  opened).
- A named `async function` used as an event handler keeps `async` in the hydration code (it
  was emitted as a plain function, so `await` was a syntax error).
- A capitalized arrow constant imported by a page and used in a handler is declared once (the
  page script declared it as both `var` and `function`, which does not parse), and an arrow
  function that is an operand of `??`, `||` or another operator is parenthesized.
- Binary data crosses between Go and the embedded QuickJS runtime as raw ArrayBuffer copies
  instead of base64 text encoded and decoded in JavaScript: request and response bodies
  (buffered and streamed), middleware responses, `fetch` request and response bodies,
  `crypto.subtle.digest`/`sign`, `crypto.getRandomValues` and multipart parsing. A 1 MiB binary
  request body now takes about 1.4 ms instead of 1.8 s, a 1 MiB response about 3 ms instead of
  1.2 s. The VM memory limit grows only when a handler reads the body, by what that read needs,
  and a pooled worker collects the body's garbage before serving the next request.
- `Response.error()` in the embedded API runtime threw a `TypeError`.
- Runaway recursion in the embedded QuickJS runtime (an API route, middleware, `krate.config.ts`)
  exhausted the Go stack and killed the process instead of throwing. `KRATE_JS_STACK_KB` now
  defaults to 16 (about 16,000 nested calls) and is capped at 32.
- Timers in the embedded API runtime ran every due callback in one batch, so promise callbacks
  queued by one `setTimeout` ran after the next timer instead of before it, unlike Node.
- `basePath` now reaches every URL the compiler emits. Imported assets, generated CSS `url()`s,
  dynamic `import()` chunks and workers used to 404 on static hosts, as did Markdown links,
  convention OG images, `llms.txt` and feed links. Deploy adapters write rules for the prefixed
  paths, and speculation rules exclude the prefixed `/api` and `/__krate`.
- Redirects under a base path no longer leave the site. Config redirects, `redirect()` and route
  handler `Location` headers used to drop the prefix, costing an extra hop for GET and failing
  for POST.
- `new URL(rel, import.meta.url)` threw in page scripts, because the substituted URL was not
  absolute.
- `krate migrate nextjs` closes the gaps a production Next.js 16 migration found: it migrates
  `proxy.ts` as middleware and lists root and `src/` scripts it does not migrate; it never
  emits a looser rule for a redirect, rewrite or header with `has`/`missing` (each is a manual
  item) and warns on duplicate rules; it translates the static entries of a partly computed
  `headers()` and says when CSP or HSTS were not migrated; every untranslated `next.config` key
  (`output`, `transpilePackages`, `serverExternalPackages`, `outputFileTracingIncludes`,
  `loadEnvConfig`, computed `images.remotePatterns`, ...) gets its own report line; route
  handlers outside `app/api` keep their public URL through a generated rewrite; static
  `icon.*`/`apple-icon.*` become public files with a head link; `instrumentation.ts` and inline
  `"use server"` functions (`KR7001`) are manual items; `--verify` checks that `@krate/core`
  resolves before writing anything; and the generated config no longer sets `entry` to an
  arbitrary page.
- A config rewrite whose destination is an `/api` route is answered by the API handler. It
  returned 404.
- Errors thrown during a page's first render reach the dev overlay and `/__krate/state`. The
  overlay loads after the page script, so it missed them; the page head now buffers them.
- Importing `krate/content` with no collections configured says so, instead of telling you to
  install a package called `krate`.
- Scaffolded projects include `@types/node`, so `process.env` in an API route typechecks.
- `cond ? <A x /> : <A y />`, ternary chains and early returns of one type keep one instance of a
  stateful component and switch its props, so state, typed input, refs and mount effects survive
  the flip, as in React. Different `key`s still remount.
- A value in a child component's signal could print its own identifier name into the HTML, as
  in `createSignal(initial)` rendering `initial`. A value the build cannot know now renders
  empty until hydration.
- CSS scope handles passed as props threw a `ReferenceError` on page load. Handles now reach
  their triggers and panels at any depth and through `{...props}`.
- An omitted prop rendered a same-named module constant instead of its default or undefined.
- Renamed and nested destructured parameters failed to build, rendered empty values, or threw
  in the browser. A signal passed through a pattern the build keeps destructured now warns.
- `<a download>`, `download={true}` and `capture={true}` were dropped from the HTML.
- Pages with two or more shadcn components built on `radix-ui` failed with `KR8321` or a false
  "recursive component" error. Aliased imports from compiled packages are no longer confused
  with same-named local components, and `import { Slot } from "radix-ui"` is Krate's native Slot.
- React islands build and server-render under Preact 11. The "Preact is not installed" message
  no longer appears when it is installed.
- `[param]` pages that render a component with a signal no longer fail with "unresolved
  identifier createSignal".
- A blog `[slug]` page no longer puts other posts in client JS, in the fallback page or in
  common shapes such as `const posts = getCollection(...)`, helpers, destructured params and
  arrow components. `KR8810` reports entry count and size, including for generated pages.
- 0.4 projects with `baseUrl: "./src"` and `./src/*` path targets build with one `KR7125`
  warning. `krate fix --apply` clears it.
- The Node API sidecar receives `.env` values, and API routes and middleware see `NODE_ENV`.
- The page build cache no longer replays HTML after `.env` or `KRATE_PUBLIC_*` changes.
- `KR8310` no longer fires for async server components, and `KR8007` no longer fires for a
  tag that only starts with a same-file component's name, such as `<Popover>` beside `P`.
- BigInt children render as text instead of throwing, and function values in a runtime spread
  are no longer written as attributes.
- `Dialog`, `Sheet`, `Popover` and `DropdownMenu` parts without a `value` open each other.
- A throwing effect cleanup no longer stops later cleanups or queued effects, and teardown
  errors are logged.
- CSS-signal widgets keep their keyboard and ARIA enhancer after a client navigation.
- Invalidating the prefetch cache drops prefetches in flight, and a hash step cancels a loading
  navigation.
- Components suspended on `use()` no longer mount after you navigate away.
- Effect order for components in server-rendered rows matches React, and keyed moves no longer
  fire `onBlur` or `onFocus`.
- In `krate dev`, a browser error no longer hides a failing build from `/__krate/status` or from
  tabs that connect later.
- Stopping the server as the Node sidecar starts no longer races, and cannot leave it running.
- `krate deploy --target <unknown>` fails before building, flags accept `--flag=value`, and a
  flag missing its value is named. `krate doctor` warns when `@krate/core` and the binary differ.
- Unknown keys in `checks` and unknown built-in rule IDs print a config warning with the nearest
  name. A syntax error in a file imported by `krate.config.*` reports `file:line:col`.
- `create-krate-app` and `create-krate-docs` exit 1 when no project name is given, and name the
  project after its directory for `.`.
- A failed release can be re-run, since versions already on npm are skipped. A pre-release
  identifier npm cannot use as a tag fails at the start, and a stable patch lower than `latest`
  no longer moves `latest`.
- Compatibility matrix rows backed by CI tests read "Supported" instead of "Beta".
- `site.htmlAttributes: { className: 'dark' }` (and `bodyAttributes`) written in `krate.config.ts` now
  emits `class="dark"`; the config loader lowercased the key to `classname` before the JSX name was mapped.
- A `Cache-Control` set by middleware or a `headers` rule is kept on pages rendered per request instead of
  being replaced by `no-store` (`krate serve` and the Cloudflare, Vercel, Netlify and node targets). The
  default stays `no-store`, and error pages, renders that read request state and actions keep it forced.
- `krate migrate nextjs` lists `preact` as a manual item with the exact install command when the app uses
  React and does not depend on it (`--update-package` adds it), instead of leaving the first build to stop
  with KR8009.
- KR7116 (two modules export the same name and it cannot be renamed) is reported once per build as the
  changelog says; it was reported again for every page whose set of importing modules differed.

### Security

- Krate Native: a page could take over a permission scope by naming one of its views after the
  window id `window.open` would hand out next. The window then shared the id, and closing it removed
  the scope of that id, leaving the view with every permission of the app. Window ids now skip ids
  that views hold, and an id is claimed atomically when it is chosen, so two concurrent `addView`
  calls with one id can no longer make the losing call drop the winner's scope. See
  `packages/native/SECURITY_REVIEW.md`.
- Krate Native: a stream started from one view (window events, remote requests, media requests,
  port messages, ...) could be opened by any other view, since stream ids are a counter; a view with
  an empty scope could read what the producer emitted with the starter's permissions. A stream is now
  bound to the native view that started it. Plugin signal gates now check the native view instead of
  the `x-krate-window` header the page sets.
- Krate Native: `startWorker(name, { env })` no longer accepts variables that change how a runtime or
  the dynamic loader loads code (`NODE_OPTIONS`, `LD_PRELOAD`, `DYLD_*`, `BUN_*`, `DENO_*`, `PATH`,
  ...) or the shell's own `KRATE_*`: a page with the `worker` permission could run arbitrary code with
  `NODE_OPTIONS=--import=data:...`.
- Krate Native: the prebuilt-shell distributable (`<productName>` with `app/` next to it) refuses
  `--dev` (unless built with `native.debug`) and another app directory on its command line, which let
  anyone who could start it run their own code or drive its pages under the app's signature,
  keychain items and camera or microphone consent.
- Krate Native: the single-instance socket falls back to a private `~/.krate-run` directory before the
  shared temporary directory when the runtime directory path is too long.

### Known issues

- Under Preact, an uncontrolled Radix Tooltip closes right after it opens, and a Dialog opened
  from inside a Popover dismisses the Popover. Both come from how Preact orders effects and
  handles portals, and reproduce without Krate. The React compatibility guide has the affected
  components and tested workarounds.

## 0.5.0-beta.3 (2026-11-06)

### Breaking

- A middleware file with no handler is a build error (`KR7143`), and a server running such a
  module answers 503.
- `krate serve` and `krate start` refuse to start when `manifest.json` or
  `.krate/api-routes.json` is corrupt, as they already do for `server-manifest.json`.
- `viewTransitions` and `ppr` take effect. They were ignored on the normal config path, so a
  setting left in a config can change output.

### Fixed

- Security: middleware exported as a default function runs in every runtime. The Node, Bun and
  Deno sidecars and the serverless adapters skipped it, and any auth check with it.
- A corrupt `manifest.json` no longer reads as "no middleware recorded" or "no closed routes".
  A later rebuild that leaves it unreadable logs the error and keeps the last good segment index.
- `@krate/core-<platform>` packages are published with an executable compiler again. In
  `0.5.0-beta.2`, `krate` failed with `spawn .../bin/krate EACCES` on every Linux and macOS
  install, because pnpm packs a file outside `bin` as `0644`. The release gate now packs each
  package and checks the mode, and the wrapper restores a lost exec bit on an installed
  binary. Until you upgrade, run `chmod +x node_modules/@krate/core-*/bin/krate`.
- `@krate/core` has no `postinstall` script any more, so npm's script gating has nothing to
  warn about. It only printed a message; a missing or unusable compiler now gets a hint when
  `krate` runs.

## 0.5.0-beta.2 (2026-11-05)

### Breaking

- More builds that used to ship broken output now fail, each with a file and a fix:
  an element type that cannot be resolved (`KR8320`), a member tag that names no
  component (`KR8321`), a `@static`/`@server` page reading a module value that top-level
  code changes in a way the build cannot run (`KR8322`), an `<Icon>` that cannot be
  resolved (`KR8840`, so offline builds need `.krate/cache/icons`), a `generateStaticParams`
  entry with a missing or unsafe param or a duplicate path (`KR7142`), malformed
  frontmatter, and a CSS, JS, runtime, chunk, manifest or image file that cannot be written.
- A syntax error in `krate.config.*` fails with `file:line:col`, including in `extends`
  bases; text after `export default { ... }` used to be ignored. The static config
  fallback rejects wrongly typed nested values. `checks.failOn` accepts only `"error"` or
  `"warning"`, non-string `checks.ignore` and `checks.custom` entries are errors, and
  `--config <path>` fails when the file does not exist on every command that takes it.
- `krate serve` and `krate start` refuse to start when the build recorded middleware and
  `.krate/middleware.js` is missing, or when `server-manifest.json` is corrupt. Rebuild
  older `dist` folders to get the middleware record; without it the server only warns.
- Importing `.scss`, `.sass`, `.less` or `.styl` is a build error (`KR7123`, Krate
  has no compiler for them); the styles used to be dropped silently. Importing a name
  `@krate/runtime` does not export is a build error too (`KR7124`); it used to fail
  in the browser with `x is not defined`.
- Builds that used to pass and ship broken output now fail: a `@server`, `@static` or
  `@runtime` component with state, effects or event handlers (`KR7110`), a component
  whose tier sources disagree (`KR7115`), and a higher-order component Krate cannot
  inline (`KR8030`). `serverDirs` and `runtimeDirs` now match project-relative paths
  as documented, so components in those directories are server or runtime components
  (and are checked as such).
- A page that renders inside an error boundary no longer disables layout
  persistence (the runtime tears down class-component owners by route scope).
- Content collections: `date` fields must hold an ISO date (a validation error
  otherwise), two entries with the same slug (`a.md` and `a.mdx`) are an error,
  enum fields are typed as unions, and fields with a `default` are no longer optional.
- Static pages no longer load the shared runtime chunk when other pages of the site
  have islands (26.1 KB gzip, 17.8 KB with `spa: false`, saved per static page). A
  site with no islands already shipped none.
- Behaviour changes for servers behind a proxy or with middleware are listed under
  Security: matcher-less middleware runs on URLs with a static extension, and
  `server.trustProxy` reads `X-Forwarded-For` from the right.

### Added

- Metadata routes: `sitemap.ts`, `robots.ts`, `manifest.ts`, `opengraph-image` and
  `twitter-image` files in the pages directory, the same shape as Next.js, including per-segment
  and per-`generateStaticParams` image files. `ImageResponse` (`@krate/runtime/og`) draws a real PNG at
  build time in pure Go (flex layout, text and fonts, colours, gradients, images); anything it cannot
  draw is a build error (`KR7130`-`KR7133`). The image URLs reach `og:image` and `twitter:image`
  automatically, a metadata file wins over a plugin, and `krate migrate nextjs` maps all of it natively.
- The Cloudflare bundle no longer imports `node:net`, `node:tls` or `node:fs/promises` (the Redis
  cache adapter and the local image reader are left out of it); `cache.adapter: "redis"` on the
  cloudflare target is a build error that says to use your own HTTP cache adapter or another target.
- `<meta>`, `<link>`, `<script>` and the title inside `<Head>` of an async page or
  async server component are in the `<head>` of the first response bytes in every
  request-time mode (SSR, ISR, streaming, async components), in `krate start` and the
  portable handler alike. They used to render in the body, where crawlers ignore them.
  A page tag replaces a baked layout or config tag with the same `name`, `property`,
  `http-equiv`, `charset` or canonical link; a `<Head>` inside a Suspense region that
  streams out of order is applied by the region script and logs a warning, because the
  head had already been sent. A page title is added when the shell has none (it used to
  be lost), and `<script>`/`<style>` text in a request-time render is no longer entity
  escaped, so JSON-LD is valid.
- Go components: `kr.Request` has recording accessors `Header`, `Cookie`, `QueryValue`
  and `Param`. A runtime Go component cached with `revalidate` is keyed on the values it
  read through them (like `Vary`), so visitors with different cookies never share a
  render and an unrelated cookie, header or query key no longer splits the cache.
- The Docker target writes a multi-stage Dockerfile: the runtime image has only the build
  output, the server parts of `.krate`, the config and production dependencies (no
  sources, dev dependencies or Go toolchain), runs as `node` and keeps the healthcheck.
  `--build-arg KEEP_DEV_DEPS=true` keeps dev dependencies. A small SSR + API + action
  project went from 543 MB to 419 MB (329 MB of it the base image).
- `KRATE_FLASH_SECRET` sets the key the no-JS flash cookie is signed with, for the Go
  host, the action sidecar and the portable handler (see Security).

- `new Date()`, `Date.now()`, `Date()` and `Math.random()` have a build-time value. In a static
  or server component they are the time of the build (`SOURCE_DATE_EPOCH` fixes it, and makes
  two builds of the same sources byte-identical); in a client component the static HTML shows
  the build value and the browser computes it again on hydration. Dates are evaluated in UTC with
  the en-US locale, so a build does not depend on the machine: deterministic parts (`new Date(ms)`,
  ISO strings, `Date.UTC`, `getUTC*`, `toISOString()`) fold everywhere, and anything that reads the
  local zone or locale (`getHours()`, `toString()`, `toLocaleDateString()`) keeps its client binding.
  The build-time interpreter has a `Date` (getters, setters, parsing, `toLocale*String` for
  en-US), `Map` and `Set`. Docs that said `new Date()` has no build-time value are updated.
- The build-time interpreter's `arr.entries()`, `arr.keys()`, `arr.values()` and the `Map` and
  `Set` equivalents are real iterators (`.next()`, `.toArray()`, `.map()`, ...), no longer arrays,
  and work with `for...of` (including `for (const [i, v] of ...)`), spread, `Array.from` and
  destructuring. The differential test against Node covers 260 more expressions and 1,700
  `toLocale*String` cases.
- Tailwind `@utility`: `--modifier()` works (`name-VALUE/MODIFIER`), including the typed
  forms (`--value(--color-*, [color])`, `--value(integer)`, `--value([length])`,
  `--modifier(number)`, `--default()`), and nested rules, `@media`, `@supports`,
  `@layer`, `@variant` and `@apply` inside a utility body no longer warn. Unresolvable
  declarations are dropped like Tailwind v4 does, custom `@theme` namespaces
  (`--tab-size-*`) work in `--value()`, and `@utility` arguments Tailwind rejects warn.
  Output is compared with Tailwind 4.3 in a golden test.
- `<Activity mode>` (children stay in the page and keep state while hidden; effects clean
  up and run again), `<ViewTransition name default>` (names its element child for the
  router's view transitions) and `useEffectEvent`, with `unstable_` names accepted. Props
  with no effect warn (`KR8031`). New diagnostics: `KR8030`, `KR8031`.
- Code follows the colour mode. `markdown.codeTheme` takes `{ light, dark }` (a plain
  name still means one theme for both modes) and defaults to `github` in light and
  `github-dark` in dark; one stylesheet serves system preference and `data-theme`,
  switching needs no JavaScript, and the HTML keeps one span per token. `<Code>` and
  fenced blocks no longer assume a dark surface: with a light theme the text was
  near-invisible (`#fafafa` on `#f7f7f7`), and the copy button hover was white on any
  background. An unknown theme name warns (`KR7150`). `@krate/base-docs-theme` code
  blocks now follow the page's mode (light blocks in light mode) with AA colours.
  Override the surface with `--krate-code-bg` / `--krate-code-text`.
- Code that changes at run time is highlighted. `<Code lang="ts">{signal()}</Code>`
  rendered plain text; it now shows the new text at once and highlights it after
  120 ms (unchanged text is left alone), and an SSR or ISR page highlights code that
  only exists per request. Both run chroma's own lexers, exported from the binary's
  chroma as small tables and run token for token (identical markup to the build, in
  light and dark): the tokenizer (2.3 KB gzip) and the language's tables (0.4 to 8.6
  KB for the 16 shipped languages) are separate chunks written only for the
  languages a page uses and loaded on first use, and a page without such code ships
  none of it. `highlight: { languages }` lists languages for a variable `lang`;
  `krateHighlight(code, lang)` highlights on demand; `highlight(code, lang)` is in
  `@krate/runtime/highlight`. Text over 64 KB, a line over 2,000 characters or more
  than 400 ms of work stays plain. A direct `<SyntaxHighlight>{signal()}</SyntaxHighlight>`
  no longer freezes the signal's first value. A language outside the shipped list
  warns (`KR7151`).
- `krate fix` repairs the 0.4 template's `tsconfig.json`: `"baseUrl": "./src"` with
  `"@/*": ["./src/*"]` can only mean the project root, so it sets `baseUrl` to `"."`
  (`KR7125`). The edited file also builds on 0.4. The build error for an alias that
  does not resolve names this command.
- Tier decorators: `@server`, `@static`, `@runtime` and `@client` can go directly on
  a function, class or const. On a file's default export or only exported component
  they classify the whole file, like a `// @server` comment; elsewhere they classify
  one component. Misplaced ones fail with `KR7115`. They work in `.js` and `.jsx`
  files too, and a decorator on a function or const wins over a same-named binding.
- `krate typecheck` runs `krate types` and `tsc --noEmit` without the errors `tsc`
  reports for tier decorators, and the new `@krate/ts-plugin` does the same in
  editors. Scaffolds use both.
- Libraries built on React hooks now work. A module that calls a hook of an npm React
  package (zustand, react-hook-form, SWR, TanStack Query, a custom hook wrapping one)
  is bundled for Preact and rendered as an island: server-rendered, hydrated, and
  sharing one store per page. A layout or server page that needs one fails with
  `KR8013`. See "Libraries built on React hooks" in the React compatibility guide.
- CSS-in-JS: styled-components, `@emotion/styled`, emotion's `css` prop (with
  `/** @jsxImportSource @emotion/react */`) and MUI run as client-only islands. A
  `css` prop without emotion's runtime warns (`KR8014`).
- `client:visible` and `client:idle` on an island's root tag defer mounting (and the
  chunk download) until the island is near the viewport or the browser is idle. A
  directive on anything else warns (`KR8012`).
- Islands compile project modules with Preact's JSX runtime even when `tsconfig.json`
  sets `jsxImportSource` to `@krate/runtime`.
- `src/pages/data.json.ts`, `feed.xml.ts`, `robots.txt.ts` and similar build as files:
  `GET` runs once at build time and its body is written to the output, instead of an
  empty HTML shell (`KR7121` for failures).
- A page's `export const metadata` (or a `generateMetadata` that returns a literal)
  becomes head tags; an explicit `<Head>` still wins. Values the build cannot read
  warn (`KR7122`). `src/pages/api/*.ts` warns (`KR7120`) that API routes live in
  `src/api`, instead of building an empty page.
- `krate deploy` runs server features on more hosts. A portable server handler
  (`@krate/runtime/server-handler`) is bundled with the build data into one module
  for `--target node` (`dist/.krate/server/server.mjs`, no Go binary), and for `vercel`
  (Build Output API v3), `netlify` (Functions) and `cloudflare` (Workers with static
  assets) instead of refusing projects with SSR/ISR/API routes/actions/middleware. A
  request matrix checks the node target against `krate start`. Go API routes, Go
  components and sidecars are refused with a hint to use Docker. Caches are per
  instance on the serverless targets, so `revalidatePath`/`revalidateTag` reach only
  the instance that handled the request (reported under `skipped`). They also serve
  per-segment not-found and error pages and run middleware for prerendered pages
  (Vercel and Netlify route page requests to the function, assets stay on the CDN).
  The error digest is the same value on every host. `--target docker` runs as the
  `node` user and adds a `HEALTHCHECK`; the Cloudflare entry trusts `X-Forwarded-*`
  only with `server.trustProxy`.
- Per-segment not-found and error pages: `404.tsx`/`not-found.tsx` and
  `error.tsx`/`500.tsx` in any pages directory. The nearest not-found page answers
  unmatched URLs, closed dynamic params and `notFound()` (404, inside the segment's
  layouts); the nearest error page answers uncaught server errors (500, sanitized
  message plus a digest, never cached) and is a client error boundary around the
  segment with `reset()`. Two files for one role, or a segment page in a `@slot`, are
  build errors (`KR9001`, `KR9002`). The Next.js migrator maps nested
  `not-found.tsx`, `error.tsx` and `global-error.tsx` to native files.
- `krate.config.ts` and plugin hooks see shell environment variables and `NODE_ENV` in
  `process.env`; `fs.existsSync`, `process.cwd/argv/platform`, `__dirname`,
  `__filename`, `module.exports` beside imports, `node:os` and a small `Buffer` are
  provided. A Node built-in the sandbox lacks fails with its name, also when imported
  without the `node:` prefix.
- `krate migrate nextjs` translates the static keys of a `next.config.*` even when
  another key is computed, accepts `async redirects()`, `rewrites()` and `headers()`
  that return a literal array, explains `revalidate`/`dynamic`/`dynamicParams` on
  pages and flags them in layouts, lists every `package.json` change, pins `@krate/*`
  to the running compiler version, and marks the files `--apply` would overwrite in
  the dry run.
- `create-krate-app` and `create-krate-docs` pin every `@krate/*` dependency to their
  own version (`^x.y.z`, or the exact version of a prerelease) instead of `latest`.
- `krate version` reports the native binary instead of the npm wrapper, and warns when
  the two differ. Backslashes in tsconfig `baseUrl` and `paths` are normalized, so a
  config written on Windows resolves on Linux and macOS. A bare import that `paths`
  and `node_modules` cannot satisfy is tried against `baseUrl`, and the package.json
  `browser` field is honoured for client builds.
- Relative images in markdown, MDX and content collections (`![x](./pic.png)`) are
  copied to `/assets/` with a hashed name and linked there, under `basePath`. Fences
  with `{1,3-5}` highlight those lines on pages as well as in collections, titled
  fences keep their header inside asides, and `mdx`/`astro`/`prisma` are highlighted.
- Docs themes: a theme can set `titleSlot: true` in its `defineDocsTheme` descriptor to
  receive the page's leading `<h1>` as `titleHTML`. `@krate/base-docs-theme` marks the
  current nav link with `aria-current` (`SiteNavLink.match: "exact"` limits a link to its
  own page), takes a footer brand block (`footerBrand`), and labels the search shortcut
  with the platform's modifier (`Ctrl K`, or the command key on Apple devices).
- Components: `OTPField` edits the visible digits (Backspace, paste, autofill) and
  submits under `name`; the searchable `Select` and `Command` are keyboard operable
  comboboxes; `ContextMenu` opens from the keyboard; `Tooltip` and `HoverCard` are
  linked with `aria-describedby`, close on Escape and take `asChild`; `Pagination`
  links use `param` and keep other query parameters; `Checkbox` and `Switch` are native
  inputs with `name`, `value`, `defaultChecked`, `disabled` and `id`; every component
  takes `class`/`className`; modal `Dialog`, `AlertDialog` and `Sheet` dim the page,
  make it inert, lock scrolling and close on an outside click; `Form.Field` links its
  description and error to the control and marks it `aria-invalid`; `Card` titles are
  headings. `<Icon>` sizes to the text (`1em`), keeps a solid icon's `fill` and maps
  `size={n}`. Focus rings use one colour.
- Tailwind: `shadow-xl/20`, `drop-shadow-xl/50`, `inset-shadow-*/n`, `text-shadow-*/n`,
  `bg-size-[...]` and `bg-position-[...]` are generated.
- A slot with no build-time value and no client code to fill it warns (`KR8011`)
  instead of rendering empty silently. A decorated class component always warns
  (`KR3011`). `Tabs`, `Accordion` and other zero-JS components built from a `.map()` over
  a value that cannot be read at build time warn with `KR8820`.
- JS plugin hooks and custom check rules are much cheaper on large pages: the page, AST
  and hook context are passed as JSON, and only when the plugin implements the hook.
  `examples/` builds in about 6 s cold (was about 12 s) on a loaded machine. The docs
  table of contents indicator measures layout once and draws when the page is idle.
- `export default () => <div />`, `export default memo(...)` and
  `forwardRef(...)` are components (they built to an empty page), and a tier
  decorator above them applies to the file's default component.
- server component regions are cached by the query keys they read, not the
  whole query string, so random query values no longer fill the region cache.
- `@apply` takes variants (`@apply hover:underline md:flex`) and
  descendant utilities (`space-x-2`), expanded as CSS nested rules, and
  functional `@utility name-* { ... --value(...) / --spacing(...) }` is supported.
- `Slider` takes `onChange` and `onInput`. A handler on an optional prop the
  call site does not pass binds nothing, so only the sliders that pass one
  hydrate.
- Providers in layouts reach React-library islands. A `QueryClientProvider`,
  react-redux `Provider`, `ThemeProvider` or a `Providers` component of the
  project that a layout wraps around `{children}` is lifted out of the layout
  and rendered around every island of the pages under it: server-rendered and
  hydrated inside it, one client/store/theme for all islands of the page, nested
  layouts composing (the inner provider wins). Props can be literals, imports,
  module-level values or constants the layout computes (`useState(() => new
  QueryClient())` with no setter). A wrapper that cannot be lifted is reported:
  `KR8015` when its props read the layout's `params`, a signal or a state setter,
  `KR8016` when it renders markup of its own. It used to fail with "No
  QueryClient set" in the browser.
- Island state survives client-side navigation. The island entries of all pages
  are bundled together with code splitting, so a module several pages import (a
  zustand or Redux store, a query client and its library) is one file with one
  instance: the same store and value after navigating, going back and going
  forward, and a leaving island unmounts and releases its subscriptions. The
  island entry is now `chunks/island_<id>.js`, revalidated on each visit, with
  immutable shared chunks `chunks/isl-<hash>.js`.
- Islands work in pages rendered per request (`dynamic = "force-dynamic"`, ISR,
  streaming). The Node renderer used to run the island components, fail with
  "Invalid hook call" and silently serve the build-time body (dropping the
  page's dynamic content); the server bundle now stubs each island with its
  placeholder and build-time markup.
- Next.js router hooks are exported from `@krate/runtime`: `usePathname`,
  `useParams`, `useSearchParams`, `useRouter` (`push`, `replace`, `back`,
  `forward`, `refresh`, `prefetch`) and `useSelectedLayoutSegment(s)`. A module
  that imports one reads it live, so values follow SPA navigation, back and
  forward. The static HTML carries the page's own route (`/blog/a` for a page from
  `generateStaticParams`), SSR pages see their request, and the hooks ship as
  their own runtime feature (about 1.3 KB gzip) only when a page calls them.
  `krate migrate nextjs` imports them from the runtime instead of generating
  `src/migrated/next-navigation*.ts`.
- `forbidden()` and `unauthorized()` (and the `forbidden.tsx` / `unauthorized.tsx`
  pages, or `403.tsx` / `401.tsx`) answer 403 and 401 the way `notFound()` answers
  404: the nearest segment page inside its layouts, never cached, with soft
  navigation swapping the page in place. They work in SSR, ISR and streaming
  pages, async server components, server actions and API routes, in `krate start`
  and the portable handler. A statically built page or a layout that calls one
  warns (`KR8604`). The migrator maps both pages and the `authInterrupts` flag.
  API routes that call `notFound()` now answer 404 instead of 500.
- `generateMetadata({ params, searchParams }, parent)` runs for real: at build time
  for static pages (once per `generateStaticParams` entry), per request for SSR and
  ISR pages (next to the render, sharing its fetch cache, tags and `searchParams`
  keys). Layouts export `metadata` and `generateMetadata` too; the chain merges root
  to page in Next.js order (`title.template`, `metadataBase`, wholesale field
  replacement, `parent`), and an explicit `<Head>` still wins. The tags are in the
  first bytes of the head, not patched in by script. A `generateMetadata` that throws
  keeps the page and logs; `notFound()` from it answers 404. `export const viewport`
  is read, and a page's viewport meta replaces the default one. `KR7122` now means
  the metadata could not be evaluated at all (it used to mean "not a literal").
  The migrator keeps `metadata` and `generateMetadata` as written and imports the
  `Metadata` types from the runtime instead of wrapping pages in `<Head>`.
- Concurrent identical cached `fetch` calls in one process share one upstream
  request, so a page and its `generateMetadata` reading the same URL ask it once.
- Shared caches. `cache: { adapter: "redis", url }` in `krate.config` moves the ISR
  page cache and the data cache (`fetch` with `next` options, `unstable_cache`) into
  Redis, so instances behind a load balancer serve each other's pages, and
  `revalidatePath` / `revalidateTag` on one instance refresh all of them (a pub/sub
  message also clears the others' region caches and the Go host's caches; a missed
  message flushes). Redis uses a built-in RESP client with no dependency (Go and
  Node); other stores plug in through the documented `CacheAdapter` interface
  (`cache.adapter` names a module). Every cache call has a timeout and a circuit
  breaker: a slow or dead store costs the sharing, never a request, and per-user
  renders are never written. `createKrateHandler({ cache })` and `krate deploy` carry
  it to the serverless targets (the bundle never contains the URL; it reads
  `KRATE_CACHE_URL` or `cache.urlEnv`).

### Fixed

- The YAML number rule no longer takes quadratic time on a long run of digits in the runtime
  highlighter.
- A data race between island detection and the universal AST transforms on modules shared
  across pages.
- Security: middleware fails closed. A middleware module that disappears after startup
  answers 500 instead of skipping auth, and the Node, Bun and Deno sidecar no longer lets
  requests through when the module fails to import.
- A corrupt server manifest no longer serves frozen build HTML with a 200. A dev rebuild
  that leaves one unreadable logs the error and keeps the last good manifest.
- An effect that throws no longer stops the effects queued behind it. They run, the flush
  completes, and the first error is rethrown.
- Top-level statements that change a module value (`for (...) list.push(...)`, assignments,
  property writes, `Object.assign`, calls to module functions that do so) run at build
  time, so static HTML shows the final value. A statement the build cannot run leaves the
  value unknown, and hydrating components read it in the browser.
- A function prop replaces a function default (`onX = () => "dflt"`); the default used to win.
- `<Tag>` renders the element when `Tag` is a module const, a prop, a prop default or a
  folded condition, and `{ as: Tag = "div" }` receives the caller's `as`.
- `<ui.Button>` from `import * as ui` resolves through barrel re-exports.
- Computed object keys (`{ [K]: v }`) fold in object literals, destructuring, style
  objects, component props and `jsx()` calls.
- `@font-face` rules whose last declaration has no `;` no longer break when `font-display`
  is injected.
- A missing `<Icon>` no longer puts its error text and a local path in the page.
- Krate warns once (`KR7140`) when a Tailwind config, `@tailwind` or `@import "tailwindcss"`
  is present but `tailwind.enabled` is off, and warns (`KR7141`) about unbalanced braces and
  unclosed comments or strings. Each stylesheet is balanced, so one file cannot swallow the next.
- `generateStaticParams` no longer duplicates or empties entries when a value is a number,
  and the logged count matches the pages written.
- Tailwind `@theme` variables stay live like Tailwind v4: utilities, `@apply` and
  `--value()` read them through `var()`, so `.dark { --color-primary: ... }` re-themes
  `bg-primary` instead of doing nothing. A default such as `--color-blue-600` or
  `--spacing` goes live once the project sets it; other defaults stay inlined.
  `@theme inline` and `@theme reference` are inlined.
- Tailwind `bg-(--color-blue-500)` defines the theme variable it reads, `max-w-*`
  honours `--container-*` (`var(--container-md)` is defined too), and `fill-*`,
  `stroke-*` and `placeholder-*` accept opacity modifiers (`fill-red-500/50`).
- Pages built per `generateStaticParams` entry (and dynamic-route templates) now host islands: a
  React-library or package component on such a page used to fail the build with "unknown component"
  or ship a missing island chunk, and `generateStaticParams` failed to evaluate a page whose
  islands import a library that does `import React from "react"`.
- Runtime-bundle regions are keyed on the query keys they read (they were keyed on the
  whole query, so any random value filled the cache), and the page and region caches use
  a segmented LRU: a client requesting hundreds of one-off `[param]` variants can no
  longer flush the pages that were requested twice (plain LRU still lost a hot page to
  512 requests). A request that has a query is rendered, not answered from the cache,
  while no render has shown what the page or region reads (after a restart the entry of
  the bare URL used to answer `?page=2`).

- An imported asset (`import logo from "./logo.png"`) now resolves to its URL through any
  chain of re-exports: `export { logo }`, `export default logo`,
  `export { default as logo } from "./logo.png"`, `export *`, `export * as ns` with `ns.logo`,
  and aliases. They used to render as the bare name (`src=logo`) and fail in the browser.
  KR7126 is kept for reads the rewrite cannot reach.
- Tailwind: functional `@utility name-*` blocks were dropped when the build merged the
  directives found in project CSS, so they never generated anything outside unit tests.
  A hyphen-free `@utility card` is no longer discarded by the class scanner as prose, and a
  class like `w-[red;}x{display:none]` can no longer break out of its rule.
- In production builds a keyed list (`items.map(...)` in a component with state) never
  updated after the first render, because the trimmed runtime dropped the list feature.
  Production builds ship every runtime feature a page's helper calls need, and a test
  checks that every runtime name the emitted code uses is defined by the trimmed runtime.
- A custom hook that takes an argument threw `krateParam is not defined` in production
  builds and stopped every component after it. One component that throws while hydrating
  no longer stops the rest of the page.
- Text, conditionals and lists directly inside `<Suspense>` were never bound; the slots
  of a conditional's inactive branch (`{user && <p>{user.name}</p>}`) no longer run while
  the branch is hidden, which logged a `TypeError` on every load.
- Destructuring with a function or runtime value (`const { fmt } = cfg`) declared every
  name as the whole object, and declared nothing in a page component. A module-level
  destructure read its source before it was declared and aborted hydration. Two
  `const [a] = useState(...)` without setters in one component redeclared
  `__krate_hole1`.
- A component that calls a function prop from an expression (`fmt={(s) => ...}`, render
  props, `children` as a function) rendered nothing and shipped no JavaScript.
  `{open && createPortal(...)}` failed with `createPortal is not defined`; a portal left
  as a call reports `KR8010`.
- Build-time evaluation: `JSON.stringify(value, null, 2)` indented nothing,
  `Number.MAX_SAFE_INTEGER` and `Math.SQRT2` rendered empty, built-in members the
  compiler lacks evaluated to `undefined` instead of staying unknown, `for (let ...)`
  closures shared one binding, and `[...Array(n)].map(...)` rendered one row.
  `Array.from({ length: n }, fn)` renders its rows in the static HTML.
- The HTML minifier collapsed white space in elements styled `white-space: pre*`.
- Build-time evaluation: `lastIndexOf` ignored its position argument (`"abcabc".lastIndexOf("b", 3)`
  gave 4, `[1, 2, 1].lastIndexOf(1, -2)` gave 2), and a standard member the interpreter lacks
  (`arr.splice`, `str.match`) read as `undefined`, so `typeof arr.splice` or `if (arr.toSpliced)`
  folded to the wrong branch; it stays unresolved now. A differential test checks the interpreter
  against Node for 250 expressions.
- Modules on a page shared one namespace, so two files with a private `up`, `Row` or
  `LABEL`, or two default exports named `Card`, showed one module's value for both. They
  are kept apart; `KR7116` is reported only for modules that cannot move (re-exported,
  imported as a namespace, `import()`ed, or "use client"/"use server"). An import from an
  alias like `@server/lib` near the top of a file no longer changes the tier of its
  components.
- `export { Card } from "./Card"` and `export { default as Card }` barrels never loaded
  the module; they resolve now, also through a tsconfig alias. A file beside a
  same-named directory (`Button.tsx` and `Button/`) resolved to the directory; the file
  wins, as in Node and tsc. An asset imported through an alias (`@/assets/logo.png`) was
  emitted but referenced as a bare identifier.
- Minified code with a string right after an identifier or `}` (`import{a}from"./b.js"`)
  and a JSX attribute named `extends` failed to parse. A binding named like a runtime
  name (`export const krateTheme`, `export function h`) lost its export or import name.
  A project component named `Link`, `Head`, `Script`, `Style`, `Icon`, `Image` or
  `Suspense` replaces the built-in. `function Box(p)` next to a module-level const failed
  the build, and `Table.Cell = function ...` is a component now.
- A page whose default export is `@runtime` (or `// @runtime`) rendered an empty region.
- The SSR renderer is supervised: after a crash every SSR/ISR page used to answer `200`
  with the empty build-time shell and ISR refresh stopped for good. It restarts, and pages
  answer `503` (`no-store`) while it is down. A region stream cut short is no longer a
  cacheable success. Sidecars no longer outlive a killed host.
- ISR caches evict the least recently used entry, background refresh re-renders only
  variants older than their window, and the first refresh waits one interval.
- A middleware or config rewrite to a page path without a trailing slash (`/feedback`)
  answered `301` and changed the URL; it is served in place.
- `node_modules/.bin/krate` was missing after `npm install @krate/core` on npm 10. The
  platform packages no longer declare `bin` (`check:release` enforces it); the fix reaches
  users with the next release of the platform packages. Until then `npx krate` works, as
  does `npm rebuild`. `@krate/core` no longer publishes a stray Linux compiler binary and
  the wrapper picks the platform package when installed from a registry; in
  `0.5.0-beta.1` a clean install failed on Linux with `spawn .../bin/krate EACCES`.
- `krate migrate nextjs` corrupted an `export default async function` page that has
  `generateMetadata` (it emitted `<async {...props} />`), and the generated metadata
  component was rejected by the renderer. A page that forwards `{...props}` lost its route
  params at build time, and an async `[slug]` page listed by `generateStaticParams`
  rendered with empty params.
- Build output and messages: a dev rebuild whose only page fails reports the failure
  instead of blaming `pagesDir`; `krate build` prints each failure once and keeps the
  diagnostic hint; esbuild's "the opening tag is here" note includes its position;
  `krate build ./typo` fails with "project directory ... does not exist" instead of
  creating the directory; `krate doctor` no longer calls an unset `output` a server build.
- Plugins could read and write outside the project through a symlink (`pluginapi`), and the
  MCP policy let `read_file` serve `.env` through a link or under another case (`.ENV`). The
  title of a page generated by a plugin route is escaped.
- A markdown autolink such as `<javascript:alert(1)>` was emitted as a link; it goes through
  the same scheme check as `[x](url)` and renders as text. The icon sanitizer also drops SMIL
  `set`/`animate` that target `href`, `<style>` that fetches external files, external
  `<image>`, `<iframe>`, `<object>` and `<embed>`.
- `<Image width={1000}>` (also 2000, 3000, 10000) failed with `KR8803`. Images are sized to
  what you ask for: `width={100}` ships 100w and 200w variants and `sizes="100px"` instead of
  six 640-2000w files. JPEGs with an EXIF orientation are rotated before resizing and GIFs
  work (animated ones are served as they are).
- Page-level and imported `.md`/`.mdx` ignored the `markdown` config; a list, quote or table
  directly inside a multi-line JSX element in MDX swallowed the closing tag. Frontmatter folds
  multi-line quoted strings, and a `+++` (TOML) or unterminated block warns. Hidden files such
  as editor lock files are skipped in collections. Short code blocks and tables no longer add
  a focus stop and a landmark each.
- `Tabs`, `Accordion` and other zero-JS components built from a `.map()` over a build-time
  array showed every panel and ignored `defaultValue`; the values are collected from the
  unrolled array. Islands from libraries written as `import React from "react"` (recharts,
  Headless UI) failed because the shared `preact/compat` chunk had no default export. Docs
  search snippets showed `&quot;` and `&#39;`.
- A tier decorator on a function or const wins over a same-named binding in the
  file (it used to leave an esbuild "Decorators are not valid here" error), and
  `@client export default <expression>` now explains how to name the component.
- An aliased import of a helper (`import { plain as pp } from "./x"` then
  `{pp()}`) rendered nothing and threw `pp is not defined` in the browser. The
  compiler no longer keeps path aliases in process-wide state, so builds of
  different projects in one process cannot see each other's tsconfig aliases.
- A code span that holds a longer backtick run (`` ` ```ts {1,3-5} ` ``) in
  an MDX page evaluated its `{1,3-5}` as an expression (it showed `-2`).
- The docs theme header put the GitHub and Twitter buttons edge to edge.
- An async page that sets `<title>` in `<Head>` served the title twice, and
  when the title's length differed from the baked one, the page body was spliced
  into the wrong place (inside a comment). Both the Go host and the portable
  handler are fixed; other `<Head>` children (`<meta>`) still render in place.
- A page that imports a module whose top-level code runs Redux Toolkit's
  `createSlice` or `configureStore` failed to build with "class declaration
  DraftMap inside a component or helper function" from immer. Library code under
  `node_modules` is no longer checked for client code generation.
- Attribute bindings and nested conditional tests in a hidden branch
  (`{user && <a href={user.url}>x</a>}`) no longer run while the branch is hidden.
- `import { default as logo }`, `import * as ns` (`ns.default`) and reads
  of an asset binding inside a class or parentheses did not resolve to the file's
  URL. An asset binding that stays unresolved, including one re-exported by a barrel
  module (`export { logo }`), now warns with KR7126 instead of failing silently
  in the browser.
- The Go host component render is cancelled with its request.
- An MDX parse error now points at the line and column of the offending tag
  or expression in the `.mdx` file instead of a position in generated code.
- A SPA navigation to a prefetched page dropped the link's `#fragment`
  (`<Link href="/docs/page#section">` landed at the top).
- `krate start` refused a site that has no root page (no top-level
  `index.html`) with "no build output found".
- A second `krate build` that replayed island pages from the disk cache wrote no
  island chunks and no Preact import map, so every island on a cached page was
  broken. The cache entry now carries the page's island entries.
- A `@server`, `@static` or `@runtime` component that uses `useState`, effects, class state or
  event handlers fails the build with `KR7110` (it used to build and ship a dead page),
  for `// @server`, `*.server.tsx`, decorators, config lists and class components.
- `useId()` in a child component no longer fails with `unresolved identifier
  __krate_useId`. Ids are unique per instance and the same in the static HTML and the
  hydration code, in children, lists (also rows added at runtime), conditional branches
  and layouts.
- `useEffectEvent` is lowered (it used to leave a bare call in client JS that threw).
- `<Activity>` and `<ViewTransition>` render their children instead of dropping them.
  `<SuspenseList>`, `cache`, `flushSync`, `useFormState`, the `react-dom` resource hints,
  `version` and `captureOwnerStack` no longer throw `x is not defined` in the browser or
  drop content.
- `@server` on `export default withX(Inner)` (and `memo(Inner)`) renders the component
  with the HOC applied, also when the HOC is declared in another module. A wrapper Krate
  cannot inline fails with `KR8030` instead of rendering nothing.
- Conflicting tiers fail with `KR7115`: `// @runtime` above `@server`, `Foo.server.tsx` with
  `@runtime`, a decorator against `serverComponents`/`runtimeDirs`, a `"@server"` body
  directive against a decorator. `@client` on one component of a `// @server` file still
  works. The rule is in Component Tiers.
- Imported components are classified against the file that declares them, and relative
  `serverDirs`/`runtimeDirs` entries match project-relative paths (they never matched).
- Custom elements no longer render `data="[object Object]"`: objects, arrays and functions
  are omitted from the HTML and set as properties on the client; `true` renders an empty
  attribute.

### Security

- A matcher-less `middleware` no longer skips URLs that merely end in a static extension.
  `/api/files/report.pdf` used to reach the API route without middleware; only a real static
  file skips it (Go host and portable handler).
- The canonical-slash redirect starts with `./`, so a last path segment like
  `ftp:3232235777` cannot redirect off-site.
- The Node, Bun and Deno API sidecar only answers the Krate host (it checks a per-process
  secret), so its loopback port cannot be used to skip middleware, CSRF and body limits.
- With `server.trustProxy`, the client address is the `X-Forwarded-For` entry
  `server.trustedProxies` hops (default 1) from the right, not the leftmost entry a client
  controls.
- The portable handler (Node target, Vercel, Netlify, Cloudflare) signs the flash cookie
  with HMAC-SHA256 through Web Crypto and never reads an unsigned or forged one. The key is
  `KRATE_FLASH_SECRET` (shared by every instance); without it the Node target uses a
  random per-process key and the serverless targets carry no flash state and log a
  warning. Go and TypeScript use one format, pinned by a shared test vector.
- Vercel, Netlify and Cloudflare follow `server.trustProxy` (it used to be hard-coded on
  for Vercel and Netlify). Not set means what each platform guarantees (Vercel's
  `x-forwarded-host`/`-proto`/`x-real-ip`, Netlify's `x-nf-client-connection-ip`,
  Cloudflare's `cf-connecting-ip`, the request URL elsewhere), `true` adds the forwarded
  headers of a proxy of yours, `false` trusts none; `x-forwarded-for`, `x-real-ip` and
  `forwarded` a client wrote never reach middleware or API routes. See Deployment.
- A Go region render is never served to another visitor: the cache key covers every value
  the component read of the request, or the whole request (headers like `Referer` and
  `X-Forwarded-For` included) when its reads cannot be recorded.
- The no-JS flash cookie is HMAC-signed, action cookies are `Secure` over https and
  whenever `SameSite=None`, CORS responses carry `Vary: Origin` for disallowed origins too,
  request paths are escaped in logs, request headers are capped at 64 KiB, and the Go API
  socket directory must be private to the user.

## 0.5.0-beta.1 (2026-11-04)

### Breaking

- tsconfig `paths` resolve exactly like `tsc`: targets are relative to `baseUrl`. The 0.4
  template (`"baseUrl": "./src"` with `"@/*": ["./src/*"]`) fails with `cannot resolve import
  "@/..."`; set `"baseUrl": "."`. A broken tsconfig is reported, and `extends` is followed.
- Tailwind output follows v4: oklch palette and the v4 font stack, renamed scale
  (`shadow-sm` is the old `shadow`, `shadow-xs` the old `shadow-sm`, likewise `blur`,
  `rounded`), `ring` is 1px `currentcolor`, `outline-none` removes the outline, and Preflight is
  the v4 one, on by default since alpha.1 (`tailwind.preflight: false` opts out). Output is layered
  (`@layer theme, base, components, utilities`): unlayered CSS beats Tailwind utilities, so
  a global reset such as `* { margin: 0; padding: 0 }` must move into `@layer base { ... }` or it
  overrides `p-*`/`m-*`.
- `krate dev` binds `127.0.0.1` (new `devServer.host`; `krate serve` still binds all interfaces).
- `krate start` only serves an existing `dist/`; `krate serve` builds and serves.
- Server-only output moved under `dist/.krate/`, including `manifest.json` (was
  `dist/manifest.json`). Static serving no longer lists directories or serves dotfiles.
- Unresolved imports, content schema errors and `<Image>` problems (`KR8801`..`KR8804`) fail
  the build with located errors; config values of the wrong type fail with the key and the
  expected type, and unknown keys warn.
- The production hydration runtime ships a core plus feature bundles; a build includes only the
  features its pages reference.

### Added

- Markdown renders with goldmark, with real MDX (ESM, inline JSX, expressions) and GFM alerts.
- A real CSS pipeline: `@import` with layers and cycles, CSS modules (keyframes, `composes`,
  `:global`), per-page CSS output, `@theme` and CSS-first Tailwind directives, `@apply`
  anywhere, container queries, and a candidate extractor for the whole file.
- `KRATE_PUBLIC_*` and `import.meta.env` constants are inlined and unexposed reads warn
  (`KR8310`); JSON, `?raw` and `?url` imports inline as constants; `KR8302` flags imported
  values that hydration code cannot see; `KR8810` flags collections shipped to the browser.
- No-JS server action posts carry state back in a flash cookie; `useActionState` keeps its
  state on redirect.
- Hydration source maps point at the `.tsx` handlers; the dev server reloads config and env,
  watches new directories and `public/`, and shows an error shell.
- Components: keyboard-operable overlays, ARIA roles, AA contrast, logical RTL properties,
  controlled overlays, `toast()`, higher-order components and store factories.
- SEO, `basePath`, fonts and search hotkey options; spec-shaped Web APIs in the embedded
  API/middleware runtime; Node `exports` semantics in the resolver.

### Fixed

- Several runtime fixes: update-depth errors reach the error boundary, effects re-track
  dependencies and report loops, `createMemo` is pull-based, `twMerge` groups derive from the
  utility. Tailwind candidates with apostrophes, regexes and template literals are found.
- Windows spawn in the npm wrapper and scaffolders, and a binary version check.
- Static serving: an open redirect and path handling were hardened.

## 0.5.0-alpha.4 (2026-10-28)

### Added

- Next.js migration (`krate migrate nextjs`) maps App Router routing, server components,
  client boundaries, cache/revalidation, and framework features.
- `@krate/runtime/cache` with `revalidatePath`, `revalidateTag` and `unstable_cache`, Next-style
  `revalidate`/`dynamic` segment exports, and an authenticated on-demand revalidation host.
- React islands: compiled React packages run as Preact islands, with React SSR.
- MCP server overhaul (M2-M5), the Simple docs theme, and the new SVG logo.

### Fixed

- Server, MCP, migration and runtime hardening, including ISR safety.

## 0.5.0-alpha.3 (2026-10-23)

### Added

- Server actions, with React 19 action hooks on the same transport.
- React parity: Suspense, `lazy`, `use()` and transitions, class lifecycles and error
  boundaries, keyed lists, React 18/19 events, forms and refs; a browser differential suite
  against React 19.
- Stable diagnostic codes (`KRxxxx`) with a registry, and a typed build-time interpreter for
  compile-time evaluation.
- A rewritten lexer and parser for ES2024 and JSX with source positions.

### Fixed

- Sidecar port collisions between concurrent servers, and a cluster bind retry on Windows.
- Minifier whitespace and comment handling; module-level `let`/`var` shared across components.

## 0.5.0-alpha.2 (2026-10-21)

- `tsx` is gone: `generateStaticParams`, API routes and the SSR renderer are bundled to
  JavaScript and run on plain Node.
- A lite router and a smaller runtime bundle.
- React compatibility R1 complete.

## 0.5.0-alpha.1 (2026-10-20)

- Precompressed assets (`--precompress`, gzip and brotli), router prefetch and reconcile
  batching, i18n, a config schema (`krate config --schema`) with unknown-key warnings.
- Faster builds: esbuild context reuse, image processing cache, a cached file resolver, an
  expression evaluation VM pool, CPU and heap profile flags.
- Tailwind Preflight on by default; no dev serve cache; MCP overhaul; React compatibility R1 beta.

## 0.4.0 (2026-10-16)

- Persistent build cache, CSS dead-code elimination and de-duplication, `krate inspect --json`,
  profiling, and a parallel page pipeline.
- `krate.config.ts` is evaluated in-process (esbuild plus QuickJS) instead of through `tsx`.
- Go runtime and server components with a unified Go API, a Bun API sidecar and Unix sockets
  for Go API routes, API timeouts and panic protection.
- CSS signals and CSS-only components, legacy browser support, and a docs theme with versioning,
  i18n and customisable search.
