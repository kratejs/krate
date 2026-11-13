export default {
  entry: 'src/pages/index.tsx',
  outDir: 'dist',
  pagesDir: 'src/pages',
  publicDir: 'public',
  devServer: { port: 3100, open: false },
  tailwind: { enabled: false },
  smoothScroll: {
    lerp: 0.08,
    wheelMultiplier: 1,
    anchors: { offset: -72 },
    prevent: '.no-smooth',
    snap: { type: 'proximity', distanceThreshold: '30%', debounce: 250 },
  },
};
