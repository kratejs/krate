import { defineConfig } from '@krate/core';

export default defineConfig({
  entry: 'src/pages/index.tsx',
  pagesDir: 'src/pages',
  outDir: 'dist',
  native: {
    appId: 'com.example.scratchpad',
    productName: 'Scratchpad',
    version: '1.0.0',
    window: { width: 900, height: 640, resizable: true },
    // An allowlist: the build derives the permissions from the code and fails
    // if it needs one that is not listed. Dev and test builds grant exactly this.
    permissions: ['app', 'dialog', 'fs', 'kv', 'menu', 'notify', 'os:info', 'tray', 'window'],
  },
});
