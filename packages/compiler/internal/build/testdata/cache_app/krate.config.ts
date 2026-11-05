export default {
  outDir: "dist",
  pagesDir: "src/pages",
  // The shared cache. The test points REDIS_URL at a real Redis.
  cache: { adapter: "redis", url: process.env.REDIS_URL, prefix: "e2e:", timeoutMs: 300 },
};
