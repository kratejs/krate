import { SiteHeader, SiteFooter, ThemeToggle } from "@krate/base-docs-theme/shell";

const site = {
  brand: { text: "krate", href: "/", logo: "k" },
  nav: [
    { label: "Docs", href: "/docs/" },
    { label: "Plugins", href: "/docs/features/plugins/" },
    { label: "Runtime API", href: "/docs/reference/runtime-api/" },
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
        { label: "API routes", href: "/docs/features/api-routes/" },
      ],
    },
    {
      title: "Community",
      links: [
        { label: "Blog", href: "/blog/" },
        { label: "Showcase", href: "/showcase/" },
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
        <link rel="stylesheet" href="/site.css" />
      </Head>
      <SiteHeader site={site}>
        <ThemeToggle />
      </SiteHeader>
      <main>{props.children}</main>
      <SiteFooter site={site} />
    </div>
  );
}
