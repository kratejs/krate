import { defineConfig } from '@krate/core';

export default defineConfig({
  entry: 'src/pages/index.tsx',
  pagesDir: 'src/pages',
  outDir: 'dist',
  native: {
    appId: 'com.example.plugin',
    productName: 'Native Plugin',
    version: '1.0.0',
    window: { width: 720, height: 520, resizable: true },
    // A local plugin: a directory with a krate-plugin.json and a Go package.
    // An npm package ("@acme/krate-sqlite") or a Go module
    // ("go:github.com/acme/krate-sqlite@v1.2.0") is listed the same way.
    plugins: ['./native-plugins/system-info'],
  },
});
