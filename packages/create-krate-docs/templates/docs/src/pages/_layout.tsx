export default function SiteLayout({ children }: { children: any }) {
  return (
    <div class="site-shell">
      <Head>
        <link rel="stylesheet" href="/site.css" />
        <link rel="icon" href="/favicon.svg" />
      </Head>

      <header class="site-navbar">
        <a class="site-navbar-brand" href="/">
          <span class="site-logo">k</span>
          <span class="site-navbar-name">__PROJECT_DISPLAY_NAME__</span>
        </a>
        <nav class="site-nav">
          <a href="/docs/">Docs</a>
          <a href="https://github.com/">GitHub</a>
        </nav>
      </header>

      <main>{children}</main>

      <footer class="site-footer">
        <div class="site-footer-inner">
          <span>© 2026 __PROJECT_DISPLAY_NAME__</span>
          <span>Built with Krate</span>
        </div>
      </footer>
    </div>
  );
}
