import { createCSSChoice, createCSSToggle } from "@krate/runtime";
import type { DocsLayoutProps } from "@krate/plugin";
import {
  Breadcrumbs,
  PrevNext,
  SidebarNav,
  SocialLinks,
  TOCAside,
  TOCNav,
  ThemeSwitch,
} from "./chrome";
import type { SimpleDocsThemeOptions } from "./index";
import "./theme.css";

function feedbackURL(feedback: { repo: string; discussions?: boolean }, title: string, positive: boolean): string {
  const kind = feedback.discussions ? "discussions/new" : "issues/new";
  const heading = (positive ? "Docs feedback (helpful): " : "Docs feedback (not helpful): ") + title;
  const body = "Page: " + title;
  return (
    "https://github.com/" + feedback.repo + "/" + kind +
    "?title=" + encodeURIComponent(heading) +
    "&body=" + encodeURIComponent(body)
  );
}

function Switcher(props: {
  label: string;
  icon: string;
  current?: string;
  links: { code?: string; label: string; url: string }[];
}) {
  if (!props.links || props.links.length < 2) return null;
  return (
    <details class="simple-switcher">
      <summary aria-label={props.label}>
        <Icon name={props.icon} width="15" height="15" />
        {props.current && <span>{props.current}</span>}
      </summary>
      <div class="simple-switcher-menu">
        {props.links.map((link) => (
          <a class={link.code === props.current ? "is-active" : undefined} href={link.url}>{link.label}</a>
        ))}
      </div>
    </details>
  );
}

export default function SimpleDocsLayout(props: DocsLayoutProps) {
  const theme = createCSSChoice<string>("auto", ["auto", "light", "dark"]);
  const nav = createCSSToggle(false);
  const options = (props.options || {}) as SimpleDocsThemeOptions;

  const routeBase = props.routeBase || "/docs";
  const tocItems = props.tocItems || [];
  const showToc = !props.tocHidden && tocItems.length > 0;
  const tocLabel = props.tocLabel || "On this page";
  const tags = props.tags || [];
  const categories = props.categories || [];
  const tagLinks = props.tagLinks || [];
  const categoryLinks = props.categoryLinks || [];
  const i18n = props.i18n || {};
  const localeLinks = i18n.locales || [];
  const versionLinks = i18n.versions || [];
  const activeVersion = i18n.version || i18n.currentVersion || "";
  const hero = props.hero;
  const isHero = props.template === "hero";
  const heroTitle = hero && hero.title ? hero.title : props.pageTitle;
  const heroActions = hero && hero.actions ? hero.actions : [];
  let currentVersionURL = "";
  for (const version of versionLinks) {
    if (version.code === i18n.currentVersion) currentVersionURL = version.url;
  }

  return (
    <div class="simple-page" data-template={props.template || "doc"} data-toc={showToc ? "true" : "false"}>
      <Head>
        <title>{props.pageTitle} - {props.siteTitle}</title>
        {props.description && <meta name="description" content={props.description} />}
        <meta name="theme-color" content="#ffffff" media="(prefers-color-scheme: light)" />
        <meta name="theme-color" content="#0b0b0f" media="(prefers-color-scheme: dark)" />
      </Head>

      <a class="simple-skip" href="#main">Skip to content</a>

      {i18n.versionBanner && (
        <div class="simple-version-banner">
          <span>You are viewing an older version ({i18n.version}).</span>
          {currentVersionURL && <a href={currentVersionURL}>Go to the latest</a>}
        </div>
      )}

      <header class="simple-header">
        <a class="simple-brand" href={routeBase + "/"}>
          <span class="simple-brand-mark" aria-hidden="true">
            <Icon name="lucide:book-open" width="18" height="18" />
          </span>
          <span>{props.siteTitle}</span>
        </a>

        <div class="simple-header-actions">
          {options.searchHref && (
            <a class="simple-search-link" href={options.searchHref}>
              <Icon name="lucide:search" width="16" height="16" />
              <span>{props.search?.placeholder || "Search"}</span>
              {props.search?.hotkey && <kbd>{props.search.hotkey}</kbd>}
            </a>
          )}

          <div class="simple-header-switchers">
            <Switcher label="Change version" icon="lucide:git-branch" current={activeVersion} links={versionLinks} />
            <Switcher label="Change language" icon="lucide:languages" current={i18n.locale} links={localeLinks} />
          </div>

          <SocialLinks links={props.socialLinks} />
          <ThemeSwitch theme={theme} />

          <button
            class="simple-icon-button simple-nav-open"
            type="button"
            aria-label="Open navigation"
            aria-controls="simple-nav-drawer"
            {...nav.trigger()}
          >
            <Icon name="lucide:menu" width="20" height="20" />
          </button>
        </div>
      </header>

      <div class="simple-nav-panel" {...nav.panel(true)}>
        <div class="simple-nav-backdrop" {...nav.close()}></div>
        <aside class="simple-nav-drawer" id="simple-nav-drawer">
          <div class="simple-nav-drawer-head">
            <span>Menu</span>
            <button class="simple-icon-button" type="button" aria-label="Close navigation" {...nav.close()}>
              <Icon name="lucide:x" width="18" height="18" />
            </button>
          </div>
          <div class="simple-nav-drawer-switchers">
            <Switcher label="Change version" icon="lucide:git-branch" current={activeVersion} links={versionLinks} />
            <Switcher label="Change language" icon="lucide:languages" current={i18n.locale} links={localeLinks} />
          </div>
          <SidebarNav items={props.sidebarItems} />
        </aside>
      </div>

      <div class="simple-shell">
        <aside class="simple-sidebar">
          <SidebarNav items={props.sidebarItems} />
        </aside>

        <main class="simple-main" id="main">
          <Breadcrumbs items={props.breadcrumbs} />

          {props.description && !isHero && <p class="simple-description">{props.description}</p>}

          {isHero && (
            <section class="simple-hero">
              <h1>{heroTitle}</h1>
              {hero && hero.tagline && <p class="simple-hero-tagline">{hero.tagline}</p>}
              {hero && hero.image && <img class="simple-hero-image" src={hero.image} alt="" />}
              {heroActions.length > 0 && (
                <div class="simple-hero-actions">
                  {heroActions.map((action) => (
                    <a
                      class={`simple-hero-action${action.variant ? " simple-hero-action-" + action.variant : ""}`}
                      href={action.link}
                    >
                      {action.text}
                    </a>
                  ))}
                </div>
              )}
            </section>
          )}

          {showToc && <TOCNav items={tocItems} label={tocLabel} />}

          <div class="simple-content">{props.children}</div>

          {(tags.length > 0 || categories.length > 0) && (
            <div class="simple-taxonomy">
              {tagLinks.length > 0
                ? tagLinks.map((tag) => <a class="simple-chip" href={tag.url}>{tag.name}</a>)
                : tags.map((tag) => <span class="simple-chip">{tag}</span>)}
              {categoryLinks.length > 0
                ? categoryLinks.map((category) => <a class="simple-chip simple-chip-category" href={category.url}>{category.name}</a>)
                : categories.map((category) => <span class="simple-chip simple-chip-category">{category}</span>)}
            </div>
          )}

          {(props.date || props.lastUpdated) && (
            <p class="simple-dates">
              {props.date && <span>Published: {props.date.slice(0, 10)}</span>}
              {props.lastUpdated && <span>Last updated: {props.lastUpdated.slice(0, 10)}</span>}
            </p>
          )}

          <PrevNext
            prevTitle={props.prevTitle}
            prevLink={props.prevLink}
            nextTitle={props.nextTitle}
            nextLink={props.nextLink}
          />

          {(props.editUrl || options.feedback) && (
            <div class="simple-page-footer">
              {props.editUrl && (
                <a class="simple-edit-link" href={props.editUrl}>
                  <Icon name="lucide:pencil" width="14" height="14" />
                  <span>{options.editLabel || "Edit this page"}</span>
                </a>
              )}
              {options.feedback && (
                <div class="simple-feedback">
                  <span>{options.feedback.label || "Was this helpful?"}</span>
                  <a class="simple-icon-button" href={feedbackURL(options.feedback, props.pageTitle, true)} target="_blank" rel="noopener noreferrer" aria-label="Yes, this page was helpful">
                    <Icon name="lucide:thumbs-up" width="16" height="16" />
                  </a>
                  <a class="simple-icon-button" href={feedbackURL(options.feedback, props.pageTitle, false)} target="_blank" rel="noopener noreferrer" aria-label="No, this page was not helpful">
                    <Icon name="lucide:thumbs-down" width="16" height="16" />
                  </a>
                </div>
              )}
            </div>
          )}
        </main>

        {showToc && <TOCAside items={tocItems} label={tocLabel} />}
      </div>
    </div>
  );
}
