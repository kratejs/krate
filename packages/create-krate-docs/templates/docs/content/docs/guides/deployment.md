---
title: Deployment
order: 1
---

# Deployment

The `build` script produces a fully static site in `dist/` — a folder of HTML,
CSS, and JavaScript you can host anywhere.

## Build for production

```bash
npm run build
```

This emits an optimized bundle into `dist/`.

## Static hosts

Deploy the `dist/` directory to any static host:

- **Vercel / Netlify / GitHub Pages** — set the build command to `npm run build`
  and the output directory to `dist`.
- **Any CDN or object store** (S3, R2, Cloudflare Pages) — upload `dist/`.

## Preview locally

```bash
npm run serve
```

This builds and serves the production output so you can verify it before
deploying.
