# Changelog

Krate uses [GitHub Releases](https://github.com/kratejs/krate/releases) for the
detailed changelog. This file records the release train at a glance: each release
lists what breaks first, then what is new, fixed and security-relevant. Upgrading
from 0.4? Read [Upgrading](https://krate.js.org/docs/guides/upgrading/#from-04-to-05).

## Unreleased

### Krate Native

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
- Krate Native rows in the compatibility matrix (Linux verified, macOS and Windows untested) and
  size, cold-start and memory budgets in `packages/native/budgets.json`.
- `krate native check` and native builds warn about oversized images, large inlined `data:` URLs,
  web fonts, compositing CSS effects and long lists without windowing (KR7270 to KR7274; thresholds
  and a silence list in `native.lean`). Native builds no longer preload lazy chunks, ship no
  script or config block on pages without client code, and write no `.br`/`.gz` siblings.
  `krate native dev` has a Memory panel (PSS, private dirty, DOM nodes, JS heap where available).
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

### Added

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

### Krate Native (preview)

Krate Native compiles a Krate app into a desktop app: the TSX UI runs in the system webview and
host code runs in the native process. Names and options may change before release.

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
