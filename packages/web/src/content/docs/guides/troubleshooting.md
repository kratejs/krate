---
title: Troubleshooting
order: 8
description: Common Krate errors and how to fix them.
---

# Troubleshooting

## Build fails with `expected X, got EOF`

A construct is unterminated. Common causes:

- An unclosed JSX element (`<div>` with no `</div>`).
- An unclosed expression or string.
- A missing `)` or `}`.

The diagnostic points at `file:line:col`; check the line it names and just
before it.

## `config uses imports/requires but could not be executed`

The compiler could not run `krate.config.ts` (it needs `npx tsx` and your
installed dependencies). Run `npm install` in the project root, then retry.

## `@krate/runtime` / `@krate/components` not found

The Node sidecar and bundler resolve packages from `node_modules`. Install the
dependencies:

```sh
npm install
```

If you use SSR, ensure `@krate/runtime` is installed (it ships the sidecar
renderer).

## CSS classes have no effect

- **Tailwind:** classes are collected from the files listed in `tailwind.content`
  (defaults to `src`). A class built dynamically (e.g. `"text-" + color`) cannot
  be detected — use a full literal class name, or add the file to `content`.
- **CSS Modules:** import the module and reference `styles.foo`; a bare class
  name in a `.module.css` import is scoped.

## Dev server 404s / no hot reload

- The watcher ignores `dist`, `.krate`, `node_modules`, and `_`-prefixed files.
- If you renamed a page, wait for the rebuild banner; a compile error blocks the
  reload (the browser shows a build-error overlay).
- Check the terminal for `Watching … for changes`.

## `krate check` fails on an intentional pattern

Disable the specific rule rather than the whole category:

```ts
checks: {
  rules: {
    "a11y/color-contrast": "off",
    "perf/js-budget": "warning",
  },
  failOn: "error",
}
```

See [Quality Checks](/docs/features/quality-checks/) for the rule list.

## SSR pages return stale ISR content

- ISR serves cached HTML and refreshes in the background. Increase/reduce
  `revalidate` on the page to change cadence.
- Missing or slow refreshes: check the terminal for `ISR revalidation failed`.
- The cache persists to `dist/.krate/isr-cache.json`; delete it to force a cold
  start.

## Port already in use

Set a different port:

```ts
devServer: { port: 3210 }
```

The SSR renderer uses `devServer.port + 10` (override with `ssr.rendererPort`).

## Generated JS is larger than expected

- Run `krate check` — `perf/js-budget` reports per-route JS.
- Move logic into a **server component** (`// @server` or `*.server.tsx`) so it
  ships no client JS.
- Confirm you are not importing a large module into a client component.

## Type errors on generated routes/content

Run `krate types` to regenerate `.krate/types/*.d.ts`, then `npx tsc --noEmit`.
Editors cache declarations — reload the TS server if types look stale.
