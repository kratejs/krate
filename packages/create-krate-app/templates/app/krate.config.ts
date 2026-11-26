import { defineConfig } from '@krate/core';

export default defineConfig({
  entry: "src/pages/index.tsx",
  outDir: "dist",
  pagesDir: "src/pages",
  publicDir: "public",
  minify: true,
  sourcemap: false,

  // Page navigations animate with the View Transitions API. Set "off" to turn
  // that off.
  viewTransitions: "auto",

  devServer: {
    port: 3000,
    open: false,
  },
});
