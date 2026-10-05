/**
 * Types + helpers for authoring Krate docs themes.
 *
 * A docs theme is a layout component that owns the whole docs shell — navbar,
 * sidebar, TOC, breadcrumbs, prev/next, social links (the "chrome"). The docs
 * plugin resolves it via the `theme` option in krate.config.ts and renders
 * every generated docs page through it, passing a single typed
 * {@link DocsLayoutProps} object. Helper functions are compile-time no-ops —
 * they are erased when the theme is bundled into Krate's embedded runtime.
 */

/** Options a docs theme factory accepts. Theme authors narrow this generic. */
export type DocsThemeOptions = Record<string, unknown>;

/** A single entry in the docs sidebar tree. */
export interface DocsSidebarItem {
  /** Display text (section header or link label). */
  title?: string;
  /** Clickable link (empty/omitted = plain section header). */
  url?: string;
  /** Highlighted when the current page's path matches (computed by krate). */
  active?: boolean;
  /** Nested folder section. */
  children?: DocsSidebarItem[];
  /** Auto-filled by krate for collapsible section home pages. */
  indexURL?: string;
  /** Whether the section shows a collapse toggle. */
  collapsible?: boolean;
  /** Whether the section is expanded for the current page. */
  expanded?: boolean;
  /** Icon name rendered next to the label (resolved by the theme). */
  icon?: string;
  /** Small colored chip rendered next to the label. */
  badge?: { text: string; variant?: string };
}

/** Hero block rendered by `template: hero` pages. */
export interface DocsHero {
  /** Main hero heading (defaults to the page title). */
  title?: string;
  /** Subtitle under the heading. */
  tagline?: string;
  /** Optional image/illustration URL. */
  image?: string;
  /** Call-to-action buttons. */
  actions?: { text: string; link: string; variant?: string }[];
}

/** Page-level head tag emitted from frontmatter `head:`. */
export interface DocsHeadTag {
  tag: string;
  attrs?: Record<string, string | undefined>;
}

/** Pagefind indexer options (only used when `search.engine` is `"pagefind"`). */
export interface PagefindOptions {
  /** Extra CSS selectors Pagefind should not index. */
  excludeSelectors?: string[];
  /** Characters Pagefind should keep when indexing (e.g. `"<>"`). */
  includeCharacters?: string;
  /** Force a single index for this ISO 639-1 language code. */
  forceLanguage?: string;
  /** Bundle directory under the output root (default: `"pagefind"`). */
  outputSubdir?: string;
  /** Print extra indexing logs (default: false). */
  verbose?: boolean;
}

/** Search configuration forwarded to the theme via the layout props. */
export interface DocsSearchOptions {
  /** Search bar on/off (default: true). */
  enabled?: boolean;
  /**
   * Index backend. `"pagefind"` (default, recommended) runs the Pagefind
   * indexer after production builds, `"docfind"` builds the embedded WASM
   * index, and `"json"` uses `search-index.json` only. Dev builds always use
   * docfind (Pagefind's post-build index isn't regenerated on incremental
   * rebuilds).
   */
  engine?: "pagefind" | "docfind" | "json";
  /** Max number of results shown (default: 8). */
  maxResults?: number;
  /** Pagefind options (only used when `engine` is `"pagefind"`). */
  pagefind?: PagefindOptions;
}

/** Every prop a generated docs page passes to the theme's layout component. */
export interface DocsLayoutProps<Options = DocsThemeOptions> {
  /** Current page's title (from frontmatter or the filename). */
  pageTitle: string;
  /** Site title set via `docs({ title })`. */
  siteTitle: string;
  /** Recursive sidebar tree (enriched: active/expanded/collapsible filled in). */
  sidebarItems: DocsSidebarItem[];
  /** Table-of-contents headings extracted from the rendered page. */
  tocItems: { title: string; id: string; depth: number }[];
  /** Breadcrumb trail for the current page. */
  breadcrumbs: { label: string; url: string; isLast: boolean }[];
  /** Previous page title/link in sidebar order. */
  prevTitle?: string;
  prevLink?: string;
  /** Next page title/link in sidebar order. */
  nextTitle?: string;
  nextLink?: string;
  /** `docs({ links })` social links. */
  socialLinks: { icon?: string; url?: string }[];
  /** Current page path (e.g. "getting-started"). */
  currentPath: string;
  /** Page description (from frontmatter) for subtitle/meta rendering. */
  description?: string;
  /** Page layout template: "doc" (default) or "hero". */
  template?: "doc" | "hero";
  /** Hero block for `template: hero` pages. */
  hero?: DocsHero;
  /** Hide the TOC panel (from `toc: false`). */
  tocHidden?: boolean;
  /** Rename the TOC panel heading (from `toc: { label }`). */
  tocLabel?: string;
  /** "Edit this page" link (from `editUrl` or `editLinkBase`). */
  editUrl?: string;
  /** Page tags (rendered as chips + included in search). */
  tags?: string[];
  /** Page categories (rendered as chips linking to category indexes). */
  categories?: string[];
  /** Published date (frontmatter `date`, ISO string). */
  date?: string;
  /** Last-updated date (frontmatter `lastUpdated` or git commit date). */
  lastUpdated?: string;
  /** Rendered markdown content. */
  children?: unknown;
  /** Theme options forwarded when the descriptor declares them. */
  options?: Options;
}

/**
 * Options accepted by the docs plugin (`docs({ ... })` in krate.config.ts).
 */
export interface DocsOptions {
  /** Directory holding the markdown/mdx docs (default: "src/content/docs"). */
  contentDir?: string;
  /** Site title used in the layout and page titles. */
  title?: string;
  /** Legacy root-relative path to a custom layout component. */
  layout?: string;
  /**
   * Docs theme: a package name, a component path, a theme factory descriptor
   * (see {@link defineDocsTheme}), or a theme object returned by one.
   */
  theme?: string | DocsThemeDescriptor;
  /** Extra social links rendered in the sidebar/header. */
  links?: { icon?: string; url?: string }[];
  /** Search configuration. */
  search?: DocsSearchOptions;
  /**
   * Base URL for "Edit this page" links (e.g. a GitHub blob URL). The frontmatter
   * `editUrl` key overrides it per page.
   */
  editLinkBase?: string;
}

/**
 * What a docs theme factory returns (the `theme` option value in
 * krate.config.ts). The generator imports `module` (or `layout`) and renders
 * every docs page through it. `module` is filled automatically by the factory
 * via `import.meta.url`, mirroring plugin factories.
 */
export interface DocsThemeDescriptor<Options = DocsThemeOptions> {
  /** Optional theme name for diagnostics. */
  name?: string;
  /**
   * Path (or file:// URL) to the theme's layout component. Krate bundles it
   * and renders every docs page through it. Filled by the factory.
   */
  module?: string;
  /** Alias of `module` — a root-relative component path, like `layout`. */
  layout?: string;
  /** Options forwarded to the layout component as `props.options`. */
  options?: Options;
}

/**
 * Identity helper that type-checks a docs theme factory's return descriptor.
 * Use it inside a theme module to build the default export factory:
 *
 * @example
 * export default function nightTheme(options: NightThemeOptions = {}) {
 *   return defineDocsTheme({
 *     name: "night-theme",
 *     module: import.meta.url,
 *     options,
 *   });
 * }
 */
export function defineDocsTheme<Options = DocsThemeOptions>(
  descriptor: DocsThemeDescriptor<Options>,
): DocsThemeDescriptor<Options> {
  return descriptor;
}