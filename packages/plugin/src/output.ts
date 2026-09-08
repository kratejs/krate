/**
 * Output types for Krate JS plugin hooks.
 *
 * A hook returns a {@link PluginOutput} (or a Promise of one). Krate applies
 * the returned fields back onto the build through the same path used by
 * built-in and Go plugins (internal/plugin/exec.go `applyPluginOutput`):
 * `files` are written into outDir, `routes` become static pages, `html` /
 * `headHTML` / `rawCSS` replace/extend the page, `scripts` / `metaTags` are
 * injected into `<head>`, and `generatedPages` enter the page pipeline.
 */

import type { GeneratedPage, PluginFile, PluginRoute } from "./context.js";
import type { Krate } from "./krate.js";

/** Optional shape a build hook may return to affect the build. */
export interface PluginOutput {
  /** Files to write into the output directory (outDir-relative paths). */
  files?: PluginFile[];
  /** Virtual pages to synthesize (GenerateRoutes). */
  routes?: PluginRoute[];
  /** Page files that should enter the normal page pipeline. */
  generatedPages?: GeneratedPage[];
  /** Replace the page HTML (AfterMarkdownParse/AfterRender/AfterPage). */
  html?: string;
  /** Additional head HTML (appended). */
  headHTML?: string;
  /** Additional raw CSS (appended). */
  rawCSS?: string;
  /** Script URLs injected as <script src>. */
  scripts?: string[];
  /** Meta tags injected as <meta ...> (full attribute markup). */
  metaTags?: string[];
}

/** A build hook implementation; may return a Promise. */
export type PluginHookFn<Ctx, Out = PluginOutput> = (
  ctx: Ctx,
  options: unknown,
  krate: Krate,
) => Out | void | Promise<Out | void>;
