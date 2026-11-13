---
title: Deployment
description: Ship a Krate site to static hosts, serverless platforms, plain Node, or Docker.
order: 3
---

# Deployment

Because Krate is **SSG-first**, most sites are just a folder of static files.

## Static hosts (Netlify, Vercel, GitHub Pages, S3, ...)

1. Build locally or in CI: `krate build`
2. Deploy the `dist/` directory.

```
dist/          deploy this
```

Static hosts should exclude `dist/.krate/`. It contains every server-only file:
the API route bundles (`api/`, `api-node/`, with their source maps), the
server components and SSR page bundles, `manifest.json`, `server-manifest.json`
(0.4 wrote `manifest.json` at the top of `dist/`),
the SSR renderer, and the persisted ISR cache. `krate serve` needs it, but a
static host must never publish it (`krate deploy` removes it for you). Nothing
under `dist/.krate/` is ever served by `krate serve` or `krate dev`.

`krate serve` also never lists directories (a directory without an
`index.html` is a 404) and never serves dotfiles; only `.well-known/` is
published from `public/`. Dotfiles such as `.env` or `.git` in `public/` are
skipped at build time with a warning.


Any static host works. The output has no server runtime dependency - pages are
pre-rendered HTML with hashed JS/CSS assets.

### Sub-paths and custom domains

Tell Krate where the site lives with `site.url`. A path in the URL becomes the
[base path](/docs/reference/config/#base-path):

```typescript
// https://example.com/ (the domain root)
site: { url: "https://example.com" }

// https://example.com/example/a/
site: { url: "https://example.com/example/a" }
```

GitHub Pages decides the path by how the repository is set up:

| Setup | Site lives at | `site.url` |
| --- | --- | --- |
| Project site, no custom domain | `user.github.io/repo/` | `https://user.github.io/repo` |
| Project site with its own custom domain | `example.com/` | `https://example.com` |
| Project site, custom domain only on the account's user site | `example.com/repo/` | `https://example.com/repo` |
| User site (`user.github.io` repository) | `user.github.io/` | `https://user.github.io` |

A repository with its own custom domain is served from the domain root, and
GitHub redirects `user.github.io/repo/` there. Do not keep the repository name
in `basePath`: stylesheets would load from `example.com/repo/...` and 404.
`krate deploy --target gh-pages` warns when it finds a `CNAME` file together
with a base path.

To build one project for two locations, such as previews on a sub-path and
production at the root, read the URL from the environment:

```typescript
site: { url: process.env.SITE_URL ?? "https://example.com" }
```

### Host adapters (`krate deploy`)

`krate deploy` builds the site and writes the small host-specific config files
that static hosts need (redirects, 404 handling), so you can upload `dist/`
unchanged:

```sh
krate deploy --target netlify     # _redirects (+ _headers)
krate deploy --target cloudflare  # _redirects (+ _headers)
krate deploy --target vercel      # vercel.json at the project root
krate deploy --target gh-pages    # .nojekyll + 404.html
krate deploy --target docker      # Dockerfile + .dockerignore (server hosting)
krate deploy --target node        # dist/.krate/server/server.mjs (plain Node, no Go binary)
```

Projects with server features also deploy to `vercel`, `netlify` and `cloudflare`
as a function or worker; see [Serverless and Node targets](#serverless-and-node-targets).

Redirects and rewrites come from your `redirects`/`rewrites` config, so the
same rules drive `krate serve` and the deployed host. Krate patterns are
translated to each host's syntax: `/legacy/*` with `/docs/:splat` stays as is
for Netlify and Cloudflare and becomes `/legacy/:path*` and `/docs/:path*` for
Vercel; a trailing `:name*` becomes `/*` and `:splat` for Netlify and Cloudflare.
Name the splat explicitly (`:splat`) in the destination: the dev server's
implicit "append the rest of the path" is not portable and is not emitted.

`vercel.json` is written to the **project root** (not `dist/`), where a
Git-integrated Vercel project reads it, with `outputDirectory` pointing at your
build output. Every redirect carries an explicit `permanent` flag (Vercel treats
a missing flag as a 308). An existing `vercel.json` that krate did not write is
never overwritten; merge by hand in that case.

### Projects with server features

SSR/ISR/streaming pages, API routes, server actions and middleware need server
code. `krate deploy --target vercel|netlify|cloudflare` bundles it into a
function or worker (next section); `--target node` and `--target docker` run it
on your own host. For a project that has server features, the **static-only**
adapters (`gh-pages`) fail and list what they found. If you really want only the
static pages from a platform target, pass `--static-only`:

```sh
krate deploy --target netlify --static-only
```

Server-only folders under `dist/.krate/` are removed from the output of every
platform target so they cannot be uploaded.

## Serverless and Node targets

`krate deploy` bundles a portable server handler (`@krate/runtime/server-handler`)
together with your build data, page bundles, API routes, server actions and
middleware into **one self-contained ES module**. It has no Go and no dynamic
imports of your files, so the same code runs on Node, Vercel, Netlify and
Cloudflare Workers. It does per request what `krate start` does: config
redirects, rewrites and headers, middleware, route matching, dynamic-route
templates, SSR/ISR/streaming pages (including async server components under
`Suspense`, `notFound()` and `redirect()`), JS/TS API routes, server actions
(origin check, multipart, redirect, flash cookie), the security headers and the
cache policy (ISR `s-maxage` + `stale-while-revalidate`, `no-store` for SSR).

| Target | Output | Static files | Runs |
|--------|--------|--------------|------|
| `node` | `dist/.krate/server/server.mjs` | the handler's own file server on `dist/` (no dotfiles, no listings, hashed assets immutable, `.br`/`.gz` siblings, Range, `Last-Modified`) | `node dist/.krate/server/server.mjs` |
| `vercel` | `.vercel/output/` (Build Output API v3): `config.json`, `static/`, `functions/__krate.func/` (Node.js function) | Vercel CDN | one Node.js function, response streaming on |
| `netlify` | `netlify/functions/krate.mjs` (a thin wrapper with `config = { path: "/*", preferStatic: true }`, or `{ path: "/*", excludedPath: [...] }` when the project has middleware) over `netlify/server/krate-server.mjs`, plus `netlify.toml`, `_redirects`, `_headers` | Netlify CDN (`dist/`) | one Netlify Function |
| `cloudflare` | `.krate/deploy/cloudflare/worker.mjs`, `wrangler.jsonc` | Workers static assets (`dist/`, `ASSETS` binding) | one Worker with `nodejs_compat` |

All four support SSR, ISR, streaming, async server components, client islands,
JS/TS API routes, server actions, middleware, redirects/rewrites/headers config,
custom 404/500 pages, [per-segment not-found and error pages](/docs/core-concepts/routing/),
trailing-slash canonicalization and dynamic routes.

| | node | vercel | netlify | cloudflare |
| --- | --- | --- | --- | --- |
| Segment 404/500 pages (nearest page, inside layouts, digest) | yes | yes | yes | yes |
| Unmatched URL reaches the function (so `/blog/x` gets blog's 404) | yes | yes (filesystem miss falls through) | yes (`path: "/*"`) | yes (a path without an asset runs the Worker) |
| Middleware runs for prerendered pages | yes | yes (HTML routes go to the function first) | yes (function first, assets excluded) | yes (`run_worker_first: true`) |
| Prerendered HTML served by | the static layer | the function (from the bundle) when there is middleware, else the CDN | the function (from the bundle) when there is middleware, else the CDN | the `ASSETS` binding |

### Node, without the krate binary

```sh
krate deploy --target node          # build + bundle once, in CI
PORT=8080 node dist/.krate/server/server.mjs
```

`server.mjs` binds `PORT` (default `server.port`/3000) and `HOST` (default
`0.0.0.0`), serves static files from the `dist/` directory it sits in
(override with `KRATE_DIST`), and on `SIGTERM`/`SIGINT` stops accepting
connections and drains in-flight requests (15 seconds, `server.shutdownTimeout`).
It also answers `/healthz` and `/readyz`. A container needs only Node and the
build output:

```dockerfile
FROM node:22-slim
WORKDIR /app
COPY dist ./dist
ENV NODE_ENV=production PORT=3000
EXPOSE 3000
CMD ["node", "dist/.krate/server/server.mjs"]
```

Everything Krate can see is inside the bundle, so a project without native
packages needs no `node_modules` at runtime. Packages with native addons (or
listed in `serverExternalPackages`) stay external: the build copies them, with
their dependencies, into `dist/.krate/server/node_modules`, and
`outputFileTracingIncludes` copies extra files next to `server.mjs`. See
[Self-hosting](/docs/guides/self-hosting/) for the full standalone story,
workspace packages, process model, systemd and reverse proxies.

### What each platform really supports

- **Vercel**: `.vercel/output/` is written for `vercel deploy --prebuilt`.
  Config redirects, rewrites and header rules are emitted as native routes (so
  they also apply to static files); everything that is not a file falls through
  to the function. An existing `.vercel/output` that krate did not write is never
  overwritten.
- **Netlify**: the bundle is `netlify/server/krate-server.mjs` (outside the functions directory, where every file is a function) and `netlify/functions/krate.mjs` re-exports it with a literal `config` export, which is what Netlify reads. A hand-written
  `netlify.toml` is left alone and the settings you need are printed.
- **Cloudflare**: `wrangler.jsonc` sets `nodejs_compat`, the assets directory
  and `run_worker_first`: a list of the server paths, or `true` when the project
  has middleware so it sees every request (static files then come through the
  `ASSETS` binding). A hand-written `wrangler.*` is left alone and the settings
  are printed. There is no filesystem on Workers: a route or action that imports
  a Node built-in outside `nodejs_compat` (`fs`, `vm`, `child_process`, `net`...)
  **fails at bundle time** naming the module and the importing file.

### Environment variables

Runtime values come from the platform (`process.env`; on Cloudflare the string
bindings and secrets are copied into `process.env`). Only `KRATE_PUBLIC_*`
values are inlined at build time. Secrets are never written into the bundle
(a test builds with a secret in `.env` and checks the output).

### Caches on serverless

ISR pages, regions and the data cache live in memory **per instance** and are
bounded (`ssr.maxCacheSize`); durability comes from the CDN headers
(`s-maxage` + `stale-while-revalidate`). `revalidatePath` and `revalidateTag`
therefore reach only the instance that handled the request: they are applied
there, and the response reports every target under `skipped` with that reason
(the node target is a single process and is not affected). `krate deploy` warns
when your sources call them. The page cache is pluggable: pass your own
`isrStore` (get/set/delete/deleteRoute/deleteVariant/deleteTags/entriesForRoute)
to `createKrateHandler` to share it across instances, or set `cache` in
`krate.config` to share the ISR page cache and the data cache and make
`revalidatePath` / `revalidateTag` reach every instance: Redis is built in and
any other store plugs in through a small adapter interface (see
[Sharing caches between instances](/docs/guides/caching-and-revalidation/#sharing-caches-between-instances)).
With it the revalidation warning above does not apply.

### What is refused

`krate deploy` fails, naming the feature and suggesting `--target docker`, for
anything that cannot run in JavaScript: Go API routes (`src/api/*.go`), Go
components rendered at request time, a custom API sidecar (`api.sidecar`),
`compat.userAgent`, and community plugins (they may use serve hooks). Legacy
`(req, res)` API handlers are not supported by the portable server: export
named `GET`/`POST` handlers.

### Middleware and static files

Middleware sees every page request on all four targets. With middleware,
Vercel and Netlify no longer let the CDN answer prerendered pages first:

- **Vercel** adds a route before `handle: filesystem` that sends every page
  request (a path without a file extension, or ending in `.html`) to the
  function.
- **Netlify** drops `preferStatic` and lists the extension of every public
  non-HTML file in `excludedPath` (`"/*.js"`, `"/*.png"`...), so only assets
  go straight to the CDN.

The function answers prerendered pages itself after the middleware ran: their
HTML is carried in the bundle (it grows by the size of your static pages).
Content-hashed and other assets keep being served by the CDN, so they never
run middleware. The function applies your `config.matcher`, so the extra
invocations for pages the matcher does not cover are cheap pass-throughs. A
public file without an extension is treated as a page on these two targets.
Without middleware nothing changes: the CDN serves static files first.

### Segment pages

`blog/404.tsx` and `blog/error.tsx` (see routing) travel in the bundle with
the manifest's segment map. The nearest page answers unmatched URLs, closed
dynamic params and `notFound()` with status 404 and `Cache-Control: no-store`,
and a failed render with status 500, `X-Krate-Error: render` and a sanitized
error plus a `digest` (the real error is logged with the same digest). Failed
and not-found renders are never stored by ISR. The page files and their
hydration directories (`dist/blog/__404`) stay in the published output.

## Node servers (SSR / ISR / API routes / middleware)

If your site uses SSR, ISR, streaming, JS API routes, or middleware, you need
the Node sidecars. Run the built site with `krate serve` or wire the output
into your own Node process:

```sh
krate build
krate start          # serves the existing dist + sidecars (no rebuild)
```

`krate serve` builds first and then serves; `krate start` only serves what
`krate build` produced.

`dist/.krate/manifest.json` records per-page modes so the runtime knows which
routes need the renderer. Page routes answer `GET` and `HEAD` only (other
methods get `405` with `Allow: GET, HEAD`), and a page URL without its trailing
slash (`/dash`) redirects to the one canonical form (`/dash/`) for static and
request-time pages alike.

A missing `dist/.krate/server-manifest.json` just means the app has no
request-time pages. A manifest that exists but cannot be read or parsed (a
truncated upload, a bad copy) stops `krate serve` and `krate start` at startup
with an error naming the file; they do not fall back to the build-time HTML. In
dev, a rebuild that leaves an unreadable manifest logs the error and keeps the
last good one.

### Deploy `.krate/` with `dist`

`krate build` compiles `middleware.ts` to `.krate/middleware.js` in the
**project** directory (not in `dist`), and `krate start` reads it from there, so
a server that only receives `dist` has no middleware to run. Copy the project's
`.krate/` directory next to `dist` (the Docker target does this for you).

The build records in `dist/.krate/manifest.json` whether the app has
middleware, and `krate start` fails closed on it:

- If the app has middleware and `.krate/middleware.js` is missing, the server
  refuses to start and the error names the missing path.
- If the module disappears while the server runs, every request that needs the
  middleware gets a `500` and a log line; it is never passed through.
- If the middleware module exists but fails to load in the Node, Bun or Deno
  sidecar, requests fail instead of skipping it.
- A `dist` built by an older krate has no such record. When `.krate/middleware.js`
  is also absent, the server logs a warning and serves as before, because it
  cannot tell "no middleware" from "middleware not deployed". Run `krate build`
  again to get the record.

The serverless and Node targets (`vercel`, `netlify`, `cloudflare`, `node`) bundle
the middleware into the server bundle and do not need `.krate/` at runtime.
Static-only deploys (`--static-only`) never run middleware.

`krate serve` binds all interfaces so it works in containers (set `server.host`
to restrict it). `krate dev` binds `127.0.0.1` only.

## Docker

`krate deploy --target docker` writes a multi-stage `Dockerfile` and a
`.dockerignore` at the project root. The **build stage** installs dependencies
with your lockfile's package manager, builds the site and drops the dev
dependencies. The **runtime stage** starts from a clean `node` image and copies
in only what `krate start` reads:

- the build output (`dist`, with its `.krate` server bundles and sidecar scripts),
- the project's `.krate` directory without its build-only parts (server actions,
  middleware and the Go host binary stay; the build cache, generated types,
  config schema and segment sources go),
- `krate.config.*` (the server evaluates it, imports included),
- `package.json` and `node_modules` without dev dependencies.

No sources and no Go toolchain end up in the runtime image (Go API routes compile
in the build stage). The image runs as the unprivileged `node` user, has a
`HEALTHCHECK` on `/healthz`, and starts `krate start` (SSR, ISR, API routes,
actions and middleware all work) without rebuilding.

`@krate/core` (the config imports it and the binary lives next to it) and the
other `@krate/*` packages are moved to `dependencies` inside the build stage when
you list them as `devDependencies`, because the server bundles import
`@krate/runtime` and the config imports `@krate/core` at runtime. Any other
package that `krate.config.*` or a page imports must be in `dependencies`; if one
is only a dev dependency, build with `--build-arg KEEP_DEV_DEPS=true` to keep
them all. For a small test project the runtime image was 419 MB against 543 MB
for the old single-stage image (the `node:22-bookworm-slim` base is 329 MB of
that).

Hand-written Dockerfiles are never overwritten. The manual equivalents:

```dockerfile
FROM node:20-slim AS build
WORKDIR /app
COPY . .
RUN npm install -g @krate/core && krate build

FROM nginx:alpine
COPY --from=build /app/dist /usr/share/nginx/html
```

For SSR/ISR/API deployments, use a Node base image and run `krate start` as the
container command instead of nginx (the image already contains the build):

```dockerfile
FROM node:20-slim
WORKDIR /app
COPY . .
RUN npm install -g @krate/core && krate build
ENV KRATE_SERVE_PORT=3000
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=3s \
  CMD node -e "fetch('http://127.0.0.1:3000/healthz').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
CMD ["krate", "start"]
```

`krate start` reads the port from `KRATE_SERVE_PORT` or `PORT` (platforms such
as Heroku and Cloud Run set `PORT`). It handles `SIGTERM` by draining in-flight
requests (15 seconds by default, `server.shutdownTimeout`), stopping its
sidecars and exiting 0, so rolling deploys do not cut requests off.

Behind a load balancer or reverse proxy that terminates TLS or rewrites `Host`,
set `server: { trustProxy: true }` so middleware, API routes and the
server-action origin check see the original scheme, host and client address.
The client address is the `X-Forwarded-For` entry `server.trustedProxies` hops
from the right (default 1), never an entry the client put at the front.

### Proxy trust on the serverless targets

The Vercel, Netlify and Cloudflare targets follow `server.trustProxy` too, and
before any of your code runs the handler rewrites `x-forwarded-host`,
`x-forwarded-proto`, `x-forwarded-for`, `x-real-ip` and `forwarded`, so
middleware, API routes and actions only ever see values the platform vouches for.
`trustProxy` has three states:

| Target | not set (default) | `trustProxy: true` | `trustProxy: false` |
|--------|-------------------|--------------------|---------------------|
| Vercel | host and protocol from `x-forwarded-host` / `x-forwarded-proto` (Vercel sets them); client address from `x-real-ip` (Vercel overwrites it) | as default, and the headers are taken as your own proxy sent them (`x-forwarded-for` is left as delivered) | host and protocol from the request URL; no client address |
| Netlify | host and protocol from the request URL; client address from `x-nf-client-connection-ip` | `x-forwarded-host` / `x-forwarded-proto` honoured, `x-forwarded-for` left as delivered | host and protocol from the request URL; no client address |
| Cloudflare | host and protocol from the request URL; client address from `cf-connecting-ip` | `x-forwarded-host` / `x-forwarded-proto` honoured, `x-forwarded-for` left as delivered | host and protocol from the request URL; no client address |

`x-forwarded-for` and `x-real-ip` carry exactly one address (the platform's) when
the table says "client address"; a chain a visitor wrote, a `Forwarded` header or
an `x-forwarded-host` on Netlify or Cloudflare (where nothing sets it) never
reaches your code. Set `trustProxy: true` only when a proxy of yours sits in front
of the platform and sets those headers; `trustedProxies` applies to `krate start`
only, not to the serverless platforms or the Node target. The Node target
(`server.mjs`) trusts forwarded headers only with `trustProxy: true`, like
`krate start`, but passes `x-forwarded-for` on as the proxy delivered it (see
[Self-hosting](/docs/guides/self-hosting/#behind-a-reverse-proxy)). The platform headers above come from each platform's
documentation; Netlify does not document `x-nf-client-connection-ip` on the page
about functions, so check that header on your site before you rely on it.

### Flash cookie secret

The no-JS form feedback cookie (`krate_flash`, see
[Server Actions](/docs/guides/server-actions/)) is signed with HMAC-SHA256 and an
unsigned or forged one is never read. `krate start` uses a per-process secret; set
`KRATE_FLASH_SECRET` to a random string of at least 32 bytes to share the key
between instances (replicas behind a load balancer, serverless instances). It is
read by the Go host, the Node action sidecar and the portable handler, so a cookie
one instance wrote is accepted by the others. When it is not set:

- the Node target and `krate start` sign with a random per-process key (no-JS
  feedback works on a single instance; behind several replicas without a shared
  key a redirect that lands on another replica shows no notice);
- Vercel, Netlify and Cloudflare carry no flash state at all (the action still
  redirects, and the server logs a warning once), because serverless instances
  share no memory and a per-instance key would make the notice appear at random.

### Health & readiness

The server exposes two probes that stay at the root even under a `basePath`:

- `GET /healthz` - liveness; returns `200 ok` while the process serves.
- `GET /readyz` - readiness; returns `503 ssr-unavailable` while an expected SSR
  sidecar is down, otherwise `200 ready`.

The SSR renderer is supervised: if it crashes, `krate start` restarts it with a
growing delay and logs the exit. Until it is back, pages that need it answer
`503` with `Cache-Control: no-store` (and `X-Krate-Error: renderer-unavailable`)
instead of the build-time shell, so a CDN never caches an empty page. The
sidecars also exit with the server: they die with it on Linux, and elsewhere
they watch the parent process.

Wire these into your orchestrator (Kubernetes `livenessProbe`/`readinessProbe`,
Docker `HEALTHCHECK`, load-balancer health checks).

## CI

```yaml
steps:
  - uses: actions/checkout@v4
  - uses: actions/setup-node@v4
    with: { node-version: 20 }
  - run: npm install -g @krate/core
  - run: krate build
  - uses: actions/upload-pages-artifact@v3
    with: { path: dist }
```

## Notes

- **Pagefind (default)** - the recommended `search.engine: "pagefind"` backend
  writes a `pagefind/` bundle at build time as a set of static assets. Pagefind
  fetches its index chunks at search time, so it needs no special
  `Content-Type`, but the files must be deployed as-is.
- **docfind WASM** - if you use `search.engine: "docfind"` (also what dev builds
  use), the search module (`docs/search/docfind_bg.wasm`) is a static asset; make
  sure your host serves `.wasm` with `Content-Type: application/wasm`. Most hosts
  do by default; the search UI falls back to the JSON index if not.
- **Trailing slashes** - routes are emitted as directory index files
  (`/docs/foo/index.html`), so both `/docs/foo` and `/docs/foo/` work on hosts
  with clean-URL rewriting.
