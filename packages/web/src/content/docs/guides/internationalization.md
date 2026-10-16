---
title: Internationalization
order: 14
description: Serve the docs in multiple languages with path-prefixed routing and per-page hreflang.
---

# Internationalization

The docs plugin can serve translations of your documentation. Locales live in
subdirectories of the docs content dir, the default locale is served unprefixed,
and every other locale is path-prefixed.

## Configuration

```typescript
// krate.config.ts
export default defineConfig({
  i18n: {
    defaultLocale: "en",
    locales: ["en", "fr", "ja"], // include the default locale
    routing: "prefix",           // only "prefix" is supported
  },
  plugins: [
    docs({ contentDir: "src/content/docs", title: "Docs" }),
  ],
});
```

With `defaultLocale: "en"`:

| File | URL |
|------|-----|
| `src/content/docs/getting-started.md` | `/docs/getting-started/` |
| `src/content/docs/fr/getting-started.md` | `/fr/docs/getting-started/` |
| `src/content/docs/ja/getting-started.md` | `/ja/docs/getting-started/` |

The default locale's pages are the same files you already write; a translation
is just a copy under `<locale>/`.

## What the plugin does

- **Locale switcher** - each page shows links to the same page in every locale
  that has it.
- **`hreflang` alternates** - the head advertises each available translation
  (`<link rel="alternate" hreflang="fr" href="...">`) so search engines can pair
  them. A page that has not been translated yet falls back to the target
  locale's docs home in its switcher link.
- **Document language** - non-default locales set `<html lang>` before paint.

## Translating a page

1. Add the locale to `i18n.locales` in `krate.config.ts`.
2. Copy the page into `src/content/docs/<locale>/`, keeping the same relative
   path so the two versions pair up.
3. Translate the frontmatter (`title`, `description`) and the body. Keep code
   blocks and API names in English; translate prose.

```text
src/content/docs/
  getting-started.md        # en (default, unprefixed)
  fr/
    getting-started.md      # /fr/docs/getting-started/
    index.md                # /fr/docs/ (locale landing page)
```

:::note
Translations are optional. Any page without a translated counterpart still
renders in the default locale; only its switcher link points at the locale
home.
:::

## Partially translated sites

You do not have to translate everything at once. Ship the locales you have
(`locales: ["en", "fr"]`), add pages as they are translated, and drop a locale
from `i18n.locales` to remove it entirely. Unlike a separate app-level router,
there is nothing else to change - removing the config entry and the
`src/content/docs/<locale>/` directory is enough.

## App pages (outside the docs)

`i18n` configures the **docs plugin**. For your own pages (`src/pages/...`),
route per language yourself with a dynamic segment (for example
`src/pages/[locale]/index.tsx` plus `generateStaticParams`) and read the locale
from `params`.
