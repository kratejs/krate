export default function SiteLayout({ children }: { children: any }) {
  return (
    <div class="site-shell">
      <Head>
        <link rel="stylesheet" href="/global.css" />
        <link rel="icon" href="/favicon.svg" />
      </Head>

      <header class="site-navbar">
        <Link class="site-navbar-brand" href="/">
          <span class="site-logo">k</span>
          <span>__PROJECT_DISPLAY_NAME__</span>
        </Link>
        <nav class="site-nav">
          <Link href="/">Home</Link>
          <Link href="/about">About</Link>
        </nav>
      </header>

      <main>{children}</main>

      <footer class="site-footer">
        <div class="site-footer-inner">
          <span>(c) {new Date().getFullYear()} __PROJECT_DISPLAY_NAME__</span>
          <span>Built with Krate</span>
        </div>
      </footer>
    </div>
  );
}
