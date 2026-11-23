---
title: Self-hosting
description: Run a Krate server on your own host - standalone server directory, native addons, extra files, monorepos, systemd, Docker and a reverse proxy.
order: 4
---

# Self-hosting

This guide is for running SSR, ISR, API routes and server actions on a machine
you control: a VM under systemd, a container, or both behind a reverse proxy
such as Caddy. [Deployment](/docs/guides/deployment/) covers the targets one by
one; this page covers what a production server needs.

## Pick a way to run it

| | `krate deploy --target node` | `krate deploy --target docker` | `krate start` |
|---|---|---|---|
| What runs | one Node process (`server.mjs`) | `krate start` in a container | the Go host plus sidecars |
| Needs the `krate` binary on the host | no | no (it is in the image) | yes |
| JS API routes | in the Node process | Node sidecar (or embedded QuickJS) | Node sidecar (or embedded QuickJS) |
| Go API routes and Go components | not supported | yes | yes |
| Output | `dist/` with a self-contained `dist/.krate/server/` | `Dockerfile` | `dist/` plus `.krate/` and `node_modules` |

If you use only TypeScript/JavaScript server code, `--target node` is Krate's
equivalent of Next's `.next/standalone`: a directory you copy to the server and
start with `node`. Use `--target docker` when you use Go API routes or Go
components, or want the image built for you.

## The standalone server directory

```sh
krate deploy --target node      # build, then bundle the server
PORT=8080 node dist/.krate/server/server.mjs
```

`dist/` is the whole deployable: static files, and in `dist/.krate/server/`:

```
dist/.krate/server/
  server.mjs                    # handler, pages, API routes, actions, middleware
  node_modules/                 # only when something stays external (below)
  assets/fonts/...              # files from outputFileTracingIncludes (below)
```

Everything Krate can see is bundled into `server.mjs`, so a project without
native packages needs nothing else at run time and no `node_modules`.

### Packages that stay external

Some packages cannot be bundled: native addons (`.node` files) such as
`@napi-rs/canvas`, `sharp`, `@resvg/resvg-js`, or anything you list in
[`serverExternalPackages`](/docs/reference/config/#server-packages-traced-files-and-env-directory).
For those, the build:

1. leaves `import "sharp"` as an import in `server.mjs`;
2. copies the package, its `dependencies` and its `optionalDependencies` into
   `dist/.krate/server/node_modules` (Node finds them there, next to the
   bundle). Optional dependencies that are not installed, such as the binaries
   for other platforms, are skipped, so the output holds the binary of the
   platform you built on.

```ts
// krate.config.ts
export default defineConfig({
  serverExternalPackages: ["sharp"],   // packages with native bindings are found automatically
});
```

Limits to know about:

- The tracing is by package, not by file: a copied package is copied whole
  (without its own nested `node_modules`, which are traced as dependencies).
  Krate does not trace which files inside a package are used.
- Build on the platform you deploy to (same OS, CPU, libc), because the
  installed native binary is what is copied. Building in the same image you run
  is the simplest way.
- A package that is not installed when you deploy produces a warning, not an
  error.
- Only the `node` target does this. Vercel, Netlify and Cloudflare bundle all
  server code into one file, cannot load native addons, and stop the deploy with
  an error that names the package.

### Files tracing cannot see

Import tracing does not see a worker entry loaded by path or a platform binary
picked by a computed `require`. Name them in
`outputFileTracingIncludes`:

```ts
export default defineConfig({
  outputFileTracingIncludes: {
    "*": ["workers/image.js", "assets/fonts/**"],
    // Platform binaries chosen at run time. Globs under node_modules are
    // followed through the symlinks pnpm creates.
    "/api/og": ["node_modules/@resvg/resvg-js-*/**"],
  },
});
```

Matched files are copied into `dist/.krate/server/` at the same
project-relative path (`workers/image.js` becomes
`dist/.krate/server/workers/image.js`), so resolve them relative to your source
file, as in [Source locations](#source-locations-in-server-code) below. A glob that matches nothing
prints a warning. Globs must be relative to the project root and may not leave
it, unless you set [`outputFileTracingRoot`](#monorepo-layout-outputfiletracingroot). The route keys exist for compatibility with Next.js; Krate copies every
file for every route.

For `--target docker` the Dockerfile copies the leading directory of each glob
(`assets/fonts` for `assets/fonts/**`) from the build stage to the runtime stage
at the same path. Globs under `node_modules` need no copy (the installed
packages are in the image), and a glob with no leading directory, such as
`**/*.wasm`, is flagged in the Dockerfile for you to handle.

### Source locations in server code

`import.meta.url`, `import.meta.dirname`, `import.meta.filename` and, in
CommonJS code, `__dirname` and `__filename` name the source file of the module
that uses them, not the bundle. A workspace package that builds a path from its
own location (`path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..")`)
gets the same answer in `krate dev`, `krate serve` and the deployed server. This
applies to API routes, middleware, instrumentation, server actions, server
rendered pages and the `node` deploy bundle.

- Paths are relative to the source root: `outputFileTracingRoot` when you set it,
  else the project. At run time the bundle finds the directory that corresponds to
  the source root from its own location, so a build tree or a deploy output
  that moves (a Docker image copies the repository to `/app`) keeps working.
  `import.meta.url` is a `file:` URL, as in Next.js with Turbopack; Next also
  leaves `import.meta.dirname` and `import.meta.filename` undefined and gives
  CommonJS a fake `__dirname`, where Krate gives the real per-module paths.
- `krate deploy --target node` copies the files that these paths reach by
  literal arguments into the deploy output at their place below the source root:
  `new URL("../data/x.txt", import.meta.url)` and
  `path.join(import.meta.dirname, "..", "data")` (or `__dirname`) with only string
  literals. A directory over 64 MB is not copied and a warning says so; a file
  outside the source root is reported. Anything built from a variable needs
  `outputFileTracingIncludes`.
- A module that declares its own `__dirname` or `__filename` keeps it, and
  `import.meta.env`, `import.meta.resolve` and a bare `import.meta` are left as
  written. The vercel, netlify and cloudflare targets bundle the output again
  without a file layout: there the paths are relative to the working directory.

### Monorepo layout: outputFileTracingRoot

Next's standalone output mirrors the repository: the server sits at
`.next/standalone/packages/web/server.js`, and code that reads a sibling package
by a relative path (`../render-service/dist/worker.mjs`) finds it because the
files are copied to the same relative places. Set `outputFileTracingRoot` to get
that layout from `krate deploy --target node`:

```ts
// packages/web/krate.config.ts
export default defineConfig({
  outputFileTracingRoot: "../..",                    // the repository root
  outputFileTracingIncludes: {
    "/api/preview": ["../render-service/dist/scramble-worker.mjs"],
    // pnpm store entries are followed through their symlinks
    "*": ["../../node_modules/.pnpm/@fastpkg+image@*/node_modules/@fastpkg/image-*/*"],
  },
});
```

```
dist/.krate/server/
  packages/web/server.mjs                    # <project below the root>/server.mjs
  packages/render-service/dist/scramble-worker.mjs
  node_modules/.pnpm/@fastpkg+image@1.2.0/node_modules/@fastpkg/image-linux-x64-gnu/...
  node_modules/                              # traced external packages, at the root
```

```sh
krate deploy --target node
node dist/.krate/server/packages/web/server.mjs
```

- Included files land at their path below the root, so a path that is
  relative to the app directory resolves the same way as in Next's standalone
  output.
- Traced packages (`serverExternalPackages`, native addons) go to
  `dist/.krate/server/node_modules`. Node finds them from the nested
  `server.mjs` by walking up, as it does in the Next layout.
- The server changes its working directory to the directory of `server.mjs`
  before it starts, like Next's `server.js`. Static files are still served from
  `dist/`; keep the whole `dist` folder together.
- A glob may leave the project but not the root: it is checked after symlinks
  are resolved (inside `node_modules`, links into the package manager's store
  are expected and allowed). The root must contain the project.
- Without the key the layout is the flat one described above.
- `outputFileTracingIncludes` route keys are not used to pick files: the output
  is one server, so the union of all keys is copied.
- `--target docker` builds from the project directory and does not copy files
  outside it. For a monorepo image, build with the node target and copy `dist`
  in your own Dockerfile (see below).

### Docker without the generated Dockerfile

The node target's `dist` is all a container needs:

```dockerfile
FROM node:22-slim
WORKDIR /app
COPY dist ./dist
ENV NODE_ENV=production PORT=3000
EXPOSE 3000
CMD ["node", "dist/.krate/server/server.mjs"]
```

Build `dist` in the same image family (a `node:22-slim` build stage) when a
native addon is involved, so its binary matches.

## Monorepos and workspace packages

A Krate project is the directory that holds `krate.config.ts`; run the commands
from there (`krate build apps/web` also works).

**Resolution.** A workspace package is resolved like any other dependency: the
package manager links it into the project's `node_modules` (pnpm and npm
workspaces use a symlink), and Krate follows the link to its real path, outside
`node_modules`. A file there is treated as your own source.

**Transpiling.** Because it is source, TypeScript and JSX in a workspace
package are compiled with the rest of your code. There is no `transpilePackages`
setting and none is needed: a workspace package whose `main`/`exports` point at
`.ts` or `.tsx` files works for pages, API routes, server actions and
middleware. A package that is installed from a registry (a real directory in
`node_modules`) is not compiled specially; it must ship JavaScript.

**Tracing.** The server bundles that run under plain Node (the per-route Node
artifact used by `krate start` and Docker, action modules, page bundles) leave
registry packages external but bundle workspace packages, because Node cannot
load their TypeScript at run time. The `node` deploy target bundles everything
except the external packages above. So a workspace package never needs to be in
the server directory or the Docker image. Two consequences:

- Dependencies of a workspace package that are not hoisted must also be
  resolvable from the app (list them in the app's `package.json`), as they are
  loaded from the app's `node_modules` at run time.
- Bundled workspace code is not kept as separate files; a change in the package
  needs a rebuild of the app.

**Env files.** A shared `.env` at the repository root is read with
[`envDir`](/docs/features/environment-variables/#env-files-outside-the-project):
`envDir: "../.."`.

For a Docker image built from a monorepo, run the build with the repository
root as the context so the workspace packages and lockfile are present, and
write the Dockerfile by hand (the generated one assumes the project root is the
repository root).

## Process model

`krate start` is one supervisor process that starts what the project needs:

| Process | Started when | Count |
|---|---|---|
| Go host (HTTP server, static files, routing, embedded QuickJS API routes and middleware) | always | 1 |
| Node SSR renderer | a page or region renders at request time with JS | 1, or `ssr.rendererWorkers` (node cluster) |
| Node API/action sidecar | an API route is Node-only (imports a native addon or a built-in, uses a global such as `Buffer` the embedded runtime lacks, or exports `runtime = "nodejs"`), `ssr.apiRuntime` is `node`/`bun`/`deno`, the project has server actions, or it has an [instrumentation](#instrumentation) file | 1 supervisor with `ssr.nodeWorkers` workers (default `min(NumCPU, 8)`) |
| Go API sidecar | the project has Go API routes | 1, over a Unix socket |

Only the host listens on a public port. The sidecars listen on `127.0.0.1` on
ports derived from it (`port + 1` for the API sidecar, `port + 10` for the
renderer; `ssr.rendererPort` pins the renderer). When that port is taken, or
lies in the OS ephemeral port range (where outgoing connections could take it
before the sidecar binds), a free port below that range is used instead, so two
instances on one machine do not collide. `krate dev` skips the derived ports and always
takes a free port below that range. Sidecars are restarted with a growing
delay if they crash, and they exit with the host.

The node target is a single Node process: pages, API routes, actions and
middleware all run in it, with no cluster. Run several instances (a systemd
template unit or several containers) behind the proxy to use more cores, and set
a [shared cache](/docs/reference/config/#shared-cache) and `KRATE_FLASH_SECRET`
so instances agree.

## Instrumentation

A file named `instrumentation.ts` (or `.js`, `.mjs`) in the project root or `src/`
exports an async `register()` that runs **once per server start**, before the server
takes traffic. Use it for one-time setup: loading secrets, connecting a telemetry
SDK, warming a cache, revoking stale sessions. It is the same file Next.js reads, so
a migrated app keeps it.

```ts
// src/instrumentation.ts
export async function register() {
  const { warmCache } = await import("@/server/cache"); // tsconfig paths work
  void warmCache(); // not awaited: the server does not wait for it
}
```

Exactly once per server means:

- `krate build` bundles the file for Node (`.krate/instrumentation.mjs`; npm packages
  stay external and resolve from `node_modules`, as in Node API routes). A CommonJS
  package is imported through its default export, so a named import such as
  `import { loadEnvConfig } from "@next/env"` works even when Node cannot detect the
  package's exports (ncc and webpack bundles).
- `krate start`, `krate serve` and `krate dev` run it in the **primary process of the
  Node API sidecar**, and await it before that process forks its `ssr.nodeWorkers`
  cluster workers or listens. The workers and the SSR renderer never run it. With
  an instrumentation file the sidecar starts even when every API route runs on the
  embedded runtime (the build prints a line saying so).
- The node deploy target (`server.mjs`) runs it once, before `startNodeServer`. The
  module is imported first, so its top-level code runs before the app's modules.
  Other deploy targets do not run it; the deploy prints a warning.
- A supervisor restart of the sidecar after an unrelated crash is a new server start,
  so `register()` runs again. Hot reloads of API modules in `krate dev` do not rerun it;
  restart `krate dev` after editing the file (or see Failure below).
- The server is not ready until `register()` resolves: the API sidecar does not
  listen yet, and `GET /readyz` returns `503 instrumentation-pending`. `/healthz`
  stays `200`.
- Requests that need the sidecar (Node API routes, server actions, dynamic metadata routes,
  middleware that runs on Node) wait for it to listen, up to 30 seconds, instead of failing
  with `502`; this covers the first start and a supervisor restart. If it is still not
  listening after that, they get `503` with `Retry-After: 1`, and Krate logs it once. Static
  files and routes the embedded runtime or Go serves never wait. `server.mjs` of the Node
  deploy target awaits `register()` before it listens, so a request then waits in the
  platform's connection queue or is refused until the server is up.

**Consequences of once per server.** `register()` does not run in every process that
serves requests. State it sets on `globalThis` or in module variables
exists only in the primary process; with `ssr.nodeWorkers` above 1 the workers that
answer API requests do not see it, and neither does the SSR renderer. Environment
variables set in `process.env` do reach the workers, which are forked after
`register()` resolves, but not the SSR renderer.
Put per-process setup (a connection pool, an SDK that patches `http`) in a module the
routes import instead. Setup that lives outside the process (a database migration,
a warmup call, a shared-cache fill) is what `register()` is for. With
`ssr.nodeWorkers: 1` the primary serves requests itself and sees its own state.

**Failure.** A `register()` that throws or rejects, and an instrumentation module that
fails to load (an error in its top-level code or in a module it imports), are startup
failures. Krate logs `An error occurred while loading instrumentation hook: <message>`
with the stack, the wording Next.js uses, and then:

- `krate start` and `krate serve` do not restart the sidecar. They print the file and
  the cause with a hint and exit with status 1, so a process manager or orchestrator
  sees a failed start instead of a server that never becomes ready. Requests that
  arrive meanwhile and need the sidecar get `503` at once, and `GET /readyz` returns
  `503 sidecar-failed`.
- `krate dev` keeps running. The error appears in the log and in the dev overlay, and
  requests that need the sidecar get `503` with the cause at once. The sidecar starts
  again (and runs `register()` again) when the instrumentation file is rebuilt, which
  happens when you save it; after fixing something it imports, save the
  instrumentation file too or restart with `r`.
- The node deploy target (`server.mjs`) exits with status 1 before it listens.

Next.js 16 behaves differently: `next start` keeps running and answers every request
with `500`. Krate refuses to start because a server that cannot serve anything should
fail where a supervisor can see it. Code `register()` starts without awaiting keeps
its own error handling.

A sidecar that exits for another reason before it listens is restarted with a growing
delay; requests stop waiting for it as soon as it has exited and get `503` until a
start succeeds. A sidecar that crashes after it was listening is waited for (up to 30
seconds) while it restarts.

`onRequestError` and `instrumentation-client` are not supported. `process.env.NEXT_RUNTIME`
is not set; `krate migrate nextjs` folds `=== "nodejs"` to `true` and `=== "edge"` to
`false` in the instrumentation files.

## Signals, shutdown and health

- **Shutdown.** `SIGTERM` and `SIGINT` stop accepting connections, let in-flight
  requests finish for `server.shutdownTimeout` seconds (default 15), then exit
  `0` (`krate start` also stops its sidecars first). A second signal exits
  immediately. This holds for `krate start` and for `server.mjs`.
- **Health.** `GET /healthz` is liveness and returns `200 ok`. `GET /readyz` is
  readiness: on `krate start` it returns `503` while an expected SSR renderer is
  down or `register()` of an [instrumentation](#instrumentation) file has not resolved; on `server.mjs` it returns `200` whenever the process serves. Both stay
  at the root under a `basePath`.
- **Port and host.** `PORT` sets the port (`krate start` also reads
  `KRATE_SERVE_PORT`), `HOST` the bind address of `server.mjs`.

## Behind a reverse proxy

By default Krate ignores `X-Forwarded-*` headers a client sends and replaces
them with what it saw on the socket. Behind your own proxy set
`server: { trustProxy: true }` so request URLs, the server-action origin check
and the client address use the forwarded values:

| | `krate start` | `server.mjs` (node target) |
|---|---|---|
| Forwarded host and protocol | `X-Forwarded-Host`, `X-Forwarded-Proto` | same |
| Client address | the `X-Forwarded-For` entry `server.trustedProxies` hops from the right (default 1); the peer address is appended to the chain first | the `X-Forwarded-For` header as the proxy delivered it; `trustedProxies` is not applied |
| Without `trustProxy` | headers are replaced by what the server observed | same |

Behind Caddy (`reverse_proxy 127.0.0.1:3000`) Caddy sets `X-Forwarded-For`,
`-Proto` and `-Host` itself and, unless you configure `trusted_proxies`,
overwrites the ones a client sends, so one trusted hop is correct. With a proxy
that appends to a client-supplied `X-Forwarded-For` (nginx's
`$proxy_add_x_forwarded_for`), read the last entry rather than the first in
`server.mjs` code.

```caddyfile
example.com {
  encode zstd gzip
  reverse_proxy 127.0.0.1:3000 {
    health_uri /readyz
  }
}
```

## systemd

```ini
# /etc/systemd/system/myapp.service
[Unit]
Description=myapp (Krate)
After=network.target

[Service]
User=myapp
WorkingDirectory=/srv/myapp
ExecStart=/usr/bin/node dist/.krate/server/server.mjs
Environment=NODE_ENV=production PORT=3000 HOST=127.0.0.1
EnvironmentFile=/etc/myapp.env
Restart=on-failure
# server.shutdownTimeout (15 s) plus a margin: systemd sends SIGTERM first.
TimeoutStopSec=25

[Install]
WantedBy=multi-user.target
```

For `krate start`, use `ExecStart=/srv/myapp/node_modules/.bin/krate start` with
the same unit (the project's `.krate/` directory and `node_modules` must be
deployed with `dist`, see [Deployment](/docs/guides/deployment/#deploy-krate-with-dist)).
`server.mjs` does not read `.env` files; put the variables in the
`EnvironmentFile`.
