---
title: Quality Checks
order: 13
description: Compiler-enforced accessibility, SEO, and performance gates.
sidebar:
  label: Quality Checks
  order: 13
---

# Quality Checks

Krate can enforce accessibility, SEO, and performance rules as part of the
build. Checks run against the **final rendered HTML** — exactly what ships — so
a rule can never be fooled by a component that only looks right at author time.

Checks are strictly **opt-in**: without a `checks` key in `krate.config.ts`,
`krate build` never fails for quality. `krate check` runs the built-in rules on
demand.

## Enabling checks

```ts
// krate.config.ts
export default defineConfig({
  checks: {
    a11y: "error",
    seo: "warning",
    perf: "warning",
    budget: { js: 150 },       // KB per route
    failOn: "error",
  },
});
```

With this config, `krate build` fails when an **error**-severity finding is
produced. `krate check` runs the same rules and always exits non-zero when a
finding meets `failOn`.

## The `krate check` command

```sh
krate check          # build, then evaluate quality gates
krate check ./site   # explicit project directory
```

The command builds the site and re-reads the emitted `dist/` output, so the
report reflects the real artifact. It exits `0` when no finding meets the
fail-on threshold, and `1` otherwise — ideal for CI.

```sh
# CI
krate check && echo "quality gates passed"
```

## Built-in rules

| Rule | Category | Default | Flags |
|------|----------|---------|-------|
| `a11y/img-alt` | a11y | error | `<img>` missing an `alt` attribute |
| `a11y/heading-order` | a11y | warning | skipped heading levels, missing/multiple `<h1>` |
| `a11y/accessible-name` | a11y | warning | icon-only links/buttons with no accessible name |
| `a11y/duplicate-id` | a11y | warning | repeated `id` values |
| `a11y/landmark` | a11y | warning | no `<main>`/`<nav>`/landmark role on the page |
| `a11y/form-label` | a11y | warning | form control with no associated label |
| `a11y/tabindex` | a11y | warning | positive `tabindex` (breaks focus order) |
| `a11y/aria-role` | a11y | warning | unknown/invalid ARIA `role` value |
| `a11y/color-contrast` | a11y | warning | inline text/background contrast below 4.5:1 |
| `a11y/broken-anchor` | a11y | warning | in-page `#id` link with no matching `id` |
| `seo/title` | seo | error | missing `<title>` or title over 60 characters |
| `seo/description` | seo | warning | missing/over-long meta description |
| `seo/canonical` | seo | warning | missing `<link rel="canonical">` |
| `seo/og` | seo | warning | missing Open Graph tags (`og:title`/`og:type`/`og:url`) |
| `seo/og-image` | seo | warning | missing or relative `og:image` |
| `seo/lang` | seo | warning | `<html>` missing `lang` |
| `seo/broken-link` | seo | warning | internal link to an unknown route |
| `seo/duplicate-meta` | seo | warning | title/description shared across pages |
| `perf/js-budget` | perf | warning | route JS over `budget.js` |
| `perf/image-dims` | perf | warning | `<img>` missing `width`/`height` (layout shift) |

`alt=""` is accepted as intentional decoration; the `a11y/img-alt` rule only
fires when the attribute is absent.

## Severity & overrides

Each category accepts `"error"`, `"warning"`, `"off"`, or a boolean
(`true` = default severity, `false` = off). Tune individual rules and suppress
noise globally:

```ts
checks: {
  seo: "warning",
  rules: {
    "a11y/img-alt": "off",       // disable one rule
    "perf/js-budget": "error",   // escalate one rule
  },
  ignore: ["seo/og"],            // suppress a rule everywhere
  failOn: "error",               // or "warning" for stricter CI
}
```

`failOn` sets the minimum severity that fails the build/check. Warnings are
still reported (and visible in the output) without failing when
`failOn: "error"` (the default).

### Perf budgets

`budget.js` is the per-route client-JS budget in **kilobytes**, counting the
page's hydration bundle plus the shared runtime chunk:

```ts
checks: {
  perf: "warning",
  budget: { js: 100 },
}
```

## Custom rules

Author rules in TypeScript and point `checks.custom` at the module. Custom
rules run in the embedded QuickJS runtime (`@krate/plugin` provides the types) —
no Node and no subprocess.

```ts
// checks/no-lorem.ts
import { defineCheckRule } from "@krate/plugin";

export default defineCheckRule((page) => {
  const findings = [];
  if (/lorem ipsum/i.test(page.html)) {
    findings.push({
      rule: "custom/no-lorem",
      message: "Remove placeholder copy.",
      severity: "warning",
    });
  }
  return findings;
});
```

```ts
// krate.config.ts
checks: {
  custom: ["./checks/no-lorem.ts"],
}
```

A custom rule receives:

- `page.route` — URL route (e.g. `/blog/hello`)
- `page.source` — source path relative to the project root
- `page.html` — the final rendered HTML
- `page.jsBytes` — the route's client-JS weight
- `page.ast` — the parsed program as a kind-tagged
  [AST document](/docs/features/typed-routes/) (when available)

and returns an array of `{ rule?, message, severity?, line?, col?, hint? }`. A
finding only needs `message`; everything else has sensible defaults.

## Findings format

Findings reuse the compiler's shared diagnostic format, so they match parse and
build errors:

```
  /pricing
    x seo/title  page has no <title>
      hint: Export a <Head><title>…</title></Head> or rely on seo.siteName.
    ! perf/js-budget  route ships 212.4KB of JS (budget 150.0KB)
```
