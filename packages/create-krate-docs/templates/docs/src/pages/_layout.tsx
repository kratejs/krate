import { ThemePrePaint, ThemeToggle } from "@krate/base-docs-theme/shell";

export default function SiteLayout({ children }: { children: any }) {
  return (
    <div class="site-shell">
      <ThemePrePaint />
      <Head>
        <link rel="stylesheet" href="/site.css" />
        <link rel="icon" href="/favicon.svg" />
      </Head>

      <a class="skip-link" href="#main">Skip to content</a>

      <header class="site-navbar">
        <a class="site-navbar-brand" href="/">
          <span class="site-logo">k</span>
          <span class="site-navbar-name">__PROJECT_DISPLAY_NAME__</span>
        </a>
        <nav class="site-nav">
          <a href="/docs/">Docs</a>
          <a href="https://github.com/">GitHub</a>
        </nav>
        <div class="site-navbar-actions">
          <ThemeToggle />
        </div>
      </header>

      <main id="main">{children}</main>

      <footer class="site-footer">
        <div class="site-footer-inner">
          <span>(c) 2026 __PROJECT_DISPLAY_NAME__</span>
          <span>Built with Krate</span>
        </div>
      </footer>
    </div>
  );
}
