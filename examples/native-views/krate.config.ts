import { defineConfig } from '@krate/core';

export default defineConfig({
  entry: 'src/pages/index.tsx',
  pagesDir: 'src/pages',
  outDir: 'dist',
  native: {
    appId: 'com.example.views',
    productName: 'Native Views',
    version: '1.0.0',
    window: { width: 1100, height: 700, resizable: true },
  },
});
