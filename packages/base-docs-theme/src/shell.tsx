import { createSignal, createEffect, onMount, onCleanup, subscribeNavigation, getNavigationState } from "@krate/runtime";
import { activeNavLink } from "./nav";
import { PLATFORM_PREPAINT } from "./platform";

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
  /**
   * `exact` marks the link only on its own page. By default a link is also
   * marked on the pages below it ("/docs/" on "/docs/start/"), except "/".
   */
  match?: "exact" | "prefix";
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
  /** Brand block in the header. `logo` is a text glyph (e.g. "k"); `logoUrl` is an image. */
  brand?: { text: string; href?: string; logo?: string; logoUrl?: string };
  /** Primary navigation links, shown in the header on every page. */
  nav?: SiteNavLink[];
  /** Social icon links (header + footer). */
  social?: SiteSocial[];
  /**
   * Brand block at the start of the footer: logo, name and a short line. `true`
   * reuses the header brand; an object overrides any part of it.
   */
  footerBrand?: boolean | { text?: string; href?: string; logo?: string; logoUrl?: string; tagline?: string };
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

const DEFAULT_BRAND: NonNullable<SiteConfig["brand"]> = { text: "krate", href: "/", logo: "k" };

/**
 * Runs before the stylesheet paints so the persisted (or OS-preferred) color
 * scheme is applied to <html> without a flash of the wrong theme. It also sets
 * data-platform ("mac" or "other") for platform-specific shortcut labels. Every
 * shell that renders ThemeToggle should include this.
 */
export function ThemePrePaint(props: { storageKey?: string }) {
  const key = JSON.stringify(props.storageKey || "theme");
  const script =
    "(function(){try{var k=" + key +
    ";var s=localStorage.getItem(k);if(s!=='dark'&&s!=='light'){s=window.matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light';}" +
    "document.documentElement.setAttribute('data-theme',s);}catch(e){}" + PLATFORM_PREPAINT + "})();";
  return (
    <Head>
      <script>{script}</script>
    </Head>
  );
}

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
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/></svg>
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3c.132 0 .263 0 .393 0a7.5 7.5 0 0 0 7.92 12.446a9 9 0 1 1 -8.313-12.454z"/></svg>
    </button>
  );
}

/**
 * Primary navigation. The link for the current page gets `aria-current="page"`
 * (`"true"` when the page is below the link's section). The state is set again
 * on every client-side navigation, because the header sits in a layout the
 * router keeps mounted. Pass `currentPath` to have it in the built HTML too.
 */
export function SiteNav(props: { items: SiteNavLink[]; currentPath?: string }) {
  const items = props.items || [];
  const initial = props.currentPath ? activeNavLink(items, props.currentPath) : null;
  var navRef: HTMLElement | null = null;

  onMount(function () {
    if (!navRef || items.length === 0) return;
    var nav = navRef;
    function mark(path: string) {
      var hit = activeNavLink(items, path);
      var anchors = nav.querySelectorAll("a");
      for (var i = 0; i < anchors.length; i++) {
        if (hit && hit.index === i) anchors[i].setAttribute("aria-current", hit.exact ? "page" : "true");
        else anchors[i].removeAttribute("aria-current");
      }
    }
    mark(getNavigationState().pathname);
    var stop = subscribeNavigation(function (state) {
      mark(state.pathname);
    });
    onCleanup(stop);
  });

  return (
    <nav class="site-nav" aria-label="Primary" ref={navRef}>
      {items.map((item, i) => (
        <Link
          href={item.href}
          {...(initial && initial.index === i ? { "aria-current": initial.exact ? "page" : "true" } : {})}
        >
          {item.label}
        </Link>
      ))}
    </nav>
  );
}

function BrandMark(props: { logo?: string; logoUrl?: string; text: string }) {
  return (
    <>
      {props.logoUrl ? (
        <img class="site-logo site-logo-image" src={props.logoUrl} alt="" aria-hidden="true" width="28" height="28" />
      ) : props.logo ? (
        <span class="site-logo">{props.logo}</span>
      ) : (
        <></>
      )}
      <span class="site-navbar-name">{props.text}</span>
    </>
  );
}

export function SiteHeader(props: { site: SiteConfig; currentPath?: string; children?: any }) {
  const site = props.site || {};
  const brand = site.brand || DEFAULT_BRAND;
  const nav = site.nav || [];
  const social = site.social || [];

  return (
    <header class="site-navbar">
      <Link className="site-navbar-brand" href={brand.href || "/"}>
        <BrandMark logo={brand.logo} logoUrl={brand.logoUrl} text={brand.text} />
      </Link>
      <SiteNav items={nav} currentPath={props.currentPath} />
      <div class="site-navbar-actions">
        {social.map((s) => (
          <a class="social-link" href={s.url} aria-label={s.name || "Social link"} rel="noopener noreferrer" target="_blank">
            <Icon name={s.icon} width="18" height="18" />
          </a>
        ))}
        {props.children}
      </div>
    </header>
  );
}

export function SiteFooter(props: { site: SiteConfig }) {
  const site = props.site || {};
  const columns = site.footerColumns || [];
  const own = site.footerBrand === true ? {} : site.footerBrand || null;
  const headerBrand = site.brand || DEFAULT_BRAND;
  // The footer brand inherits the header's mark unless it brings its own.
  const ownMark = !!(own && (own.logo || own.logoUrl));
  const brand = own
    ? {
        text: own.text || headerBrand.text,
        href: own.href || headerBrand.href || "/",
        logo: ownMark ? own.logo : headerBrand.logo,
        logoUrl: ownMark ? own.logoUrl : headerBrand.logoUrl,
        tagline: own.tagline || "",
      }
    : null;
  return (
    <footer class="site-footer">
      <div class="site-footer-inner">
        {(brand || columns.length > 0) && (
          <div class={brand ? "site-footer-top has-brand" : "site-footer-top"}>
            {brand && (
              <div class="site-footer-brand">
                <Link className="site-footer-brand-link" href={brand.href}>
                  <BrandMark logo={brand.logo} logoUrl={brand.logoUrl} text={brand.text} />
                </Link>
                {brand.tagline && <p class="site-footer-tagline">{brand.tagline}</p>}
              </div>
            )}
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
      <ThemePrePaint storageKey={props.site.themeStorageKey} />
      <Head>
        <link rel="stylesheet" href="/site.css" />
      </Head>
      <SiteHeader site={props.site} currentPath={props.currentPath}>
        <ThemeToggle />
      </SiteHeader>
      <main>{props.children}</main>
      <SiteFooter site={props.site} />
    </div>
  );
}
