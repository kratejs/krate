import { defineDocsTheme } from "@krate/plugin";

export {
  defineSite,
  SiteHeader,
  SiteFooter,
  HomeLayout,
  ThemeToggle,
} from "./shell";
export type {
  SiteConfig,
  SiteNavLink,
  SiteSocial,
  SiteFooterColumn,
} from "./shell";
export { DocsSearch, DocsSearchComponent } from "./search";
export type { DocsSearchProps } from "./search";

/**
 * Options for the base docs theme.
 */
export interface BaseDocsThemeOptions {
  /** localStorage key used to persist the light/dark choice (default `theme`). */
  themeStorageKey?: string;
  /** Forward-compatible escape hatch for future options. */
  [key: string]: unknown;
}

/**
 * The default Krate docs theme. Pass the factory result to the docs plugin:
 *
 * ```ts
 * import { docs } from "@krate/core";
 * import { baseDocsTheme } from "@krate/base-docs-theme";
 *
 * docs({
 *   contentDir: "src/content/docs",
 *   theme: baseDocsTheme(),
 * })
 * ```
 */
export function baseDocsTheme(options: BaseDocsThemeOptions = {}) {
  return defineDocsTheme({
    name: "@krate/base-docs-theme",
    module: new URL("./layout.tsx", import.meta.url).href,
    options,
  });
}

export default baseDocsTheme;