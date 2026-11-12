import { defineConfig } from '@krate/core';

export default defineConfig({
  entry: 'src/pages/index.tsx',
  pagesDir: 'src/pages',
  outDir: 'dist',
  native: {
    appId: 'com.example.files',
    productName: 'Native Files',
    version: '1.0.0',
    window: { width: 900, height: 600, resizable: true },
    // Double-clicking a .krnote or .note file opens it here. Packaging turns
    // this into Info.plist document types, MSIX file type associations, NSIS
    // registry entries and a .desktop MimeType with a shared-mime-info file.
    fileAssociations: [
      { ext: ['krnote', 'note'], name: 'Krate note', mime: 'text/x-krate-note', role: 'editor' },
    ],
    // A page that reads the files it is given needs fs:read as well.
    permissions: ['app', 'window', 'fs:read'],
  },
});
