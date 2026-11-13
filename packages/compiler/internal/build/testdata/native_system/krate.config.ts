import { defineConfig } from '@krate/core';

export default defineConfig({
  entry: 'src/pages/index.tsx',
  pagesDir: 'src/pages',
  outDir: 'dist',
  native: {
    appId: 'com.example.system',
    productName: 'Native System',
    version: '1.0.0',
    window: { width: 700, height: 500, resizable: true },
    permissions: ['badge', 'screen', 'power', 'theme', 'login', 'crash'],
  },
});
