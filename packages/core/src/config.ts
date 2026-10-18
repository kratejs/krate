/**
 * Typed configuration helpers for Krate.
 *
 * Use `defineConfig` in `krate.config.ts` to get full type-checking of every
 * supported config key - unknown or misspelled keys will fail to compile.
 *
 * ```ts
 * import { defineConfig, sitemap, docs } from '@krate/core';
 *
 * export default defineConfig({
 *   outDir: "dist",
 *   plugins: [
 *     sitemap({ baseUrl: "https://example.com" }),
 *     docs({ contentDir: "src/content/docs", title: "Docs" }),
 *   ],
 * });
 * ```
 */

import type {
  DocsLayoutProps,
  DocsSearchOptions,
  DocsSidebarItem,
  DocsThemeDescriptor,
  DocsThemeOptions,
} from "@krate/plugin";

export interface DevServerConfig {
  /** Dev server port (default: 3000). */
  port?: number;
  /** Open the browser on start (default: false). */
  open?: boolean;
  /** Show the dev error overlay (default: true). */
  overlay?: boolean;
  /** Show the dev toolbar (default: true). */
  toolbar?: boolean;
  /** Editor command for the overlay's "open in editor" action (default: "code"). */
  editor?: string;
  /** Proxy matching path prefixes to a backend during dev, e.g. `{ "/api": "http://localhost:8080" }`. */
  proxy?: Record<string, string>;
}

export interface ServerConfig {
  /** Bind address for `krate serve` (default: all interfaces). */
  host?: string;
  /** Listen port for `krate serve` (falls back to devServer.port, then 3000). */
  port?: number;
  /** Request body cap in bytes (default: 1 MiB pages, 25 MiB API). */
  maxBodySize?: number;
}

export interface CORSConfig {
  enabled?: boolean;
  /** Allowed origins (default ["*"]). */
  origins?: string[];
  methods?: string[];
  headers?: string[];
  credentials?: boolean;
  /** Preflight cache seconds. */
  maxAge?: number;
}

/** A custom API sidecar that owns some/all `/api/*` routes. */
export interface SidecarConfig {
  /** Supervised mode: command to spawn (e.g. "node", "go", "python"). */
  command?: string;
  args?: string[];
  cwd?: string;
  env?: Record<string, string>;
  /** Port the sidecar listens on (required in supervised mode). */
  port?: number;
  /** Proxy-only mode: base URL of an already-running service. */
  target?: string;
  /** Path prefix owned by the sidecar (default "/api"). */
  prefix?: string;
}

export interface APIConfig {
  sidecar?: SidecarConfig;
}

export interface PluginConfig {
  /** Unique plugin name. Built-ins: "sitemap", "docs". */
  name: string;
  /**
   * Community plugins only. Path (or file:// URL) to a JS plugin module. When
   * using a plugin factory function, this is filled in automatically.
   */
  module?: string;
  /** Execution priority. Lower runs first (default: 50). */
  order?: number;
  /** Per-plugin options, read by the plugin's hooks. */
  options?: object;
}

export interface TailwindConfig {
  enabled?: boolean;
  scanDirs?: string[];
  /** Tailwind-style content globs (takes precedence over scanDirs). */
  content?: string[] | { files: string[] };
  /** Enable the base reset. */
  preflight?: boolean;
  /** Warn on classes that produced no rule. */
  strict?: boolean;
  /** "media" (default) | "class" | "selector". */
  darkMode?: string;
  /** Run tailwind.config via `npx tsx` instead of static parse. */
  executeConfig?: boolean;
}

export interface CSPConfig {
  enabled?: boolean;
  /** Custom CSP directive string. Empty = auto-generate. */
  directive?: string;
}

export interface MarkdownConfig {
  /** Root directory for relative links/images (default: project root). */
  root?: string;
  gfm?: boolean;
  headingAnchors?: boolean;
  admonitions?: boolean;
  codeHighlight?: boolean;
  /** chroma theme for syntax highlighting (default "github-dark"). */
  codeTheme?: string;
  math?: boolean;
  /** Render ```` ```mermaid ```` code blocks as diagrams (loads Mermaid from a CDN). */
  mermaid?: boolean;
  /**
   * JS modules that transform rendered markdown HTML (see `defineMarkdownPlugin`
   * in `@krate/plugin`). Each runs on the AfterMarkdownParse hook.
   */
  plugins?: string[];
}

export type RuntimeName = "quickjs" | "node" | "bun" | "deno";

export interface SSRConfig {
  rendererPort?: number;
  timeout?: number;
  maxCacheSize?: number;
  middlewareRuntime?: RuntimeName;
  apiRuntime?: RuntimeName;
  ssrRuntime?: "quickjs" | "node" | "bun" | "deno";
  /** Force ALL pages to render in streaming SSR mode (Suspense-based). */
  streaming?: boolean;
}

export interface RedirectConfig {
  source: string;
  destination: string;
  /** true = 301, false = 302 (default). */
  permanent?: boolean;
}

export interface RewriteConfig {
  source: string;
  destination: string;
}

export interface SEOConfig {
  baseUrl?: string;
  siteName?: string;
  description?: string;
  image?: string;
  /** Title template applied to page titles, e.g. `"%s | Krate"`. */
  titleTemplate?: string;
  /** Default `twitter:site` handle (leading @ optional). */
  twitter?: string;
  /** `<meta name="theme-color">` value. */
  themeColor?: string;
  /** Default `<html lang>` / og:locale, e.g. "en" or "en-US". */
  locale?: string;
}

/**
 * Canonical site metadata. Supersedes `seo` (kept as an alias); values here win
 * per field. The sitemap/feed/llms/robots plugins inherit all of it.
 */
export interface SiteConfig {
  url?: string;
  name?: string;
  description?: string;
  image?: string;
  titleTemplate?: string;
  twitter?: string;
  themeColor?: string;
  locale?: string;
}

export interface RobotsConfig {
  allow?: string;
  disallow?: string;
  sitemap?: string;
}

export interface HeadScript {
  /** External script URL. */
  src?: string;
  /** Inline script body (mutually exclusive with src). */
  content?: string;
  type?: string;
  async?: boolean;
  defer?: boolean;
  /** Emit `type="module"`. */
  module?: boolean;
  /** Extra attributes (e.g. `data-domain`). */
  attrs?: Record<string, string>;
}

export interface HeadMeta {
  name?: string;
  property?: string;
  httpEquiv?: string;
  charset?: string;
  content?: string;
}

export interface HeadLink {
  rel: string;
  href: string;
  as?: string;
  type?: string;
  crossOrigin?: string;
  media?: string;
  sizes?: string;
}

/** Site-wide <head> injection. Also accepts the array form `string[]`. */
export interface HeadConfig {
  meta?: HeadMeta[];
  links?: HeadLink[];
  scripts?: HeadScript[];
  /** Raw HTML injected verbatim (trusted config only). */
  raw?: string[];
}

/**
 * Security response-header overrides for `krate serve`. Empty keeps the default;
 * `"off"` (or "none"/"false") disables the header.
 */
export interface SecurityConfig {
  /** Toggle `X-Content-Type-Options: nosniff` (default on). */
  contentTypeOptions?: boolean;
  /** `X-Frame-Options` (default "DENY"). */
  frameOptions?: string;
  /** `Referrer-Policy` (default "strict-origin-when-cross-origin"). */
  referrerPolicy?: string;
  /** `Permissions-Policy`. */
  permissionsPolicy?: string;
  /** `Strict-Transport-Security` (default sent for https base URLs). */
  hsts?: string;
}

/** Custom response headers for paths matching `source` (`krate serve` only). */
export interface HeaderRule {
  /** Exact path, prefix wildcard ("/assets/*"), or path glob. */
  source: string;
  headers: Record<string, string>;
}

/** Allowlist entry for remote <Image> sources. */
export interface ImageRemotePattern {
  hostname: string;
  pathname?: string;
}

/** Responsive-image pipeline config used by <Image>. */
export interface ImagesConfig {
  /** Generated responsive widths (ascending). */
  breakpoints?: number[];
  /** Base encode quality (1-100, default 82). */
  quality?: number;
  /** Default `sizes` attribute. */
  sizes?: string;
  /** Blur-up placeholder (default true). */
  placeholder?: boolean;
  /** Output formats: "webp" and/or "original" (default both). */
  formats?: ("webp" | "original")[];
  /** Emit the original image with no processing. */
  unoptimized?: boolean;
  /** Remote hosts allowed for <Image src>. */
  remotePatterns?: ImageRemotePattern[];
}

/** Deploy adapter selection + options for `krate deploy`. */
export interface DeployConfig {
  target?: "netlify" | "vercel" | "cloudflare" | "gh-pages";
  /** Adapter-specific options (e.g. vercel: cleanUrls, trailingSlash). */
  options?: Record<string, string>;
}

/** One user-agent group in a robots.txt (`robots({ rules: [...] })`). */
export interface RobotsRule {
  /** User-agent token (default "*"). */
  userAgent?: string;
  /** Paths to allow. */
  allow?: string[];
  /** Paths to disallow. */
  disallow?: string[];
}

/** Options for the built-in robots plugin. */
export interface RobotsPluginOptions {
  /** Allow a single path (default "/"). */
  allow?: string;
  /** Disallow a single path. */
  disallow?: string;
  /** Sitemap URL (defaults to `<site.url>/sitemap.xml`). */
  sitemap?: string;
  /** Multi user-agent groups (advanced). Overrides allow/disallow. */
  rules?: RobotsRule[];
}

/** Options for the built-in llms plugin (`llms.txt` / `llms-full.txt`). */
export interface LLMsPluginOptions {
  /** Falls back to `site.url` / `seo.baseUrl`. */
  baseUrl?: string;
  /** Docs content directory (defaults to the docs plugin's `contentDir`). */
  contentDir?: string;
  /** Title (falls back to the docs title or `site.name`). */
  title?: string;
  /** Description (falls back to `site.description`). */
  description?: string;
  /** URL prefix docs are mounted under (default "/docs"). */
  pathPrefix?: string;
  /** Emit `llms-full.txt` (concatenated page text). Default: true. */
  full?: boolean;
}

export interface DocsPluginOptions {
  /** Directory of markdown/mdx docs, relative to project root. */
  contentDir?: string;
  /** Site title shown in the docs layout. */
  title?: string;
  /**
   * Path to a layout component, relative to project root. The layout receives
   * a single {@link DocsLayoutProps} object and owns the docs chrome (navbar,
   * sidebar, TOC, breadcrumbs, prev/next, social links).
   */
  layout?: string;
  /**
   * Docs theme - the component every generated docs page is rendered through.
   * Like {@link layout}: a root-relative file path (`./` or `/`) or an npm
   * package name (a docs theme installed from a registry) - or a theme
   * descriptor returned by a theme factory (bare object). `theme` and
   * `layout` are aliases: set only one unless both resolve to the same
   * component.
   */
  theme?: DocsThemeDescriptor<DocsThemeOptions> | string;
  /** Custom sidebar override ("auto", "none", an explicit tree, or a JSON file). */
  sidebar?: DocsSidebarItem[] | "auto" | "none" | { from: "file"; file?: string };
  /** Social links rendered in the docs layout. */
  links?: { icon?: string; url?: string }[];
  /** Search bar configuration (docfind WASM search). */
  search?: DocsSearchOptions;
  /**
   * Base URL for "Edit this page" links (e.g. a GitHub blob URL). The per-page
   * frontmatter `editUrl` key overrides it.
   */
  editLinkBase?: string;
  /**
   * "Edit this page" link config. `pattern` supports `:path` (source path
   * relative to the project root), e.g.
   * `"https://github.com/org/repo/edit/main/:path"`.
   */
  editLink?: { base?: string; pattern?: string };
  /** Show the table of contents (default true). */
  toc?: boolean;
  /** Show breadcrumbs (default true). */
  breadcrumbs?: boolean;
  /** Show prev/next links (default true). */
  pagination?: boolean;
  /** Fill "last updated" from git history (default true). */
  lastUpdated?: boolean;
}

export type {
  DocsHeadTag,
  DocsHero,
  DocsLayoutProps,
  DocsSearchOptions,
  DocsSidebarItem,
  DocsThemeDescriptor,
  DocsThemeOptions,
} from "@krate/plugin";

export interface SitemapPluginOptions {
  /** e.g. "https://example.com" (falls back to `seo.baseUrl`). */
  baseUrl: string;
  /** always|hourly|daily|weekly|monthly|yearly|never (default: weekly). */
  changeFreq?: string;
  /** 0.0 - 1.0 (default: "0.5"). */
  priority?: string;
}

/** Build-time precompression (`.br`/`.gz` siblings). */
export interface CompressConfig {
  brotli?: boolean;
  gzip?: boolean;
}

/** Browser compatibility output mode. */
export interface CompatConfig {
  mode?: "modern" | "legacy" | "hybrid";
  /** Hybrid only: emit a legacy document chosen by User-Agent. */
  userAgent?: boolean;
}

/** CSS/JS dead-code elimination. */
export interface DceConfig {
  css?: boolean;
  js?: boolean;
  aggressive?: boolean;
}

/** Web-font preload + font-display. */
export interface FontsConfig {
  preload?: boolean;
  display?: string;
}

export interface GoDep {
  path: string;
  version: string;
}
export interface GoReplace {
  from: string;
  to: string;
}
export interface GoExclude {
  path: string;
  version: string;
}

/** Unified Go module hosting API routes + Go components. */
export interface GoConfig {
  enabled?: boolean;
  module?: string;
  deps?: GoDep[];
  replaces?: GoReplace[];
  exclude?: GoExclude[];
  tidy?: boolean;
  componentDirs?: string[];
  apiDirs?: string[];
  apiEnabled?: boolean;
}

/** Legacy Go API sidecar config (superseded by `go`). */
export interface GoAPIConfig {
  enabled?: boolean;
  module?: string;
  deps?: GoDep[];
  replaces?: GoReplace[];
  tidy?: boolean;
}

/** The full, type-checked Krate configuration surface. */
export interface KrateConfig {
  entry?: string;
  outDir?: string;
  /** Merge one or more base configs (paths or packages) underneath this one. */
  extends?: string | string[];
  pagesDir?: string;
  publicDir?: string;
  minify?: boolean;
  minifyHTML?: boolean;
  minifyCSS?: boolean;
  minifyJS?: boolean;
  sourcemap?: boolean;
  /** @deprecated React-to-krate transpilation is always enabled; accepted but ignored. */
  emitReact?: boolean;
  /**
   * URL path prefix the whole site is served under (e.g. "/docs"). Empty =
   * root. Used for emitted asset URLs and the SPA router.
   */
  basePath?: string;
  /**
   * View Transitions API integration. `true`/`"auto"` (default) enables
   * same-document SPA morphs and the cross-document `@view-transition` rule;
   * `false`/`"off"` disables it.
   */
  viewTransitions?: boolean | "auto" | "off";
  /**
   * Partial prerendering: cache dynamic regions on non-ISR pages with
   * stale-while-revalidate. `true` uses the default window; an object sets it.
   * Only enable for regions that do NOT depend on per-request cookies/sessions.
   */
  ppr?: boolean | { revalidate?: number };
  /**
   * Multi-language docs. Locales live in subdirectories of the docs content dir
   * (`src/content/docs/<locale>/...`); the default locale is unprefixed
   * (`/docs/...`) and others are path-prefixed (`/fr/docs/...`).
   */
  i18n?: {
    defaultLocale?: string;
    locales?: string[];
    routing?: "prefix";
  };
  /**
   * Versioned docs. Versions live in subdirectories
   * (`src/content/docs/<version>/...`); the current version is unprefixed.
   */
  versions?: {
    current?: string;
    versions?: string[];
    banner?: boolean;
  };
  devServer?: DevServerConfig;
  server?: ServerConfig;
  cors?: CORSConfig;
  api?: APIConfig;
  plugins?: PluginConfig[];
  tailwind?: TailwindConfig;
  csp?: CSPConfig;
  markdown?: MarkdownConfig;
  runtime?: "node" | "bun" | "deno";
  ssr?: SSRConfig;
  redirects?: RedirectConfig[];
  rewrites?: RewriteConfig[];
  seo?: SEOConfig;
  /** Canonical site metadata (supersedes `seo`). */
  site?: SiteConfig;
  robots?: RobotsConfig;
  /** Site-wide <head> injection (meta/links/scripts/raw). */
  head?: HeadConfig | string[];
  /** Security response-header overrides (`krate serve`). */
  security?: SecurityConfig;
  /** Custom response headers by path (`krate serve`). */
  headers?: HeaderRule[];
  /** Responsive-image pipeline (<Image>). */
  images?: ImagesConfig;
  /** Deploy adapter + options (`krate deploy`). */
  deploy?: DeployConfig;
  /** Build-time precompression. */
  compress?: CompressConfig;
  /** Browser compatibility output mode. */
  compat?: CompatConfig;
  /** CSS/JS dead-code elimination. */
  dce?: DceConfig;
  /** Web-font handling. */
  fonts?: FontsConfig;
  /** Unified Go module (API routes + components). */
  go?: GoConfig;
  /** Legacy Go API config (superseded by `go`). */
  goApi?: GoAPIConfig;
  serverComponents?: string[];
  runtimeComponents?: string[];
  serverDirs?: string[];
  runtimeDirs?: string[];
  pathAliases?: Record<string, string[]>;
  tsBaseDir?: string;

  /**
   * Output mode. Default (`undefined`) allows request-time rendering
   * (SSR/ISR/streaming and dynamic route fallbacks). `"static"` produces a
   * fully static build: dynamic routes render only the params returned by
   * `generateStaticParams`, unknown params 404, and SSR/ISR/streaming are
   * disabled. A page can opt back in with `export const dynamicParams = true`.
   */
  output?: "static";

  /**
   * Compiler-enforced quality gates. When configured, `krate build` runs the
   * rules and fails on findings at or above `failOn`; `krate check` runs the
   * same rules on an existing build. Categories run by default only when a
   * `checks` object is present.
   */
  checks?: ChecksConfig;

  /**
   * Typed content collections. Either a plain object or `defineContent({...})`:
   *
   * ```ts
   * export default defineConfig({
   *   content: defineContent({
   *     blog: { dir: "src/content/blog", schema: { title: "string" } },
   *   }),
   * });
   * ```
   */
  content?: ContentConfig;

  /** Optional validation function called at build time with the loaded config. */
  validate?: (config: KrateConfig) => void | Promise<void>;
}

/** Context passed to a function-form `defineConfig`. */
export interface ConfigContext {
  /** Current command: "build" | "dev" | "serve" | ... */
  command: string;
  /** Resolved mode (production/development/custom). */
  mode: string;
  /** True when mode is development or the command is dev. */
  isDev: boolean;
}

/**
 * Identity helper that gives `krate.config.ts` full type-checking. Unknown
 * config keys become compile errors. Accepts either the config object or a
 * function invoked with the build {@link ConfigContext} (e.g. to vary settings
 * per mode).
 *
 * ```ts
 * export default defineConfig(({ isDev }) => ({
 *   seo: { baseUrl: isDev ? "http://localhost:3000" : "https://example.com" },
 * }));
 * ```
 */
export function defineConfig(config: KrateConfig): KrateConfig;
export function defineConfig(config: (env: ConfigContext) => KrateConfig): (env: ConfigContext) => KrateConfig;
export function defineConfig(config: KrateConfig | ((env: ConfigContext) => KrateConfig)) {
  return config;
}

export interface FeedPluginOptions {
  /** e.g. "https://example.com" (falls back to `seo.baseUrl`). */
  baseUrl?: string;
  /** Docs content directory (defaults to the docs plugin's `contentDir`). */
  contentDir?: string;
  /** Feed title (defaults to the docs title or `seo.siteName`). */
  title?: string;
  /** Feed description (defaults to `seo.description`). */
  description?: string;
  /** Feed language (default "en"). */
  language?: string;
  /** Which feeds to emit: "rss" | "atom" | "json" | "all" (default "all"). */
  type?: "rss" | "atom" | "json" | "all";
  /** Maximum number of items (default 20). */
  count?: number;
  /** URL prefix docs are mounted under (default "/docs"). */
  pathPrefix?: string;
}

/**
 * Built-in sitemap plugin. Generates `sitemap.xml` after the build.
 */
export function sitemap(options: SitemapPluginOptions): PluginConfig {
  return { name: "sitemap", options };
}

/**
 * Built-in feed plugin. Generates `feed.xml` (RSS), `atom.xml`, and
 * `feed.json` from the docs content, and adds `<link rel="alternate">` feed
 * discovery plus JSON-LD structured data to docs pages.
 */
export function feed(options: FeedPluginOptions = {}): PluginConfig {
  return { name: "feed", options };
}

/**
 * Built-in docs plugin. Generates documentation pages from a markdown/mdx
 * content directory.
 */
export function docs(options: DocsPluginOptions = {}): PluginConfig {
  return { name: "docs", order: 10, options };
}

/**
 * Built-in robots plugin. Generates `robots.txt` after the build. Options
 * override the top-level `robots` config block; both fall back to `site.url`
 * for the sitemap line.
 */
export function robots(options: RobotsPluginOptions = {}): PluginConfig {
  return { name: "robots", options };
}

/**
 * Built-in llms plugin. Generates `llms.txt` + `llms-full.txt` for LLM
 * consumption from the docs content.
 */
export function llms(options: LLMsPluginOptions = {}): PluginConfig {
  return { name: "llms", options };
}

// Content collections

/** Shorthand field types accepted in a collection schema. */
export type ContentFieldType =
  | "string"
  | "number"
  | "boolean"
  | "string[]"
  | "number[]"
  | "date";

/** Object form of a schema field. */
export interface ContentField {
  type: ContentFieldType;
  /** Require the field to be present in frontmatter (default: false). */
  required?: boolean;
  /** Restrict a string field to these values. */
  enum?: string[];
  /** Value used when frontmatter omits the field. */
  default?: unknown;
}

/** One field: a shorthand type string or a {@link ContentField} object. */
export type ContentFieldSpec = ContentFieldType | ContentField;

/** A single content collection. */
export interface ContentCollection {
  /** Directory containing the collection's markdown/mdx entries, relative to
   * the project root (default: `src/content/<name>`). */
  dir?: string;
  /** Frontmatter schema; each key maps to a field spec. */
  schema?: Record<string, ContentFieldSpec>;
}

/** The shape passed to {@link defineContent}. */
export type ContentConfig = Record<string, ContentCollection>;

/**
 * Define typed content collections. Optional identity helper - you can also
 * pass a plain object to `defineConfig({ content: {...} })`; it exists purely
 * for type-checking and editor assistance.
 *
 * Krate validates each entry's frontmatter against the schema at build time and
 * generates typed declarations (`.krate/types/content.d.ts`).
 *
 * ```ts
 * import { defineConfig, defineContent } from "@krate/core";
 *
 * export default defineConfig({
 *   content: defineContent({
 *     blog: {
 *       dir: "src/content/blog",
 *       schema: {
 *         title: "string",
 *         order: { type: "number", required: true },
 *         tags: "string[]",
 *       },
 *     },
 *   }),
 * });
 * ```
 */
export function defineContent(config: ContentConfig): ContentConfig {
  return config;
}

// Quality gates (checks)

/** Severity for a check rule or category. `false` disables it. */
export type CheckSeverity = "error" | "warning" | "off" | boolean;

/** Built-in check rule IDs. */
export type CheckRuleId =
  | "a11y/img-alt"
  | "a11y/heading-order"
  | "a11y/accessible-name"
  | "a11y/duplicate-id"
  | "a11y/landmark"
  | "a11y/form-label"
  | "a11y/tabindex"
  | "a11y/aria-role"
  | "a11y/color-contrast"
  | "a11y/broken-anchor"
  | "seo/title"
  | "seo/description"
  | "seo/canonical"
  | "seo/og"
  | "seo/og-image"
  | "seo/lang"
  | "seo/broken-link"
  | "seo/duplicate-meta"
  | "perf/js-budget"
  | "perf/image-dims";

export interface ChecksConfig {
  /** Master switch (default: true when `checks` is present). */
  enabled?: boolean;
  /** Accessibility rules. `false` turns the category off. */
  a11y?: CheckSeverity;
  /** SEO rules. */
  seo?: CheckSeverity;
  /** Performance-budget rules. */
  perf?: CheckSeverity;
  /** Per-rule severity overrides, keyed by rule ID. */
  rules?: Partial<Record<CheckRuleId, CheckSeverity>>;
  /** Rule IDs to suppress entirely. */
  ignore?: CheckRuleId[];
  /** Per-route budgets. `js` is the client-JS budget in kilobytes. */
  budget?: { js?: number };
  /** Minimum severity that fails `krate check` / the build (default "error"). */
  failOn?: "error" | "warning";
  /**
   * Paths (relative to the project root) to JS/TS modules that export a custom
   * `check(page, krate)` rule. Runs in the embedded QuickJS runtime.
   */
  custom?: string[];
}
