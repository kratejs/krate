import { defineConfig } from '@krate/core';

export default defineConfig({
  entry: 'src/pages/index.tsx',
  pagesDir: 'src/pages',
  outDir: 'dist',
  native: {
    appId: 'com.example.media',
    productName: 'Native Media',
    version: '1.0.0',
    window: { width: 900, height: 600, resizable: true },
    // The app decides: the camera is allowed, the microphone and the location
    // are refused. A policy of "prompt" would ask with a native dialog.
    media: {
      camera: 'allow',
      microphone: 'deny',
      location: 'deny',
      usage: {
        camera: 'Native Media shows your camera in the preview.',
        microphone: 'Native Media records a voice note.',
        location: 'Native Media tags notes with where you wrote them.',
      },
    },
  },
});
