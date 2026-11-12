import { defineConfig } from '@krate/core';

export default defineConfig({
  entry: 'src/pages/index.tsx',
  pagesDir: 'src/pages',
  outDir: 'dist',
  native: {
    appId: 'dev.krate.bench',
    productName: 'KrateBench',
    version: '1.0.0',
    window: { width: 800, height: 600, resizable: true },
  },
});
