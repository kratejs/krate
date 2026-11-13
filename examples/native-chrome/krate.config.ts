import { defineConfig } from '@krate/core';

export default defineConfig({
  entry: 'src/pages/index.tsx',
  pagesDir: 'src/pages',
  outDir: 'dist',
  native: {
    appId: 'com.example.chrome',
    productName: 'Native Chrome',
    version: '1.0.0',
    // The page draws the title bar: no system title bar, and the window
    // background is transparent where the page leaves it so. On Windows and
    // macOS the system's own window buttons stay (Windows: titleBarOverlay,
    // macOS: the traffic lights); elsewhere the page draws them.
    window: { width: 900, height: 600, resizable: true, titleBar: 'none', transparent: true },
  },
});
