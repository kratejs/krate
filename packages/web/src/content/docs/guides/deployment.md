---
title: Deployment
order: 3
---

# Deployment

Because Krate is **SSG-first**, most sites are just a folder of static files.

## Static hosts (Netlify, Vercel, GitHub Pages, S3, …)

1. Build locally or in CI: `krate build`
2. Deploy the `dist/` directory.

```
dist/          ← deploy this
```

Any static host works. The output has no server runtime dependency — pages are
pre-rendered HTML with hashed JS/CSS assets.

## Node servers (SSR / ISR / API routes / middleware)

If your site uses SSR, ISR, streaming, JS API routes, or middleware, you need
the Node sidecars. Run the built site with `krate serve` or wire the output
into your own Node process:

```sh
krate build
krate serve          # serves dist + sidecars
```

`manifest.json` records per-page modes so the runtime knows which routes need
the renderer.

## Docker

```dockerfile
FROM node:20-slim AS build
WORKDIR /app
COPY . .
RUN npm install -g @krate/core && krate build

FROM nginx:alpine
COPY --from=build /app/dist /usr/share/nginx/html
```

For SSR/ISR/API deployments, use a Node base image and run `krate serve` as the
container command instead of nginx:

```dockerfile
FROM node:20-slim
WORKDIR /app
COPY . .
RUN npm install -g @krate/core && krate build
ENV KRATE_SERVE_PORT=3000
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=3s \
  CMD node -e "fetch('http://127.0.0.1:3000/healthz').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
CMD ["krate", "serve"]
```

### Health & readiness

The server exposes two probes that stay at the root even under a `basePath`:

- `GET /healthz` - liveness; returns `200 ok` while the process serves.
- `GET /readyz` - readiness; returns `503 ssr-unavailable` while an expected SSR
  sidecar is down, otherwise `200 ready`.

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

- **Pagefind (default)** — the recommended `search.engine: "pagefind"` backend
  writes a `pagefind/` bundle at build time as a set of static assets. Pagefind
  fetches its index chunks at search time, so it needs no special
  `Content-Type`, but the files must be deployed as-is.
- **docfind WASM** — if you use `search.engine: "docfind"` (also what dev builds
  use), the search module (`docs/search/docfind_bg.wasm`) is a static asset; make
  sure your host serves `.wasm` with `Content-Type: application/wasm`. Most hosts
  do by default; the search UI falls back to the JSON index if not.
- **Trailing slashes** — routes are emitted as directory index files
  (`/docs/foo/index.html`), so both `/docs/foo` and `/docs/foo/` work on hosts
  with clean-URL rewriting.
