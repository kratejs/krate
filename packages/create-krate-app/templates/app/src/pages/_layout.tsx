export default function SiteLayout({ children }: { children: any }) {
  return (
    <div class="site-shell">
      <Head>
        <link rel="stylesheet" href="/global.css" />
        <link rel="icon" href="/favicon.svg" />
      </Head>

      <header class="site-navbar">
        <a class="site-navbar-brand" href="/">
          <span class="site-logo">k</span>
          <span>__PROJECT_DISPLAY_NAME__</span>
        </a>
        <nav class="site-nav">
          <a href="/">Home</a>
          <a href="/about">About</a>
        </nav>
      </header>

      <main>{children}</main>

      <footer class="site-footer">
        <div class="site-footer-inner">
          <span>© {new Date().getFullYear()} __PROJECT_DISPLAY_NAME__</span>
          <span>Built with Krate</span>
        </div>
      </footer>
    </div>
  );
}
