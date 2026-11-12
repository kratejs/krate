import { defineConfig } from '@krate/core';

export default defineConfig({
  entry: 'src/pages/index.tsx',
  pagesDir: 'src/pages',
  outDir: 'dist',
  native: {
    appId: 'com.example.remote',
    productName: 'Native Remote',
    version: '1.0.0',
    window: { width: 1000, height: 700, resizable: true },
    // A dev build grants what is listed; a release build derives it from the code.
    permissions: ['window', 'remote', 'remote:intercept', 'remote:network'],
  },
});
