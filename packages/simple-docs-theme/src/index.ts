import { defineDocsTheme } from "@krate/plugin";

export interface SimpleDocsThemeOptions {
  /** Optional URL for a search page or external search UI. */
  searchHref?: string;
  /** "Was this helpful?" feedback, opening a prefilled GitHub issue. */
  feedback?: {
    repo: string;
    discussions?: boolean;
    label?: string;
  };
  /** Label for the edit link (default `Edit this page`). */
  editLabel?: string;
  /** Forward-compatible escape hatch for future options. */
  [key: string]: unknown;
}

/**
 * A zero-JS docs theme. Interactive chrome uses CSS signals and native
 * `<details>` elements, so pages ship no theme runtime at all.
 *
 * ```ts
 * import { docs } from "@krate/core";
 * import { simpleDocsTheme } from "@krate/simple-docs-theme";
 *
 * docs({
 *   contentDir: "src/content/docs",
 *   theme: simpleDocsTheme(),
 * });
 * ```
 */
export function simpleDocsTheme(options: SimpleDocsThemeOptions = {}) {
  return defineDocsTheme({
    name: "@krate/simple-docs-theme",
    module: new URL("./layout.tsx", import.meta.url).href,
    options,
  });
}

export default simpleDocsTheme;
