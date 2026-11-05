import { defineConfig } from '@krate/core';

export default defineConfig({
  entry: "src/pages/index.tsx",
  outDir: "dist",
  pagesDir: "src/pages",
  publicDir: "public",
  minify: true,
  sourcemap: false,

  devServer: {
    port: 3000,
    open: false,
  },
});
