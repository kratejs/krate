import { defineConfig, defineContent, docs, sitemap, robots, llms } from '@krate/core';
import demoPlugin from './plugins/krate-plugin-demo';
import { baseDocsTheme } from '@krate/base-docs-theme';

export default defineConfig(({ isDev }) => ({
  // Merge ./shared.config.ts (markdown + tailwind) underneath this config.
  extends: "./shared.config.ts",

  entry: "src/pages/index.tsx",
  outDir: "dist",
  pagesDir: "src/pages",
  publicDir: "public",
  minify: true,
  sourcemap: true,

  // Canonical site metadata (supersedes `seo`; `seo` still works as an alias).
  site: {
    url: "https://example.com",
    name: "Krate Test",
    description: "A modern static site generator",
    titleTemplate: "%s | Krate Test",
    twitter: "kratejs",
    themeColor: "#0b1020",
    locale: "en",
  },

  devServer: {
    port: 3001,
    open: false,
  },

  // Site-wide <head> injection (structured form). Analytics/scripts go here.
  head: {
    meta: [{ name: "example-head", content: "injected-via-config" }],
    links: [{ rel: "preconnect", href: "https://example.com" }],
    scripts: [{ src: "/analytics.js", defer: true }],
  },

  // Response header overrides (`krate serve`).
  security: { frameOptions: "SAMEORIGIN" },
  headers: [
    {
      source: "/_krate/images/*",
      headers: { "Cache-Control": "public, max-age=31536000, immutable" },
    },
  ],

  // Responsive-image pipeline.
  images: { quality: 80 },

  // Deploy adapter selection (`krate deploy`).
  deploy: { target: "netlify", options: { cleanUrls: "true" } },

  // Build-time precompression (skipped in dev).
  compress: isDev ? undefined : { gzip: true },

  // Markdown transform plugin (runs on AfterMarkdownParse).
  markdown: {
    plugins: ["./plugins/markdown-transform.ts"],
  },

  plugins: [
    sitemap({ baseUrl: "https://example.com" }),
    robots({ allow: "/" }),
    llms({ title: "Krate Docs", description: "Examples documentation" }),
    docs({
      contentDir: "src/content/docs",
      title: "Krate Docs",
      theme: baseDocsTheme(),
      sidebar: "auto",
      toc: true,
      breadcrumbs: true,
      pagination: true,
      editLink: {
        pattern: "https://github.com/kratejs/krate/edit/main/examples/:path",
      },
      search: {
        enabled: true,
        engine: "pagefind",
        maxResults: 8,
        placeholder: "Search the docs",
        hotkey: "/",
      },
      links: [
        { icon: "lucide:github", url: "https://github.com/kratejs" },
        { icon: "lucide:twitter", url: "https://twitter.com/kratejs" }
      ],
    }),
    demoPlugin({ greeting: "Hello from the Krate Demo Plugin!" }),
  ],

  redirects: [
    { source: "/old-about", destination: "/about", permanent: true },
    { source: "/legacy/*", destination: "/docs/:splat", permanent: false },
  ],
  rewrites: [
    { source: "/help/:path*", destination: "/about" },
  ],

  // Typed content collections: entries under src/content/blog are validated
  // against this schema and typed in .krate/types/content.d.ts.
  content: defineContent({
    blog: {
      dir: 'src/content/blog',
      schema: {
        title: 'string',
        description: 'string',
        date: 'date',
        author: 'string',
        tags: 'string[]',
        draft: 'boolean',
        order: { type: 'number', required: true },
        // `enum` restricts allowed values; `default` fills it when omitted.
        status: { type: 'string', enum: ['draft', 'published'], default: 'draft' },
      },
    },
  }),

  // Compiler-enforced quality gates. `krate check` (and `krate build`) evaluate
  // these against the rendered HTML; `./plugins/check-no-lorem.ts` shows a
  // custom QuickJS rule alongside the built-ins.
  checks: {
    a11y: 'warning',
    seo: 'warning',
    perf: 'warning',
    budget: { js: 200 },
    custom: ['./plugins/check-no-lorem.ts'],
  },
}));
