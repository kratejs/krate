---
title: Upgrading
order: 12
description: Keep Krate, @krate/runtime, and @krate/components in sync and upgrade safely.
---

# Upgrading

Krate ships as several packages that must stay on the same minor:

- `@krate/core` — the CLI + Go compiler
- `@krate/runtime` — signals, DOM, router, server renderer
- `@krate/components` — the built-in component library

## Upgrading a project

```sh
npm install @krate/core@latest @krate/runtime@latest @krate/components@latest
```

`@krate/core` declares an `@krate/runtime` peer; a version mismatch prints a
warning. Upgrade them together.

Then rebuild and check:

```sh
krate build
krate check
```

## Prereleases

Prereleases are published under a dist-tag (e.g. `beta`):

```sh
npm install @krate/core@beta
```

Pin exact versions in production. Prereleases may change config or output
between builds.

## Breaking-change checklist

When upgrading across a minor:

1. Read the release notes (GitHub Releases for the tag).
2. Re-read [Configuration](/docs/reference/config/) for renamed/removed keys —
   Krate now warns on unknown config keys.
3. Regenerate types: `krate types`.
4. Run `krate build && krate check && npx tsc --noEmit`.
5. Diff `dist/` if output shape matters to you.

## Version compatibility

| Package | Requirement |
|---------|-------------|
| Node (for `npx tsx` config + sidecars) | 20+ |
| Go (to build the compiler from source) | see `packages/compiler/go.mod` |
| Browsers | see [Browser Support](/docs/core-concepts/browser-support/) |

## Rolling back

Pin the previous version explicitly:

```sh
npm install @krate/core@0.4.0-rc.4 @krate/runtime@0.4.0-rc.4 @krate/components@0.4.0-rc.4
```

If a build broke after an upgrade, delete stale caches and rebuild:

```sh
rm -rf dist .krate
```
