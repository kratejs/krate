export default function SiteLayout({ children }: { children: any }) {
  return (
    <div class="site-shell">
      <Head>
        <link rel="stylesheet" href="/site.css" />
      </Head>

      <header class="site-navbar">
        <a class="site-navbar-brand" href="/">
          <span class="site-logo">k</span>
          <span class="site-navbar-name">krate</span>
        </a>
        <nav class="site-nav">
          <a href="/docs/">Docs</a>
          <a href="/docs/features/plugins/">Plugins</a>
          <a href="/docs/reference/runtime-api/">Runtime API</a>
          <a href="/docs/guides/contributing/">Contributing</a>
          <a href="https://github.com/kratejs/krate" class="site-nav-github">
            GitHub
          </a>
        </nav>
      </header>

      <main>{children}</main>

      <footer class="site-footer">
        <div class="site-footer-inner">
          <span>© 2026 kratejs</span>
          <span>Apache-2.0 · Built with Krate</span>
        </div>
      </footer>
    </div>
  );
}
