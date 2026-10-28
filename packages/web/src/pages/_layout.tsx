import { SiteHeader, SiteFooter, ThemeToggle } from "@krate/base-docs-theme/shell";

const site = {
  brand: { text: "krate", href: "/", logoUrl: "/logo.svg" },
  nav: [
    { label: "Docs", href: "/docs/" },
    { label: "Plugins", href: "/docs/features/plugins/" },
    { label: "Runtime API", href: "/docs/reference/runtime-api/" },
    { label: "Blog", href: "/blog/" },
    { label: "Contributing", href: "/docs/contributing/" },
  ],
  social: [
    { icon: "lucide:github", url: "https://github.com/kratejs/krate", name: "GitHub" },
  ],
  footerNote: "© 2026 kratejs",
  footerLicense: "Apache-2.0 · Built with Krate",
  footerColumns: [
    {
      title: "Docs",
      links: [
        { label: "Getting started", href: "/docs/getting-started/" },
        { label: "Core concepts", href: "/docs/core-concepts/" },
        { label: "Configuration", href: "/docs/configuration/" },
      ],
    },
    {
      title: "Features",
      links: [
        { label: "Routing", href: "/docs/core-concepts/routing/" },
        { label: "Styling", href: "/docs/core-concepts/styling/" },
        { label: "Go components", href: "/docs/features/go-components/" },
        { label: "API routes", href: "/docs/features/api-routes/" },
      ],
    },
    {
      title: "Community",
      links: [
        { label: "Blog", href: "/blog/" },
        { label: "Showcase", href: "/showcase/" },
        { label: "Compatibility", href: "/docs/guides/compatibility/" },
        { label: "Roadmap", href: "/roadmap/" },
        { label: "Changelog", href: "/changelog/" },
        { label: "GitHub", href: "https://github.com/kratejs/krate", external: true },
        { label: "Contributing", href: "/docs/contributing/" },
      ],
    },
  ],
};

export default function SiteLayout(props: { children: any }) {
  return (
    <div class="site-shell">
      <Head>
        <link rel="icon" href="/logo.svg" type="image/svg+xml" />
        <link rel="manifest" href="/site.webmanifest" />
        <link rel="preload" href="/fonts/inter.woff2" as="font" type="font/woff2" crossOrigin="anonymous" />
        <link rel="preload" href="/fonts/jetbrains-mono.woff2" as="font" type="font/woff2" crossOrigin="anonymous" />
        <link rel="stylesheet" href="/site.css" />
      </Head>
      <a class="skip-link" href="#main">
        Skip to content
      </a>
      <SiteHeader site={site}>
        <a class="site-search" href="/docs/" aria-label="Search the documentation">
          <span>Search</span>
          <kbd>Ctrl K</kbd>
        </a>
        <ThemeToggle />
      </SiteHeader>
      <main id="main">{props.children}</main>
      <SiteFooter site={site} />
    </div>
  );
}
