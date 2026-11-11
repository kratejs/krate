---
title: Getting Started
order: 2
description: Create a Krate Native app with krate native init, build its app directory with krate native build, and know how pages, dynamic routes and host calls behave.
---

# Getting Started

> **Preview.** Part of [Krate Native](/docs/native/), which is under construction.

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
krate native build [dir] [--out dist-native] [--target linux/arm64] [--shell <path>]
```

`krate native build` produces an **app directory**, `dist-native/app/`, that the
prebuilt shell runs:

```sh
krate-shell dist-native/app
```

The build

1. loads the config and runs the same checks as `krate native check`; any error
   stops the build, and nothing is written;
2. splits host code from the UI: TypeScript host code becomes `host/host.js`, Go
   host code gets generated glue and becomes `gohost/krate-gohost`;
3. prerenders every route into `dist/`;
4. writes [`app.json`](/docs/native/app-json/).

| Flag | Meaning |
|------|---------|
| `--out <dir>` | Output directory; the app is `<dir>/app`. Default `dist-native`. |
| `--target <os>/<arch>` | Platform of the Go host: `linux`, `darwin` (or `macos`), `windows`, with `amd64` or `arm64`. Default: this machine. A different platform builds with `CGO_ENABLED=0`. |
| `--shell <path>` | Copy a shell binary to `<dir>/krate-shell`. `--shell auto`, or the `KRATE_SHELL` environment variable, uses `$KRATE_SHELL` or builds `packages/native/cmd/krate-shell` when run inside the Krate repository. A shell that cannot be found is a note, not an error. |

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
page load. A native build turns View Transitions off unless `krate.config.ts`
sets `viewTransitions` itself, because a cross-document transition aborts the
WebKitGTK web process in software-rendered setups.

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
| Resident memory, `krate-shell` process after the page ran | about 105 MB |
| Resident memory, all processes | about 315 MB: shell 105, WebKit web process 137, WebKit network process 64, Go host 7 |

The shell's resident memory includes the WebKitGTK libraries it maps. The web and
network processes belong to the system webview; the Go host is a separate child
process.
