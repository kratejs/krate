import { defineConfig } from '@krate/core';

export default defineConfig({
  entry: 'src/pages/index.tsx',
  pagesDir: 'src/pages',
  outDir: 'dist',
  native: {
    appId: 'com.example.procs',
    productName: 'Native Procs',
    version: '1.0.0',
    window: { width: 1000, height: 700, resizable: true },
    permissions: ['window', 'port', 'worker', 'offscreen', 'automation', 'extension'],
    // A worker process: a TypeScript module on Node that may use ports and
    // nothing else of the SDK.
    workers: {
      echo: { entry: 'src/workers/echo.ts', runtime: 'node', permissions: ['port'], restart: { policy: 'onFailure' } },
    },
    // The remote debugging endpoint, in dev and with native.debug only.
    automation: { port: 19321 },
  },
});
