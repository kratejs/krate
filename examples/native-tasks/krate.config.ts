import { defineConfig } from '@krate/core';

export default defineConfig({
  entry: 'src/pages/index.tsx',
  pagesDir: 'src/pages',
  outDir: 'dist',
  native: {
    appId: 'com.example.tasks',
    productName: 'Native Tasks',
    version: '1.0.0',
    window: { width: 960, height: 640, resizable: true },
  },
});
