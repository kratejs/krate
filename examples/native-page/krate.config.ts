import { defineConfig } from '@krate/core';

export default defineConfig({
  entry: 'src/pages/index.tsx',
  pagesDir: 'src/pages',
  outDir: 'dist',
  native: {
    appId: 'com.example.page',
    productName: 'Native Page',
    version: '1.0.0',
    window: { width: 900, height: 600, resizable: true },
  },
});
