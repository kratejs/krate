import { defineConfig, defineContent, docs, sitemap } from '@krate/core';
import { baseDocsTheme } from '@krate/base-docs-theme';

export default defineConfig({
  entry: "src/pages/index.tsx",
  outDir: "dist",
  pagesDir: "src/pages",
  publicDir: "public",
  minify: true,

  viewTransitions: "auto",

  devServer: {
    port: 3000,
    open: false,
  },

  markdown: {
    gfm: true,
    headingAnchors: true,
    admonitions: true,
    codeHighlight: true,
    mermaid: true,
  },

  // Canonical site metadata (supersedes `seo`).
  site: {
    url: "https://krate.js.org",
    name: "Krate",
    description: "A Go-native static site generator with signal-based reactivity.",
    image: "https://krate.js.org/og.png",
    titleTemplate: "%s | Krate",
    twitter: "kratejs",
    themeColor: "#0b1020",
    locale: "en",
  },

  // Build-time precompression + deploy adapter for Cloudflare Pages.
  compress: { brotli: true, gzip: true },
  deploy: { target: "cloudflare" },

  redirects: [
    { source: "/docs/guides/contributing/", destination: "/docs/contributing/", permanent: true },
    { source: "/docs/guides/diagrams/", destination: "/docs/core-concepts/markdown/", permanent: true },
  ],

  content: defineContent({
    blog: {
      dir: "src/content/blog",
      schema: {
        title: { type: "string", required: true },
        description: { type: "string", required: true },
        date: { type: "date", required: true },
        author: "string",
        tags: "string[]",
        draft: "boolean",
        order: { type: "number", required: true },
        status: { type: "string", enum: ["draft", "published"], default: "draft" },
      },
    },
  }),

  plugins: [
    sitemap({ baseUrl: "https://krate.js.org", changeFreq: "daily", priority: "0.8" }),
    { name: "robots", options: { allow: "/" } },
    {
      name: "feed",
      options: {
        baseUrl: "https://krate.js.org",
        title: "Krate Docs",
        description: "Guides and reference for the Krate Go-native web framework.",
      },
    },
    docs({
      contentDir: "src/content/docs",
      title: "Krate Docs",
      theme: baseDocsTheme({
        feedback: { repo: "kratejs/krate", label: "Was this page helpful?" },
      }),
      editLink: { pattern: "https://github.com/kratejs/krate/blob/main/packages/web/:path" },
      sidebar: "auto",
      toc: true,
      pagination: true,
      search: {
        enabled: true,
        engine: "pagefind",
        maxResults: 8,
        placeholder: "Search the docs",
        hotkey: "/",
      },
      links: [
        { icon: "lucide:github", url: "https://github.com/kratejs/krate", name: "GitHub" },
      ],
    }),
  ],
});
