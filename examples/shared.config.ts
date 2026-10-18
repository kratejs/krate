import { defineConfig } from '@krate/core';

// Shared base config, merged underneath krate.config.ts via `extends`.
// The main config deep-merges over these values.
export default defineConfig({
  markdown: {
    gfm: true,
    headingAnchors: true,
    admonitions: true,
    codeHighlight: true,
    codeTheme: "github-dark",
  },
  tailwind: {
    enabled: true,
  },
});
