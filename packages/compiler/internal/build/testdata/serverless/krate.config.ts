export default {
  outDir: "dist",
  pagesDir: "src/pages",
  redirects: [
    { source: "/old-about", destination: "/about", permanent: true },
    { source: "/legacy/:slug", destination: "/blog/:slug", permanent: false },
  ],
  rewrites: [{ source: "/team", destination: "/about" }],
  headers: [{ source: "/about", headers: { "X-Custom": "about" } }],
};
