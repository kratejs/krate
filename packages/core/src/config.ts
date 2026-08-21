/**
 * Typed configuration helpers for Krate.
 *
 * Use `defineConfig` in `krate.config.ts` to get full type-checking of every
 * supported config key — unknown or misspelled keys will fail to compile.
 *
 * ```ts
 * import { defineConfig, sitemap, docs } from '@krate/core';
 *
 * export default defineConfig({
 *   outDir: "dist",
 *   plugins: [
 *     sitemap({ baseUrl: "https://example.com" }),
 *     docs({ contentDir: "content/docs", title: "Docs" }),
 *   ],
 * });
 * ```
 */

export interface DevServerConfig {
  /** Dev server port (default: 3000). */
  port?: number;
  /** Open the browser on start (default: false). */
  open?: boolean;
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
}

export interface CSPConfig {
  enabled?: boolean;
  /** Custom CSP directive string. Empty = auto-generate. */
  directive?: string;
}

export interface MarkdownConfig {
  gfm?: boolean;
  headingAnchors?: boolean;
  admonitions?: boolean;
  codeHighlight?: boolean;
  math?: boolean;
}

export type RuntimeName = "quickjs" | "node" | "bun" | "deno";

export interface SSRConfig {
  rendererPort?: number;
  timeout?: number;
  maxCacheSize?: number;
  middlewareRuntime?: RuntimeName;
  apiRuntime?: RuntimeName;
  serverComponentRuntime?: RuntimeName;
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
}

export interface RobotsConfig {
  allow?: string;
  disallow?: string;
  sitemap?: string;
}

export interface DocsSidebarItem {
  title?: string;
  url?: string;
  children?: DocsSidebarItem[];
}

export interface DocsSearchOptions {
  /**
   * Turn the search bar on/off (default: true).
   */
  enabled?: boolean;
  /**
   * Index backend. "docfind" (default) builds a WASM search index with the
   * documents embedded in-process at build time; "json" uses the classic
   * search-index.json. The client falls back to JSON automatically when the
   * WASM index is unavailable.
   */
  engine?: "docfind" | "json";
  /**
   * Max number of results shown (default: 8).
   */
  maxResults?: number;
}

export interface DocsPluginOptions {
  /** Directory of markdown/mdx docs, relative to project root. */
  contentDir?: string;
  /** Site title shown in the docs layout. */
  title?: string;
  /** Path to a layout component, relative to project root. */
  layout?: string;
  /** Custom sidebar override. */
  sidebar?: DocsSidebarItem[];
  /** Social links rendered in the docs layout. */
  links?: { icon?: string; url?: string }[];
  /** Search bar configuration (docfind WASM search). */
  search?: DocsSearchOptions;
}

export interface SitemapPluginOptions {
  /** e.g. "https://example.com" (falls back to `seo.baseUrl`). */
  baseUrl: string;
  /** always|hourly|daily|weekly|monthly|yearly|never (default: weekly). */
  changeFreq?: string;
  /** 0.0 - 1.0 (default: "0.5"). */
  priority?: string;
}

/** The full, type-checked Krate configuration surface. */
export interface KrateConfig {
  entry?: string;
  outDir?: string;
  pagesDir?: string;
  publicDir?: string;
  minify?: boolean;
  minifyHTML?: boolean;
  minifyCSS?: boolean;
  minifyJS?: boolean;
  sourcemap?: boolean;
  emitReact?: boolean;
  devServer?: DevServerConfig;
  plugins?: PluginConfig[];
  tailwind?: TailwindConfig;
  csp?: CSPConfig;
  markdown?: MarkdownConfig;
  runtime?: "node" | "bun" | "deno";
  ssr?: SSRConfig;
  redirects?: RedirectConfig[];
  rewrites?: RewriteConfig[];
  seo?: SEOConfig;
  robots?: RobotsConfig;
  serverComponents?: string[];
  runtimeComponents?: string[];
  serverDirs?: string[];
  runtimeDirs?: string[];
  pathAliases?: Record<string, string[]>;
  tsBaseDir?: string;

  /** Optional validation function called at build time with the loaded config. */
  validate?: (config: KrateConfig) => void | Promise<void>;
}

/**
 * Identity helper that gives `krate.config.ts` full type-checking. Unknown
 * config keys become compile errors.
 */
export function defineConfig(config: KrateConfig): KrateConfig {
  return config;
}

/**
 * Built-in sitemap plugin. Generates `sitemap.xml` after the build.
 */
export function sitemap(options: SitemapPluginOptions): PluginConfig {
  return { name: "sitemap", options };
}

/**
 * Built-in docs plugin. Generates documentation pages from a markdown/mdx
 * content directory.
 */
export function docs(options: DocsPluginOptions = {}): PluginConfig {
  return { name: "docs", order: 10, options };
}
