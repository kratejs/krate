---
title: Host Runtimes
order: 5
description: Run TypeScript host code on Node, Bun, Deno or scriptc instead of the embedded QuickJS runtime - islands, release bundling and Deno permissions.
---

# Host Runtimes

> **Preview.** Part of [Krate Native](/docs/native/), which is under construction.
> `scriptc` is experimental.

TypeScript [host code](/docs/native/host-code/) runs on an **embedded QuickJS**
runtime inside the shell by default. It starts fast and ships nothing extra, but
it has no Node built-in modules. When host code needs `node:fs`, a package with
Node-only dependencies, or a full engine, choose another runtime:

```typescript
// krate.config.ts
export default defineConfig({
  native: {
    host: {
      runtime: "quickjs", // "quickjs" (default) | "node" | "bun" | "deno" | "scriptc"
      version: "22",      // node, bun and deno: the version a release bundles
      bundle: true,       // ship the runtime with the app; false uses PATH (dev only)
      islands: { sharp: "node" },
    },
    experimental: { scriptc: false },
  },
});
```

| Runtime | Where host code runs | Release build |
|---------|----------------------|---------------|
| `quickjs` | In the shell process | Nothing to ship |
| `node` | A Node child process | A [Node single executable application](https://nodejs.org/api/single-executable-applications.html) |
| `bun` | A Bun child process | `bun build --compile` |
| `deno` | A Deno child process | `deno compile` |
| `scriptc` | QuickJS in development; a native executable with no JavaScript engine in a release | `scriptc build` |

Go host code does not depend on this setting: it runs in the shell process in a
release and in a Go child process in development.

## Islands

`native.host.islands` runs part of the host code on a different runtime and
leaves the rest on the default one. A key is a **package name** (`sharp`,
`@scope/name`) or a **path** relative to the project root (`src/native/**`,
`src/lib/img.host.ts`; `*` stays inside one directory, `**` crosses
directories, a plain directory matches everything below it). The value is
`node`, `bun`, `deno` or `quickjs`.

A registered host export runs on an island when anything in its imports
(transitively, through your files and through packages) matches a key. So
`{ sharp: "node" }` moves every host export that reaches `sharp`, and nothing
else.

```typescript
native: { host: { islands: { sharp: "node", "src/native/**": "bun" } } }
```

- An export that reaches islands of two different runtimes cannot run anywhere:
  **KR7231**. Split it so each part imports one island.
- An entry that matches no host code is **KR7232** (a warning).
- With `runtime: "node"` and `islands: { "src/lib/fast.host.ts": "quickjs" }`
  the module runs on the embedded runtime and the rest on Node.
- Islands only choose between processes. Calls between exports on different
  runtimes go through the UI, like any other host call.

## Node built-ins on QuickJS (KR7230)

A QuickJS host bundle that imports `node:fs` or any other Node built-in fails the
build with **KR7230** at the import, with a hint that names the fix: add the
file or package to `islands`, or set `runtime` to `node`. A package that uses a
built-in is reported once, with the package name.

## The host child

A Node, Bun or Deno host runs as a child process of the shell and speaks the
same protocol as the [Go host child](/docs/native/go-host/): the shell sets
`KRATE_GOHOST_SOCKET`, the child listens on that unix socket, writes `ready` to
stdout and exits when its stdin closes or on SIGTERM. The shell forwards calls,
streams and signals to it with the ids listed in `app.json`
([`hostchildren`](/docs/native/app-json/)).

The child runtime is `packages/native/hostchild`, a dependency-free ES module
bundled into every host child, so Node 20 or later, Bun and Deno behave the
same. It uses the codec of `@krate/app` (`wire.ts`), so there is one
implementation of the frame format, and the shared wire test vectors run through
it. `globalThis.__krateHost.register` and `registerSignal` behave as in the
embedded runtime: `this.signal` is an `AbortSignal` that aborts when the call is
cancelled, `this.window` is the calling window, a thrown error's `code` and
`data` reach the UI, an async generator is a stream whose `return()` runs when
the UI stops reading, and a `hostSignal` has `get`, `subscribe` and `set`.

Differences from the embedded runtime:

- Host code can log while it loads; the child still reports `ready` once it
  listens.
- A reload (`krate native dev`) restarts the child. Signal values and call state
  are not carried over; open signal connections reconnect on their own.
- The SDK packages that need the shell (`@krate/app/db`, `/kv`, `/fs`, ...) are
  not wired into Node, Bun and Deno children yet. Call them from the UI or from
  QuickJS host code.
- The unix socket needs Linux or macOS. Windows would need a named-pipe
  transport, which is not implemented.
- Native addons (`.node` files) cannot be bundled.

### Development

`krate native dev` and `krate native build` write the child as
`host/<runtime>/host.mjs` and `app.json` starts it with a command line:

| Runtime | Command |
|---------|---------|
| `node` | `node --enable-source-maps {entry}` |
| `bun` | `bun run {entry}` |
| `deno` | `deno run --no-prompt --allow-env --allow-read={sockdir} --allow-write={sockdir} ... {entry}` |

The runtime must be on `PATH` (`bundle: false` is the same). The build prints a
note when it is not. A release build bundles it instead.

### Deno permissions

`native.permissions` maps to Deno flags. The child always gets `--no-prompt`,
`--allow-env`, and read and write access to the shell's socket directory
(`{sockdir}`; a compiled executable cannot name it, so `deno compile` gets
unscoped `--allow-read --allow-write`).

| `native.permissions` | Deno flags |
|----------------------|------------|
| `fs` | `--allow-read --allow-write` |
| `fs:read` | `--allow-read` |
| `net` | `--allow-net` |
| `os:info` | `--allow-sys` |
| every other name | none (those areas are served by the shell) |

## Release builds

`krate native build --release` turns each host child into one self-contained
executable per target. Pin the runtime with `native.host.version` (`"22"`,
`"22.5"`); the build fails with **KR7233** when the installed one does not match.
`bundle: false` is development only and is an error in a release.

| Runtime | How | Verified |
|---------|-----|----------|
| `node` | Bundle as CommonJS, `node --experimental-sea-config`, copy the node binary, inject the blob with `postject` (`npx --yes postject`, or an installed `postject`) and flip the SEA fuse. On macOS the signature is removed and re-added ad hoc. | Linux x64 |
| `bun` | `bun build --compile --target=bun-<os>-<arch>` | Linux x64 |
| `deno` | `deno compile --target <triple>` with the flags above | Command line only |
| `scriptc` | `scriptc build -o <exe> --strip --optimization release`, `SCRIPTC_TARGET` for a cross build | Command line only |

Cross-building Node needs the target's node binary: set
`KRATE_NODE_<OS>_<ARCH>` (for example `KRATE_NODE_LINUX_ARM64`). `KRATE_NODE`,
`KRATE_BUN` and `KRATE_DENO` select the executable that does the compiling.

The release pipeline calls one function:

```go
// package runtimes (packages/compiler/internal/native/runtimes)
func BuildRelease(rt Runtime, bundle bool, target Target, outDir string, in ReleaseInput) (exePath string, err error)

type ReleaseInput struct {
    Entry       string   // .krate/native/host/<runtime>/host.mjs
    Name        string   // executable base name; default krate-host-<runtime>
    Version     string   // native.host.version
    Permissions []string // native.permissions (Deno flags)
}
```

The caller then rewrites the child's `app.json` entry: `entry` is the executable
(relative to the app directory) and `command` is removed. The bundles of a
build are in `NativeAppResult.HostGroups`.

## scriptc (experimental)

[scriptc](https://github.com/vercel-labs/scriptc) compiles TypeScript and
JavaScript to native executables. With `runtime: "scriptc"` and
`experimental.scriptc: true`:

- `krate native dev` runs the host code on QuickJS for fast reloads.
- A release compiles the host bundle into a child executable that speaks the
  child protocol, with no JavaScript engine. A library build would have no
  `async`, so it is always an executable.
- There is no fallback. Code scriptc rejects fails the build with **KR7235**,
  at the original file and line, carrying scriptc's `SC####` code and hint.
- Building needs scriptc on `PATH` (`npm install -g scriptc`, Node 24 or later)
  and a linker: clang on macOS and glibc Linux, zig on Windows, musl and for any
  cross build (`SCRIPTC_LINKER` overrides). A missing tool is **KR7236**.

```sh
krate native check --scriptc                      # scriptc coverage --fail-on=blockers
krate native check --scriptc --scriptc-fail-on divergences
```

`--scriptc` bundles the host code the way a release does, runs
`scriptc coverage --print=diagnostics --fail-on=<level>` on the bundle and maps
each reported site back through the bundle's source map. A site inside the
Krate host child runtime itself is **KR7237**.
