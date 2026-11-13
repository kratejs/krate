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
`dist/.krate/server/workers/image.js`), so resolve them from the bundle with
`new URL("./workers/image.js", import.meta.url)`. A glob that matches nothing
prints a warning. Globs must be relative to the project root and may not leave
it. The route keys exist for compatibility with Next.js; Krate copies every
file for every route.

For `--target docker` the Dockerfile copies the leading directory of each glob
(`assets/fonts` for `assets/fonts/**`) from the build stage to the runtime stage
at the same path. Globs under `node_modules` need no copy (the installed
packages are in the image), and a glob with no leading directory, such as
`**/*.wasm`, is flagged in the Dockerfile for you to handle.

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
| Node API/action sidecar | an API route is Node-only (imports a native addon or a built-in), `ssr.apiRuntime` is `node`/`bun`/`deno`, or the project has server actions | 1 supervisor with `ssr.nodeWorkers` workers (default `min(NumCPU, 8)`) |
| Go API sidecar | the project has Go API routes | 1, over a Unix socket |

Only the host listens on a public port. The sidecars listen on `127.0.0.1` on
ports derived from it (`port + 1` for the API sidecar, `port + 10` for the
renderer, or the next free port; `ssr.rendererPort` pins the renderer), so two
instances on one machine do not collide. Sidecars are restarted with a growing
delay if they crash, and they exit with the host.

The node target is a single Node process: pages, API routes, actions and
middleware all run in it, with no cluster. Run several instances (a systemd
template unit or several containers) behind the proxy to use more cores, and set
a [shared cache](/docs/reference/config/#shared-cache) and `KRATE_FLASH_SECRET`
so instances agree.

## Signals, shutdown and health

- **Shutdown.** `SIGTERM` and `SIGINT` stop accepting connections, let in-flight
  requests finish for `server.shutdownTimeout` seconds (default 15), then exit
  `0` (`krate start` also stops its sidecars first). A second signal exits
  immediately. This holds for `krate start` and for `server.mjs`.
- **Health.** `GET /healthz` is liveness and returns `200 ok`. `GET /readyz` is
  readiness: on `krate start` it returns `503` while an expected SSR renderer is
  down; on `server.mjs` it returns `200` whenever the process serves. Both stay
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
