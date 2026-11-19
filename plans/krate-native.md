# Krate Native - Plan

Status: design, not started. Last revised 2026-11-10.

Krate Native compiles a Krate app into a desktop application for Windows,
macOS and Linux. v1 is desktop with the full SDK and dev loop; v2 is mobile and
native widgets.

## 0. Summary of decisions

| Area | Decision |
| --- | --- |
| Model | System webview UI plus a native host in one process (release) |
| Platforms | Windows, macOS, Linux in v1 |
| Rendering | No SSR, ISR, streaming SSR, API routes, middleware or server actions in native builds. Every route is prerendered; dynamic segments fill in on the client |
| Host code | Go (`.go` files) and TypeScript marked `@host`, imported and called directly from UI code. No bridge, no registration, no command names |
| Host JS runtime | `quickjs` (default, in-process), `node`, `bun`, `deno`, `scriptc` (experimental) |
| Origin | Custom scheme served in-process; no TCP listener in release |
| Platform layer | Own thin layer, no cgo: WebView2 via COM, WKWebView via purego/objc, WebKitGTK via purego |
| Builds | Prebuilt shell for dev and TS-only apps (no toolchain); full compile for release |
| Bindings | Static analysis of Go and TS sources; generated, reflection-free glue |
| Transport | One HTTP-shaped channel on the app origin: calls, streams, signals, HMR |
| SDK | Full SDK in v1, including the updater |
| Signing | In v1: ad-hoc by default, sign and notarize commands |
| v2 | Mobile, native widgets, plugins, multiple webviews |

## 1. The model

A Krate Native app is one project. TSX is the UI. Code that needs the machine
(files, database, secrets, background work, OS features) is written in Go or in
TypeScript marked `@host`. UI code imports host code and calls it like any async
function. The compiler sees both sides of every import, decides where each piece
runs, generates the call plumbing and the types, and optimizes the calls. The UI
runs in the system webview; host code runs in the same native process.

## 2. Linked code

### Go

```go
// src/lib/tasks.go
package tasks

import "github.com/kratejs/krate/packages/native/krate"

type Task struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// Count is shared state; UI code that reads it re-renders when it changes.
var Count = krate.NewSignal(0)

func List(ctx krate.Ctx) ([]Task, error)                { /* SQLite in krate.AppDataDir() */ }
func Add(ctx krate.Ctx, title string) (Task, error)     { /* ...; Count.Update(+1) */ }
func Watch(ctx krate.Ctx) (<-chan Task, error)          { /* ... */ }
func Export(ctx krate.Ctx, path string) ([]byte, error) { /* ... */ }
```

### UI

```tsx
import { List, Add, Watch, Count } from "../lib/tasks.go";

export default async function Tasks() {
  const tasks = await List();                 // typed: Task[]
  return (
    <main>
      <p>{Count()} tasks</p>                  {/* host signal, live */}
      <button onClick={() => Add("Buy milk")}>Add</button>
    </main>
  );
}

for await (const t of Watch()) { /* <-chan Task is AsyncIterable<Task> */ }
```

### Rules

- Only exported Go functions and signals that UI code imports are callable from
  the webview. The attack surface is exactly what the UI uses.
- Signatures: optional first parameter `ctx krate.Ctx`; returns `(T, error)`,
  `error`, `T` or `(<-chan T, error)`. The TS signature drops `ctx` and adds an
  optional trailing `{ signal?: AbortSignal }`; aborting cancels the `ctx`.
- Type mapping:

  | Go | TS |
  | --- | --- |
  | structs (with `json` tags) | objects |
  | slices | arrays |
  | `map[string]T` | `Record<string, T>` |
  | `*T` | `T \| null` |
  | `time.Time` | `Date` |
  | `[]byte` | `Uint8Array` (raw, no base64) |
  | `<-chan T` | `AsyncIterable<T>` |
  | `krate.Signal[T]` | read-only signal |

  Interfaces, channels inside fields, and generics that do not resolve fail the
  build with a `KR` diagnostic and a hint.
- A Go `error` is thrown as `KrateError { code, message, data }`;
  `krate.Errorf(code, ...)` sets the code.
- Go names stay as written in TS (`List`), matching `.go` component imports.
- Host signals are read-only in the UI and change only through host code, so
  every window sees one source of truth.

### What the compiler does with both sides visible

1. Placement from the import graph: `.go` files and `@host` TypeScript run in
   the host, everything else in the webview.
2. Static analysis (extending `internal/gotsgen` and `internal/gohost/scan.go`)
   generates a Go dispatch table and per-type encoders and decoders on both
   sides. No `reflect` and no generic JSON walking at runtime.
3. Calls made in the same tick are batched into one request; identical reads in
   flight are shared. Both integrate with Suspense.
4. A function marked `//krate:build` (Go) or `@build` (TS) runs during the build
   and its result is baked into the prerendered HTML. Opt-in, because side
   effects cannot be proven absent.
5. Host signals are pushed only to windows whose loaded code reads them.
6. The compiler knows which SDK calls are reachable. It derives the permission
   set from that and, in release builds, links only those SDK packages.

## 3. Host TypeScript: `@host`

Host TypeScript is marked the same six ways Krate marks component tiers
(`core-concepts/component-tiers.md`), with the same precedence and the same rule
that sources must agree. `@host` marks functions and modules, not components.

| Precedence | Form | Example |
| --- | --- | --- |
| 1 | Decorator on a declaration | `@host async function addTask(title: string) { ... }` |
| 1 | String at the top of a function body | `async function addTask(t) { "@host"; ... }` |
| 2 | Directive on the first non-comment line (whole file) | `// @host` |
| 3 | File convention | `*.host.ts`, `*.host.tsx` |
| 4 | Config name or path list | `hostFunctions: ["syncNow", "src/lib/sync.ts"]` |
| 5 | Directory membership | `hostDirs: ["src/host"]` |
| always | Go source | every `.go` file is host code |

`hostFunctions` and `hostDirs` are top-level keys next to `runtimeComponents` and
`runtimeDirs`, registered in `internal/config/validate.go`.

Rules:

- Sources that disagree fail the build (the KR7115 pattern, with a new code for
  host conflicts).
- A decorator on one function overrides the file for that function alone, so a
  host function can sit inside a page:

  ```tsx
  // src/pages/index.tsx
  import { db } from "@krate/app/db";

  @host
  async function addTask(title: string) {
    return db.run("insert into tasks(title) values (?)", title);
  }

  export default function Home() {
    return <button onClick={() => addTask("Buy milk")}>Add</button>;
  }
  ```

  The compiler moves `addTask` and its imports into the host bundle and drops
  them from the UI bundle when the UI does not use them itself.
- An inline `@host` function may use only its parameters, module-level imports
  and other host code. Capturing component state or a signal fails the build
  with a hint to pass the value as an argument.
- `@host` on a component fails: host code does not render. The hint points to
  `@server` (build-time) or a client component.
- Only host code the UI imports is callable.
- The UI sees host functions as async (`count(): number` becomes
  `() => Promise<number>`); `async function*` becomes `AsyncIterable`.
- Values crossing the boundary must be data: JSON values, `Date`,
  `Uint8Array`, `Map`, `Set`. Functions, class instances and DOM nodes fail the
  build, naming the parameter.
- `export const count = hostSignal(0)` (from `@krate/app/host`) is the TS twin
  of `krate.NewSignal`.
- In a web build `@host` fails with a diagnostic in v1. Mapping `@host` to
  server actions on the web is a later option for shared codebases.

## 4. Host JS runtime

```ts
native: {
  host: {
    runtime: "quickjs",   // "quickjs" | "node" | "bun" | "deno" | "scriptc"
    version: "22",        // node/bun/deno: version to bundle
    bundle: true,         // ship the runtime in the app; false uses PATH (dev only)
  },
  experimental: { scriptc: false },
}
```

| Runtime | Where it runs | npm packages | Size cost | Dev | Release |
| --- | --- | --- | --- | --- | --- |
| quickjs (default) | In the app process | Pure-JS packages plus Krate host APIs | A few MB, already in the prebuilt shell | Modules reload in place | In the prebuilt shell or the full compile; no toolchain |
| node | Child process | All, including native addons | Tens of MB | `node` on PATH; child restarts on edit | Node single-executable app; native addons need a build per OS |
| bun | Child process | Nearly all | Tens of MB | `bun` on PATH | `bun build --compile --target=...` (cross-compiles) |
| deno | Child process | Through `npm:` | Tens of MB | `deno` on PATH | `deno compile --target=...` (cross-compiles); `native.permissions` maps to `--allow-*` |
| scriptc (experimental) | Child process, native executable with no JS engine | What scriptc can compile | Smallest child option | Runs on QuickJS for fast HMR; `krate native check --scriptc` wraps `scriptc coverage --fail-on` | scriptc build; clang on macOS and glibc Linux, zig on Windows, musl and cross-compiles |

- Child runtimes speak the same pipe protocol as the Go dev host (section 10).
- On QuickJS, `@krate/app/os` calls from host code go straight into the Go SDK;
  on child runtimes they go back over the pipe. The import is the same.
- `runtime` is the app-wide default. `native.host.islands` overrides it per
  package or path, for example `{ "sharp": "node" }`. Only that module runs in a
  node child; the rest stays on the default runtime. Go host code always runs
  in the app process.
- The QuickJS host gets a persistent event loop. One goroutine owns the VM,
  with a timer heap and a job queue that I/O goroutines post completions to.
- Unsupported APIs are reported at build time. Each QuickJS host bundle is
  scanned against the embedded runtime's compatibility ledger, and each miss
  becomes a `KR` diagnostic with a hint (Go host function or island).
- scriptc is behind `native.experimental.scriptc`. It has no runtime fallback:
  code it rejects fails the build as a `KR` diagnostic at the original file and
  line. Its library mode emits only static archives without async support, so
  it always runs as a child executable. CI runs host tests on QuickJS and
  scriptc from one shared corpus, because dev and release use different engines.
- Source: https://github.com/vercel-labs/scriptc. See
  `plans/scriptc-quickjs-evaluation.md` for how it uses QuickJS and what Krate
  can take from it.

## 5. Easier than Electron or Tauri

| Task | Electron | Tauri v2 | Krate Native |
| --- | --- | --- | --- |
| Call backend code from the UI | `ipcMain.handle` + preload `contextBridge` + `ipcRenderer.invoke("channel")` | `#[tauri::command]` + `generate_handler!` + `invoke("name", {...})` | Import the function and call it |
| Types across the boundary | By hand | By hand or an add-on (tauri-specta) | Automatic |
| Backend language | JS on Node | Rust | TS (5 runtimes) or Go, mixed |
| Permissions | You build them | Capability JSON files | Derived from imports; config narrows |
| Shared state across windows | Hand-wired IPC events | Events | `hostSignal` / `krate.NewSignal` |
| Tools for a JS-only app | Node + Electron | Rust + C toolchain | `npm i` |
| Engine shipped | Chromium + Node | System webview | System webview (+ chosen runtime) |

TypeScript guarantees:

1. A TS-only app needs only `npm i` (prebuilt shell + QuickJS). `krate native
   init` creates a TS-only app; `--go` adds a Go host.
2. UI and host code can share a file through inline `@host`.
3. `@krate/app/os` behaves the same in UI and host code.
4. `@krate/app/db` (SQLite over pure-Go `modernc.org/sqlite`) and
   `@krate/app/kv` work on every runtime, so local-data apps need no native addon.
5. A throw in host code shows in the dev overlay at its file and line, with a
   source-mapped stack.
6. Tests mock by module: `mockHost("./lib/tasks.host.ts", {...})`, same for
   `.go` modules.

## 6. Pages without SSR

- Every route is prerendered. Go components work at build time only.
- Dynamic segments (`[id]`) get one prerendered shell per route; the client
  router fills in params. `generateStaticParams` is optional.
- Data comes from host calls or from `//krate:build` / `@build` functions.
- Runtime SSR, ISR, streaming SSR, `src/api`, middleware, `@runtime`
  components and server actions fail the native build with a `KR` diagnostic each.
- Because there is no request-time server, the native host does not reuse
  `internal/build`'s `serve()`. The in-process runtime is a small new package.

## 7. Runtime architecture

```
one native process (release)
  platform thread (main)            host (Go)
    windows, menus, tray,     <->     user Go packages + generated dispatch
    dialogs, notifications            host signals hub
    webview(s)                        JS host runtime (QuickJS in-process,
        |                               or a child process)
        |                             krate SDK
        |  custom-scheme requests (in-process, no TCP)
        v
  scheme handler
    GET  /...                  embedded dist (pages, assets, runtime)
    POST /__krate/call         batched host calls (generated codecs)
    GET  /__krate/stream/<id>  streams, host signals, events
    GET  /__krate/hmr          dev only
```

| OS | Origin | Mechanism |
| --- | --- | --- |
| macOS | `krate://app` | `WKURLSchemeHandler` |
| Windows | `https://app.localhost` | WebView2 `WebResourceRequested` on a virtual host |
| Linux | `krate://app` | `webkit_web_context_register_uri_scheme`, registered secure and CORS-enabled |

App code reads the origin from `app.origin`. The stable origin keeps
localStorage, IndexedDB and cookies across launches.

Every SDK operation is a serializable message to the platform thread. Native UI
calls run on the main thread anyway, and this is what lets dev split into two
processes.

## 8. Platform layer

| OS | Webview | Binding |
| --- | --- | --- |
| Windows | WebView2 (preinstalled on Win 10/11; bootstrapper for older) | Pure Go COM over `syscall` |
| macOS | WKWebView | `purego` + objc |
| Linux | WebKitGTK (`webkit2gtk-4.1` first, `webkitgtk-6.0` detected) | `purego` `dlopen` of GTK and WebKit |

- Linux tray and notifications go over D-Bus in pure Go (StatusNotifierItem,
  `org.freedesktop.Notifications`).
- No cgo: the shell cross-compiles from any OS, the compiler stays
  `CGO_ENABLED=0`, and users never need a C compiler.
- Platform-dependent SDK calls have `krate.Supports(cap)` and return
  `ErrUnsupported`. Known Wayland limits: global shortcuts only through the
  xdg-desktop-portal, no window positioning, no always-on-top. Documented in the
  compatibility matrix.

## 9. Build modes

| | Prebuilt shell (dev; default for TS-only apps) | Full compile (`krate native build --release`) |
| --- | --- | --- |
| Toolchain | None; shell ships in `@krate/core-<platform>` | Go only; cross-compiles all targets |
| Contents | Full SDK + QuickJS | Only reachable SDK packages (generated build tags); QuickJS only if host JS uses it |
| Assets | Next to the binary (`.app/Contents/Resources`, beside the `.exe`) | `go:embed`, served from memory |
| User Go code | Dev host child process | Linked into one process |
| Optimizations | None | Generated codecs, dead SDK removed, `-trimpath -s -w`, PGO from `krate native profile` |

`native.release.compile: "auto"` compiles when Go is found and otherwise falls
back to the prebuilt shell with a warning; `"always"` makes a missing Go an
error. Apps with Go code always need the Go toolchain.

## 10. Dev loop (`krate native dev`)

- Two processes in dev, one in release. The prebuilt shell owns the windows;
  user Go code and generated glue run in a dev host child over a pipe. A Go edit
  rebuilds and restarts only the child, so the window, scroll and UI state
  survive; host signals resend current values on reconnect.
- JS host: QuickJS re-evaluates changed modules in place; node, bun and deno
  restart their child.
- UI HMR reuses `internal/build/devhub.go`, delivered over the streamed
  `/__krate/hmr` response.
- `krate native dev --browser` serves the UI to a normal browser, with the host
  on a loopback port and a per-session token (dev only), for Playwright and
  browser devtools.
- Inspector overlay: host calls (arguments, timing, errors), signal traffic,
  permission denials, stream lifecycles.
- `@krate/app/testing` mocks `.go` and `@host` modules per test.
- CI smoke tests run against `--release` builds, since dev and release differ by
  a process boundary.

## 11. SDK (v1)

Go: `github.com/kratejs/krate/packages/native/...`. TS: `@krate/app/os`,
generated from the same definitions.

| Area | Contents |
| --- | --- |
| `app` | Lifecycle (`src/app.go` `Setup(app)` or `src/app.host.ts`), single instance, deep links, `origin`, quit and relaunch |
| `window` | Multiple windows (each opens a route), frameless and custom chrome, transparency, always-on-top, saved state, fullscreen, per-window permissions |
| UI and input | `menu` (app, context, dock), `tray`, `shortcut` (global, local), `dialog`, `clipboard` (text, image, files), `notify` (actions, progress) |
| `os` | App directories, secrets (Keychain, DPAPI, Secret Service), system info, `openExternal`, power and idle events |
| `fs` | Scoped files: app directories plus paths picked in a dialog |
| `db`, `kv` | SQLite and a key-value store, pure Go |
| `net` | HTTP client that can attach stored secrets; authenticated remote calls are host functions |
| `protocol` | URL scheme registration, OAuth deep-link round trips |
| `updater` | Signed delta and full updates: appcast on macOS, MSIX or custom on Windows, AppImage zsync or package-manager notes on Linux |
| `permission` | Queries and structured denials |

## 12. Security

- No network listener in release. Navigation locked to the app origin; external
  links through `openExternal`; strict default CSP.
- Host code is trusted. The UI reaches only host code it imports.
- Permissions are derived from reachable SDK calls and written to the build
  report. Config `permissions` is an optional allowlist; per-window scopes
  narrow it. A reachable capability that is not allowed fails the build.
- Secrets: the UI can set and delete them; reading a value into the webview
  needs `os:secrets.read`. Reading in host code is the recommended path.
- Signing: ad-hoc by default; `krate native sign` (Developer ID, Authenticode)
  and `krate native notarize`.

## 13. Config

```ts
// krate.config.ts
hostFunctions: ["syncNow"],
hostDirs: ["src/host"],
native: {
  appId: "com.acme.app",
  productName: "Acme",
  version: "1.0.0",
  platforms: ["windows", "macos", "linux"],
  icons: "./assets/icon.png",
  window: { width: 1280, height: 800, resizable: true, titleBar: "native" },
  permissions: ["dialog", "clipboard", "notify", "tray", "db", "shell:openExternal"],
  deepLinks: ["acme"],
  host: { runtime: "quickjs", bundle: true },
  experimental: { scriptc: false },
  release: { compile: "auto", pgo: "./native.pgo" },
  bundle: {
    macos: { category: "public.app-category.productivity" },
    windows: { publisher: "Acme", installer: "msix" },
    linux: { formats: ["deb", "rpm", "appimage"] },
  },
  updater: { url: "https://acme.com/updates/{target}.json", publicKey: "..." },
}
```

Menus, tray, extra windows, shortcuts and lifecycle are code.

## 14. Repo layout

| Path | Role |
| --- | --- |
| `packages/compiler/internal/native/` | Placement (six `@host` sources), inline-`@host` split, boundary type checks, glue and codec generation, capability analysis, runtime adapters (node, bun, deno, scriptc), release builds, packaging, signing, doctor |
| `packages/compiler/internal/config/` | `native` block, `hostFunctions`, `hostDirs` |
| `packages/compiler/cmd/krate/` | `krate native init / dev / build / profile / sign / notarize / check / doctor` |
| `packages/native/` | Go module: `krate` core (`Ctx`, `Signal`, `Errorf`), scheme runtime, platform layer, SDK, `hostjs` (QuickJS hosting, child protocol), shell `main` |
| `packages/app/` | `@krate/app` with `/os`, `/host`, `/db`, `/kv`, `/testing` |
| `@krate/core-<platform>` | Adds the prebuilt shell |
| project `.krate/native/` | Generated glue, dev-host module, `.d.go.ts` for function imports |

Users depend on `github.com/kratejs/krate/packages/native`, tagged
`packages/native/vX.Y.Z`. `replace` is only for the monorepo.

## 15. v1 milestones

| Milestone | Contents | Exit check |
| --- | --- | --- |
| M1 Platform spike | Window plus custom scheme on all 3 OSes; streamed responses; POST bodies; main-thread message queue; TS-only app on the prebuilt shell | `examples` launches on Windows, macOS and Linux from a shell built on Linux |
| M2 Linked code | `.go` function and signal imports; `@host` markers and conflict diagnostics; inline split; boundary checks; generated dispatch and codecs; errors, cancellation, streams, batching; `//krate:build` / `@build` (bare VM, validated output); QuickJS host with persistent event loop and ArrayBuffer transfer (no base64); build-time unsupported-API scan; `hostSignal`; `db`, `kv`; native-target diagnostics | Task-manager example with no hand-written plumbing |
| M3 Dev loop | Two-process dev; Go restart keeps the window; UI HMR; host JS reload; child restarts for node/bun/deno; browser mode; inspector; testing mocks; source-mapped host errors | Go edit keeps the window; TSX edit patches in place |
| M4 Full SDK | Section 11 except the updater; derived permissions; per-window scopes; capability report in `krate check` | SDK conformance suite on all 3 OSes in CI |
| M5 Release | `--release`, capability shaking, PGO, packaging (`.app`/`.dmg`, MSIX or NSIS, `.deb`/`.rpm`/AppImage), sign and notarize, updater, single-executable bundling for node/bun/deno, scriptc behind the flag, CI matrix with host tests per runtime, docs and compat-matrix rows | Signed builds install and update on clean VMs |

## 16. Acceptance criteria

- `krate native build examples` and `krate native build packages/web` produce
  installable, signed apps for all 3 OSes; a TS-only build needs no Go.
- Host calls, streams and signals need zero hand-written glue. Unsupported types
  and native-unsupported features fail the build with a `KR` code.
- Go edits in dev keep the window and UI state.
- Cold start, idle RSS and binary size are recorded per OS in M1 and enforced as
  CI budgets in M5.

## 17. Risks

- purego callbacks from GTK and WebKit on Linux are the least proven part;
  verify first in M1.
- WebKitGTK version spread: reading custom-scheme request bodies needs a recent
  WebKitGTK (believed 2.40+); pin a minimum.
- Wayland capability gaps.
- Dev/release asymmetry from the process boundary (release smoke tests).
- Codec coverage of real-world Go types: start strict, widen.
- QuickJS speed for host JS: document and budget.
- Node native addons cannot be cross-compiled.
- `modernc.org/quickjs` has no ArrayBuffer API; raw bytes need two wrapper
  functions over `modernc.org/libquickjs` (M2 spike).
- node, bun and deno add tens of MB and a second process.
- scriptc is young: rejected code and behavior that differs from QuickJS. The
  flag and dual-engine tests contain it.
- Scope: v1 is large. M1 and M2 alone prove the core idea; treat them as an
  internal checkpoint.

## 18. v2 and later

- Mobile: Go as a library (`aar`, `xcframework`) with the same linked-code model.
- Native widgets (IR-to-widget emitter) and `classToNative` styling.
- Plugins, multiple webviews and split panes, store submission.
- `@host` on the web, mapped to server actions.
