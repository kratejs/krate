import { createSignal, createEffect, onMount } from "@krate/runtime";

/**
 * Shared site shell: header, footer, theme toggle, and the marketing HomeLayout.
 * The docs layout composes the same header/footer/nav so marketing and docs
 * pages share one chrome and one theme choice.
 */

export interface SiteNavLink {
  label: string;
  href: string;
  /** Open in a new tab (external links). */
  external?: boolean;
}

export interface SiteSocial {
  icon: string;
  url: string;
  name?: string;
}

export interface SiteFooterColumn {
  title: string;
  links: SiteNavLink[];
}

export interface SiteConfig {
  /** Brand block in the header. `logo` is a text glyph (e.g. "k"). */
  brand?: { text: string; href?: string; logo?: string };
  /** Primary navigation links, shown in the header on every page. */
  nav?: SiteNavLink[];
  /** Social icon links (header + footer). */
  social?: SiteSocial[];
  /** Footer note (left side). */
  footerNote?: string;
  /** Footer license/right side. */
  footerLicense?: string;
  /** Optional footer link columns. */
  footerColumns?: SiteFooterColumn[];
  /** localStorage key for the light/dark preference (default `theme`). */
  themeStorageKey?: string;
}

/** Identity helper giving `defineSite` full type-checking. */
export function defineSite(config: SiteConfig): SiteConfig {
  return config;
}

const DEFAULT_BRAND = { text: "krate", href: "/", logo: "k" };

export function ThemeToggle(props: { storageKey?: string }) {
  const storageKey = props.storageKey || "theme";
  const [theme, setTheme] = createSignal("light");
  var btnRef: HTMLElement | null = null;

  onMount(function () {
    var saved = "";
    try {
      saved = localStorage.getItem(storageKey) || "";
    } catch (e) {}
    var dark = saved === "dark";
    if (!dark && saved !== "light") {
      dark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    }
    setTheme(dark ? "dark" : "light");
  });

  createEffect(function () {
    var isDark = theme() === "dark";
    if (typeof document !== "undefined" && document.documentElement) {
      document.documentElement.setAttribute("data-theme", isDark ? "dark" : "light");
    }
    if (btnRef) {
      btnRef.setAttribute("aria-label", isDark ? "Switch to light mode" : "Switch to dark mode");
      btnRef.setAttribute("data-theme-state", isDark ? "dark" : "light");
    }
  });

  function toggle() {
    var next = theme() === "dark" ? "light" : "dark";
    setTheme(next);
    try {
      localStorage.setItem(storageKey, next);
    } catch (e) {}
  }

  return (
    <button class="theme-toggle" ref={btnRef} aria-label="Toggle colour theme" type="button" onClick={toggle}>
      <Icon name="tabler:sun" width="16" height="16" />
      <Icon name="tabler:moon" width="16" height="16" />
    </button>
  );
}

export function SiteHeader(props: { site: SiteConfig; currentPath?: string }) {
  const site = props.site || {};
  const brand = site.brand || DEFAULT_BRAND;
  const nav = site.nav || [];
  const social = site.social || [];

  return (
    <header class="site-navbar">
      <Link className="site-navbar-brand" href={brand.href || "/"}>
        {brand.logo ? <span class="site-logo">{brand.logo}</span> : <></>}
        <span class="site-navbar-name">{brand.text}</span>
      </Link>
      <nav class="site-nav">
        {nav.map((item) => (
          <Link href={item.href}>{item.label}</Link>
        ))}
      </nav>
      <div class="site-navbar-actions">
        {social.map((s) => (
          <a class="social-link" href={s.url} aria-label={s.name || "Social link"} rel="noopener noreferrer" target="_blank">
            <Icon name={s.icon} width="18" height="18" />
          </a>
        ))}
        <ThemeToggle storageKey={site.themeStorageKey} />
      </div>
    </header>
  );
}

export function SiteFooter(props: { site: SiteConfig }) {
  const site = props.site || {};
  const columns = site.footerColumns || [];
  return (
    <footer class="site-footer">
      <div class="site-footer-inner">
        {columns.length > 0 && (
          <div class="site-footer-columns">
            {columns.map((col) => (
              <div class="site-footer-column">
                <div class="site-footer-column-title">{col.title}</div>
                {col.links.map((l) => (
                  <Link href={l.href}>{l.label}</Link>
                ))}
              </div>
            ))}
          </div>
        )}
        <div class="site-footer-meta">
          <span>{site.footerNote || ""}</span>
          <span>{site.footerLicense || ""}</span>
        </div>
      </div>
    </footer>
  );
}

/**
 * Marketing/home shell. Wraps product pages in the shared header/footer so the
 * marketing site and docs share one chrome. Usage:
 *
 *   export default function Layout({ children }) {
 *     return <HomeLayout site={site}>{children}</HomeLayout>
 *   }
 */
export function HomeLayout(props: { site: SiteConfig; currentPath?: string; children: any }) {
  return (
    <div class="site-shell">
      <Head>
        <link rel="stylesheet" href="/site.css" />
      </Head>
      <SiteHeader site={props.site} currentPath={props.currentPath} />
      <main>{props.children}</main>
      <SiteFooter site={props.site} />
    </div>
  );
}
