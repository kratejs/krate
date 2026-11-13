---
title: Getting Started with Krate Native
order: 2
description: Create a Krate Native app with krate native init, build its app directory with krate native build, and know how pages, dynamic routes and host calls behave.
---

# Getting Started

> **Experimental.** Part of [Krate Native](/docs/native/), which is under construction.

## Create an app

```sh
krate native init my-app          # a TypeScript host function and a page
krate native init my-app --go     # adds go.mod and src/lib/tasks.go
cd my-app
npm install
```

`init` writes `krate.config.ts` (with a `native` block), `package.json`,
`tsconfig.json`, `src/pages/index.tsx` and `src/lib/greet.host.ts`. It refuses to
overwrite an existing file (`KR7205`). With `--go` it also writes a `go.mod`, and
`src/lib/tasks.go` with a host function and a host signal. Outside the Krate
repository the `go.mod` does not require the host module yet; run
`go get github.com/kratejs/krate/packages/native`.

The UI needs two npm packages next to `@krate/core`: `@krate/app` (the host call
client, imported by the generated stubs) and `@krate/runtime`.

## Build

```sh
krate native build [dir] [--out dist-native] [--target linux/arm64] [--shell <path>] [--release]
```

`krate native build` produces an **app directory**, `dist-native/app/`, that the
prebuilt shell runs:

```sh
krate-shell dist-native/app
```

When a shell is available without building it (`--shell`, `$KRATE_SHELL`, or the
`krate-shell` that the `@krate/core-<platform>` package puts next to `krate`),
the build also writes a runnable distributable, no Go toolchain needed:

```
dist-native/linux-amd64/
  My App          the shell, renamed to the product name (My App.exe on Windows)
  app/            a copy of the app directory
```

Run `dist-native/linux-amd64/My\ App` directly, or hand the directory to
`krate native package`. Check the machine with `krate native doctor`.

The build

1. loads the config and runs the same checks as `krate native check`; any error
   stops the build, and nothing is written;
2. splits host code from the UI: TypeScript host code becomes `host/host.js`, Go
   host code gets generated glue and becomes `gohost/krate-gohost`;
3. prerenders every route into `dist/`;
4. works out the SDK permissions the code needs ([derived permissions](/docs/native/release/#derived-permissions)) and
   writes [`app.json`](/docs/native/app-json/) with them.

| Flag | Meaning |
|------|---------|
| `--out <dir>` | Output directory; the app is `<dir>/app`. Default `dist-native`. |
| `--target <os>/<arch>` | Platform of the Go host: `linux`, `darwin` (or `macos`), `windows`, with `amd64` or `arm64`. Default: this machine. A different platform builds with `CGO_ENABLED=0`. |
| `--release` | Link one executable per `--target` instead (comma-separated or repeated): `dist-native/<os>-<arch>/<productName>`. See [Release builds](/docs/native/release/). |
| `--shell <path>` | Copy a shell binary to `<dir>/krate-shell`. `--shell auto` looks for `$KRATE_SHELL`, then the `krate-shell` next to the `krate` executable, then builds `packages/native/cmd/krate-shell` when run inside the Krate repository. A shell that cannot be found is a note, not an error. |

## Check the machine

```sh
krate native doctor [dir] [--shell <path>] [--json]
```

`doctor` reads the config and reports what the app needs and what is there:

- the **Go toolchain**: required only for Go host code, `native.release.compile:
  "always"`, and to link a single-file release;
- the **shell binary** and where it was found (see
  [Finding the shell](/docs/native/dev/#finding-the-shell));
- the **system webview**, asked of the shell itself (`krate-shell --doctor`
  prints it as JSON): on Linux the WebKitGTK 4.1 version (2.40 or newer) with
  the library paths and whether a display is reachable; on macOS the OS version
  (WKWebView is part of the system); on Windows the WebView2 Runtime version
  from the registry;
- the **packaging and signing tools** for this OS (`appimagetool`, `dpkg-deb`,
  `rpm`, `makensis`, `hdiutil`, `codesign`, `xcrun` for notarytool, `signtool`
  or `osslsigncode`). A tool is required only when the config picks the format
  that needs it on its home OS; the rest are listed as optional;
- the **host runtime** the config selects: `node`, `bun` or `deno` with their
  version against `native.host.version`, or `scriptc` with its linker.

```
Krate Native doctor (linux/amd64)

Toolchain
  ok       Go               go1.26.1 (/usr/local/go/bin/go); ...

Shell
  ok       krate-shell      .../node_modules/@krate/core-linux-x64/bin/krate-shell (next to the krate executable)

Webview
  ok       WebKitGTK 4.1    2.54.0 (needs 2.40 or newer); OS Debian GNU/Linux 13 (trixie)
           /usr/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0.24.3
  ok       display          x11 (:0)

Packaging and signing
  MISSING  appimagetool     not found on PATH; builds .AppImage (native.bundle.linux.formats)
  -        rpm              not found on PATH; optional: .rpm files are written without it
```

`doctor` exits 1 when a required item is `MISSING`. A missing display is only a
warning: builds and `--browser` mode work without one. `--json` prints the same
items for scripts.

## Pages in a native app

Every route is prerendered. There is no server, so SSR, ISR, streaming, API
routes, middleware, runtime components and server actions are errors
([Host code](/docs/native/host-code/#native-target-restrictions)).

### Calling host code

Import host code and call it. Host functions are asynchronous: call them from
event handlers and effects.

```tsx
import { createSignal } from "@krate/runtime";
import { Add, Count } from "../lib/tasks.go";
import { greet } from "../lib/greet.host";

export default function Home() {
  const [msg, setMsg] = createSignal("");
  return (
    <main>
      <p>Open tasks: {Count()}</p>
      <button onClick={() => Add("write docs")}>Add</button>
      <button onClick={async () => setMsg(await greet("world"))}>Greet</button>
      <p>{msg()}</p>
    </main>
  );
}
```

**Host calls never run at build time.** The generated stubs are lazy: when the
compiler prerenders a page, a host function call or a host signal read yields no
value, and the page is rendered with an empty slot where the value would go. The
slot is filled in the webview. A page that reads a host signal (`Count()`)
always ships client code, even if it has no event handler, so the slot is never
left empty at run time. Do not use the result of a host call to decide the
structure of the page at build time (a top-level `await List()` is a build
error, `KR7102`); keep it in a signal that an effect or handler sets.

**Host signals are reactive.** The first host access on a page installs a host
client built on the Krate runtime's `createSignal`, so `Count()` in JSX or in an
effect is tracked and updates when the host pushes a new value.

### Dynamic routes

A page such as `src/pages/tasks/[id].tsx` is prerendered once, as a **shell**
(`tasks/[id]/index.html`), and listed in `app.json` under `fallbacks`. The shell
serves every URL that matches the pattern and has no file of its own, so
`/tasks/1` and `/tasks/anything` show the same page. In the webview, a small
inline script matches the address bar against the route and fills in the
parameters: `useParams()` works as on the web, and text, attributes and the
title that the page built from a parameter are replaced before the page
hydrates, also after a soft navigation.

With `generateStaticParams`, those parameters are prerendered as their own files
and the shell is still written for all other values. Set
`export const dynamicParams = false` to close the set: no shell and no
fallback, only the listed parameters.

### Navigation and view transitions

Use `<Link>` for navigation inside the app: it is a soft navigation that keeps
the window and the host connections, as on the web. A plain `<a href>` is a full
page load. A native build turns View Transitions off, whatever the top-level
`viewTransitions` says (a site can opt in for the web and stay safe on the
desktop), because a cross-document transition aborts the WebKitGTK web process
in software-rendered setups. Set `native.viewTransitions: "auto"` to keep them.

### Developer tools

The webview's inspector is on in `krate native dev` and off in a release build.
Set `native: { debug: true }` to keep it in a release build (to look at a bug on
a user's machine, for example). F12 or Ctrl+Shift+I opens it (Cmd+Option+I on
macOS); see [Developer tools](/docs/native/dev/#developer-tools).

## Footprint (Linux, measured)

Measured with the `examples/native-tasks` app on Linux x86-64 (WebKitGTK 2.54,
software rendering under Xvfb, Go 1.26.1, `CGO_ENABLED=0`, no `-ldflags`) by the
end-to-end test (`KRATE_NATIVE_GUI=1 go test ./internal/build -run TestNativeE2EGUI -v`).
Treat the numbers as an order of magnitude; they move with the machine and the
WebKitGTK build.

| What | Size |
|------|------|
| Cold start, process start to the window being open | about 0.2 s |
| Cold start, process start to the first page's load event | about 0.37 s |
| `krate-shell` binary | 15.6 MB (10.9 MB with `-ldflags="-s -w"`) |
| `dist-native/app` | 9.0 MB: Go host 8.9 MB, UI 124 KB (42 KB of JS gzipped), host JS 4 KB |

Memory is measured with `krate native profile --memory` (it needs a display and
the system webview; see the [release page](/docs/native/release/#measuring-memory)).
Summing the resident size of every process counts the shared WebKit libraries once
per process, so it overstates the footprint. The table uses two numbers per process:
**PSS** (private pages plus an equal share of each shared page; PSS values add up to
the real total) and **private dirty** (written pages only this process holds, the
memory that cannot be dropped without swapping). The **floor** is a blank one-line
page in the same shell and window size with no host code: it is what the system
webview costs before your app does anything, so *app minus floor* is the part
Krate and your app can change.

Release executable of `examples/native-tasks`, 30 s after the page loaded, MB as
PSS / private dirty:

| Process | App | Floor | App minus floor |
|---------|-----|-------|-----------------|
| `krate-shell` (the Go host is linked into it) | 51 / 25 | 53 / 24 | -3 / +1 |
| WebKit web process | 54 / 21 | 49 / 17 | +5 / +4 |
| WebKit network process | 20 / 13 | 21 / 12 | -1 / +2 |
| **Total** | **124 / 60** | **124 / 53** | **+1 / +7** |

Right after the load the app is a few MB higher (the page's JavaScript and the Go
heap have not been collected yet): about +15 / +19 for the total. Runs differ by a
few MB, and the floor moves with the WebKitGTK build, so compare the last column.
With the Go host as a separate child (the prebuilt-shell layout) add about 10 / 5 for
that process.

