import { defineConfig, docs, sitemap } from '@krate/core';

// Replace with your production URL.
const baseUrl = "https://example.com";

export default defineConfig({
  entry: "src/pages/index.tsx",
  outDir: "dist",
  pagesDir: "src/pages",
  publicDir: "public",
  minify: true,

  devServer: {
    port: 3000,
    open: false,
  },

  markdown: {
    gfm: true,
    headingAnchors: true,
    admonitions: true,
    codeHighlight: true,
  },

  seo: {
    baseUrl,
    siteName: "__PROJECT_DISPLAY_NAME__",
    description: "Documentation site built with Krate.",
  },

  plugins: [
    sitemap({ baseUrl, changeFreq: "daily", priority: "0.8" }),
    docs({
      contentDir: "content/docs",
      title: "__PROJECT_DISPLAY_NAME__",
      layout: "src/components/docs-layout.tsx",
      search: {
        enabled: true,
        engine: "docfind",
        maxResults: 8,
      },
      links: [
        { icon: "lucide:github", url: "https://github.com/" },
      ],
    }),
  ],
});
