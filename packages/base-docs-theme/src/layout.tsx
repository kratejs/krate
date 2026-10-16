import { SidebarNav, TOCNav, Breadcrumbs, PrevNext, SocialLinks } from "./chrome";
import { DocsSearch } from "./search";
import { createSignal, createEffect, onMount, onCleanup } from "@krate/runtime";
import "./theme.css";

interface SidebarItem {
  title: string;
  url: string;
  active?: boolean;
  indexURL?: string;
  collapsible?: boolean;
  expanded?: boolean;
  icon?: string;
  badge?: { text: string; variant?: string };
  children?: SidebarItem[];
}

interface TOCItem {
  title: string;
  id: string;
  depth: number;
}

interface BreadcrumbItem {
  label: string;
  url: string;
  isLast: boolean;
}

interface SocialLinkItem {
  icon: string;
  url: string;
  name: string;
}

interface HeroAction {
  text: string;
  link: string;
  variant?: string;
}

interface HeroData {
  title?: string;
  tagline?: string;
  image?: string;
  actions?: HeroAction[];
}

interface AltLink {
  code?: string;
  label: string;
  url: string;
}

interface I18nProps {
  locale?: string;
  defaultLocale?: string;
  version?: string;
  currentVersion?: string;
  locales?: AltLink[];
  versions?: AltLink[];
  versionBanner?: boolean;
}

interface BaseDocsLayoutProps {
  pageTitle: string;
  siteTitle: string;
  children: any;
  sidebarItems: SidebarItem[];
  tocItems: TOCItem[];
  breadcrumbs: BreadcrumbItem[];
  prevTitle?: string;
  prevLink?: string;
  nextTitle?: string;
  nextLink?: string;
  socialLinks: SocialLinkItem[];
  currentPath: string;
  description?: string;
  template?: "doc" | "hero";
  hero?: HeroData;
  tocHidden?: boolean;
  tocLabel?: string;
  editUrl?: string;
  tags?: string[];
  categories?: string[];
  date?: string;
  lastUpdated?: string;
  authors?: string[];
  i18n?: I18nProps;
  options?: {
    themeStorageKey?: string;
    feedback?: { repo: string; discussions?: boolean; label?: string };
  };
}

function feedbackURL(fb: { repo: string; discussions?: boolean }, title: string, positive: boolean): string {
  const kind = fb.discussions ? "discussions/new" : "issues/new";
  const heading = (positive ? "Docs feedback (helpful): " : "Docs feedback (not helpful): ") + title;
  const body = "Page: " + title;
  return (
    "https://github.com/" +
    fb.repo +
    "/" +
    kind +
    "?title=" +
    encodeURIComponent(heading) +
    "&body=" +
    encodeURIComponent(body)
  );
}

function taxonomySlug(name: string): string {
  return name
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}



export default function BaseDocsLayout(props: BaseDocsLayoutProps) {
  const pageTitle = props.pageTitle;
  const siteTitle = props.siteTitle;
  const themeOptions = props.options || {};
  const themeKey = themeOptions.themeStorageKey || "theme";
  const tocItems = props.tocItems || [];
  const showToc = !props.tocHidden && tocItems.length > 0;
  const tocHeading = props.tocLabel || "On this page";
  const description = props.description;
  const editUrl = props.editUrl;
  const tags = props.tags || [];
  const hasTags = tags.length > 0;
  const categories = props.categories || [];
  const hasCategories = categories.length > 0;
  const lastUpdated = props.lastUpdated || "";
  const i18n = props.i18n || {};
  const localeLinks = i18n.locales || [];
  const versionLinks = i18n.versions || [];
  const activeVersion = i18n.version || i18n.currentVersion;
  let currentVersionURL = "";
  if (i18n.versions) {
    for (const v of i18n.versions) {
      if (v.code === i18n.currentVersion) currentVersionURL = v.url;
    }
  }
  const isHero = props.template === "hero";
  const heroTitle = props.hero && props.hero.title ? props.hero.title : pageTitle;
  const heroTagline = props.hero && props.hero.tagline;
  const heroActions = props.hero && props.hero.actions && props.hero.actions.length > 0 ? props.hero.actions : null;
  const [theme, setTheme] = createSignal("light");
  const [sidebarOpen, setSidebarOpen] = createSignal(false);
  const [tocOpen, setTocOpen] = createSignal(false);

  var themeBtnRef: HTMLElement | null = null;
  var themeSideBtnRef: HTMLElement | null = null;
  var sideBtnRef: HTMLElement | null = null;
  var overlayRef: HTMLElement | null = null;
  var sidebarRef: HTMLElement | null = null;
  var tocBtnRef: HTMLElement | null = null;
  var tocRef: HTMLElement | null = null;
  var tocOverlayRef: HTMLElement | null = null;

  onMount(function () {
    var saved = "";
    try {
      saved = localStorage.getItem(themeKey) || "";
    } catch (e) {}
    var dark = saved === "dark";
    if (!dark && saved !== "light") {
      dark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    }
    setTheme(dark ? "dark" : "light");

    var mq = window.matchMedia("(min-width: 1024px)");
    function onDesktop(e: MediaQueryListEvent) {
      if (e.matches) {
        setSidebarOpen(false);
        setTocOpen(false);
      }
    }
    if (mq.addEventListener) mq.addEventListener("change", onDesktop);
    else if (mq.addListener) mq.addListener(onDesktop);

    window.addEventListener("keydown", onKeydown);

    onCleanup(function () {
      window.removeEventListener("keydown", onKeydown);
      if (mq.removeEventListener) mq.removeEventListener("change", onDesktop);
      else if (mq.removeListener) mq.removeListener(onDesktop);
    });
  });

  createEffect(function () {
    var isDark = theme() === "dark";
    if (typeof document !== "undefined" && document.documentElement) {
      document.documentElement.setAttribute("data-theme", isDark ? "dark" : "light");
    }
    var label = isDark ? "Switch to light mode" : "Switch to dark mode";
    if (themeBtnRef) {
      themeBtnRef.setAttribute("aria-label", label);
      themeBtnRef.setAttribute("data-theme-state", isDark ? "dark" : "light");
    }
    if (themeSideBtnRef) {
      themeSideBtnRef.setAttribute("aria-label", label);
      themeSideBtnRef.setAttribute("data-theme-state", isDark ? "dark" : "light");
    }
  });

  createEffect(function () {
    var open = sidebarOpen();
    if (sidebarRef) sidebarRef.classList.toggle("open", open);
    if (overlayRef) overlayRef.classList.toggle("open", open);
    if (sideBtnRef) sideBtnRef.setAttribute("aria-expanded", open ? "true" : "false");
    if (open) setTocOpen(false);
  });

  createEffect(function () {
    var open = tocOpen();
    if (tocRef) tocRef.classList.toggle("open", open);
    if (tocOverlayRef) tocOverlayRef.classList.toggle("open", open);
    if (tocBtnRef) tocBtnRef.setAttribute("aria-expanded", open ? "true" : "false");
    if (open) setSidebarOpen(false);
  });

  // Lock background scrolling while a mobile drawer/sheet is open. The drawer
  // and TOC never open together (each closes the other), so either flag is
  // enough to decide the lock.
  createEffect(function () {
    if (typeof document === "undefined" || !document.body) return;
    document.body.classList.toggle("krc-nav-open", sidebarOpen() || tocOpen());
  });

  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      setSidebarOpen(false);
      setTocOpen(false);
    }
  }

  function closeNav() {
    setSidebarOpen(false);
    setTocOpen(false);
  }

  function toggleTheme() {
    var next = theme() === "dark" ? "light" : "dark";
    setTheme(next);
    try {
      localStorage.setItem(themeKey, next);
    } catch (e) {}
  }

  return (
    <div class={`docs-page${showToc ? "" : " docs-page-no-toc"}`}>
      <Head>
        <title>{pageTitle} - {siteTitle}</title>
        <meta name="theme-color" content="#ffffff" media="(prefers-color-scheme: light)" />
        <meta name="theme-color" content="#09090b" media="(prefers-color-scheme: dark)" />
      </Head>

      <a class="skip-link" href="#main">Skip to content</a>

      {i18n.versionBanner && (
        <div class="docs-version-banner">
          <span>You're viewing an older version ({i18n.version}).</span>
          {currentVersionURL && (
            <Link class="docs-version-banner-link" href={currentVersionURL}>
              Go to the latest
            </Link>
          )}
        </div>
      )}

      <header class="docs-navbar">
            <Link className="navbar-title" href="/docs/">{siteTitle}</Link>
        <div class="navbar-actions">
          <div class="navbar-switchers">
            {versionLinks.length > 1 && (
              <details class="krate-switcher krate-version-switcher">
                <summary aria-label="Change version">
                  <Icon name="tabler:versions" width="16" height="16" />
                  <span class="krate-switcher-current">{activeVersion}</span>
                </summary>
                <div class="krate-switcher-menu">
                  {versionLinks.map((v) => (
                    <Link class={v.code === activeVersion ? "active" : ""} href={v.url}>
                      {v.label}
                    </Link>
                  ))}
                </div>
              </details>
            )}
            {localeLinks.length > 1 && (
              <details class="krate-switcher">
                <summary aria-label="Change language">
                  <Icon name="tabler:world" width="16" height="16" />
                </summary>
                <div class="krate-switcher-menu">
                  {localeLinks.map((l) => (
                    <Link class={l.code === i18n.locale ? "active" : ""} href={l.url}>
                      {l.label}
                    </Link>
                  ))}
                </div>
              </details>
            )}
          </div>
          <DocsSearch />
          <div class="navbar-social-links">
            <SocialLinks links={props.socialLinks} />
          </div>
          <button class="theme-toggle" id="theme-toggle" ref={themeBtnRef} aria-label="Toggle light mode" type="button" onClick={toggleTheme}>
            <Icon name="tabler:sun" width="16" height="16" />
            <Icon name="tabler:moon" width="16" height="16" />
          </button>
          <button class="sidebar-toggle" id="sidebar-toggle" ref={sideBtnRef} aria-label="Open navigation" aria-controls="sidebar" aria-expanded="false" onClick={() => setSidebarOpen(!sidebarOpen())}>
            <Icon name="tabler:menu" width="20" height="20" />
          </button>
        </div>
      </header>

      {showToc && (
        <div class="toc-mobile-shell">
          <button class="toc-mobile-toggle" id="toc-toggle" ref={tocBtnRef} aria-label="Toggle table of contents" aria-controls="toc" aria-expanded="false" onClick={() => setTocOpen(!tocOpen())}>
            <span class="toc-mobile-copy">
              <span class="toc-mobile-label">{tocHeading}</span>
              <span class="toc-current" id="toc-current"></span>
            </span>
            <Icon name="tabler:chevron-down" width="18" height="18" />
          </button>
        </div>
      )}

      <div class="sidebar-overlay" id="sidebar-overlay" ref={overlayRef} onClick={closeNav}></div>

      {showToc && (
        <div class="toc-overlay" id="toc-overlay" ref={tocOverlayRef} onClick={closeNav}></div>
      )}

      <nav class="sidebar" id="sidebar" ref={sidebarRef}>
        <div class="sidebar-header">
          <div class="sidebar-header-row">
            <div class="sidebar-social-links">
              <SocialLinks links={props.socialLinks} />
            </div>
            <button class="sidebar-close" id="sidebar-close" aria-label="Close navigation" onClick={() => setSidebarOpen(false)}>
              <Icon name="tabler:x" width="18" height="18" />
            </button>
          </div>
          <div class="sidebar-switchers">
            {versionLinks.length > 1 && (
              <details class="krate-switcher krate-version-switcher">
                <summary aria-label="Change version">
                  <Icon name="tabler:versions" width="16" height="16" />
                  <span class="krate-switcher-current">{activeVersion}</span>
                </summary>
                <div class="krate-switcher-menu">
                  {versionLinks.map((v) => (
                    <Link class={v.code === activeVersion ? "active" : ""} href={v.url}>
                      {v.label}
                    </Link>
                  ))}
                </div>
              </details>
            )}
            {localeLinks.length > 1 && (
              <details class="krate-switcher">
                <summary aria-label="Change language">
                  <Icon name="tabler:world" width="16" height="16" />
                </summary>
                <div class="krate-switcher-menu">
                  {localeLinks.map((l) => (
                    <Link class={l.code === i18n.locale ? "active" : ""} href={l.url}>
                      {l.label}
                    </Link>
                  ))}
                </div>
              </details>
            )}
            <button class="theme-toggle" ref={themeSideBtnRef} aria-label="Toggle color theme" type="button" onClick={toggleTheme}>
              <Icon name="tabler:sun" width="16" height="16" />
              <Icon name="tabler:moon" width="16" height="16" />
            </button>
          </div>
        </div>
        <SidebarNav items={props.sidebarItems} currentPath={props.currentPath} onNavigate={closeNav} />
      </nav>

      <div class="docs-body">
        <main class="docs-main" id="main">
          <Breadcrumbs items={props.breadcrumbs} />
          {description && <p class="docs-description">{description}</p>}
          {isHero && (
            <section class="docs-hero">
              <h1 class="docs-hero-title">{heroTitle}</h1>
              {heroTagline && <p class="docs-hero-tagline">{heroTagline}</p>}
              {heroActions && (
                <div class="docs-hero-actions">
                  {heroActions.map((action) => (
                    <Link
                      className={`docs-hero-action${action.variant ? " docs-hero-action-" + action.variant : ""}`}
                      href={action.link}
                    >
                      {action.text}
                    </Link>
                  ))}
                </div>
              )}
            </section>
          )}
          <div class="docs-content">{props.children}</div>
          {(hasTags || hasCategories) && (
            <div class="docs-tags">
              {tags.map((tag) => (
                <Link className="docs-tag" href={"/docs/tags/" + taxonomySlug(tag) + "/"}>{tag}</Link>
              ))}
              {categories.map((cat) => (
                <Link className="docs-tag docs-category" href={"/docs/categories/" + taxonomySlug(cat) + "/"}>{cat}</Link>
              ))}
            </div>
          )}
          {lastUpdated !== "" && (
            <p class="docs-last-updated">Last updated: {lastUpdated.slice(0, 10)}</p>
          )}
          {props.authors && props.authors.length > 0 && (
            <p class="docs-contributors">
              <span>Contributors:</span> {props.authors.join(", ")}
            </p>
          )}
          <PrevNext
            prevTitle={props.prevTitle}
            prevLink={props.prevLink}
            nextTitle={props.nextTitle}
            nextLink={props.nextLink}
          />
          <div class="docs-page-footer">
            {editUrl && (
              <a class="docs-edit-link" href={editUrl}>
                <Icon name="tabler:pencil" width="14" height="14" />
                <span>Edit this page</span>
              </a>
            )}
            {themeOptions.feedback && (
              <div class="docs-feedback">
                <span class="docs-feedback-label">
                  {themeOptions.feedback.label || "Was this helpful?"}
                </span>
                <a
                  class="docs-feedback-btn"
                  href={feedbackURL(themeOptions.feedback, pageTitle, true)}
                  target="_blank"
                  rel="noopener noreferrer"
                  aria-label="Yes, this page was helpful"
                >
                  👍
                </a>
                <a
                  class="docs-feedback-btn"
                  href={feedbackURL(themeOptions.feedback, pageTitle, false)}
                  target="_blank"
                  rel="noopener noreferrer"
                  aria-label="No, this page was not helpful"
                >
                  👎
                </a>
              </div>
            )}
          </div>
        </main>

        {showToc && (
          <aside class="toc" id="toc" ref={tocRef}>
            <div class="toc-panel">
              <div class="toc-header">
                <Icon name="lucide:text-align-start" width="16" height="16" />
                <span>{tocHeading}</span>
              </div>
              <TOCNav items={props.tocItems} onNavigate={closeNav} />
            </div>
          </aside>
        )}
      </div>
    </div>
  );
}