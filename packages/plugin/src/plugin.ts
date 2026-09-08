/**
 * Plugin descriptor + helper types for Krate community plugins.
 *
 * A JS community plugin is a module that exports:
 *   - a `hooks` object keyed by hook name, and/or
 *   - a `default` factory `(options?) => descriptor` used from krate.config.ts.
 *
 * Krate discovers hooks in this order (see internal/plugin/jsplugin.go):
 *   plugin.hooks  ->  mod.hooks  ->  plugin itself (single-hook shorthand)
 * where `plugin` is `mod.default` (or `mod` when no default) and, if it is a
 * function, it is called with the plugin options first.
 */

import type {
  BuildContext,
  BuildResultContext,
  MarkdownContext,
  PageContext,
  ParseContext,
  RenderContext,
  ServeRequestContext,
  ServeRequestResult,
  ServeResponseContext,
  ServeResponseResult,
} from "./context.js";
import type { PluginHookFn } from "./output.js";

/** A hook map keyed by the Krate lifecycle hook names. */
export interface PluginHooks {
  /** Runs before any page is built. Generate pages / emit files. */
  BeforeBuild?: PluginHookFn<BuildContext>;
  /** Runs after a page's source parses into an AST. */
  AfterParse?: PluginHookFn<ParseContext>;
  /** Runs after a markdown page renders to HTML (pre-layout). */
  AfterMarkdownParse?: PluginHookFn<MarkdownContext>;
  /** Runs after a page renders to HTML, before layout wrapping. */
  AfterRender?: PluginHookFn<RenderContext>;
  /** Runs after BeforeBuild to synthesize virtual pages. */
  GenerateRoutes?: PluginHookFn<BuildContext>;
  /** Runs after a page is fully built including layout wrapping. */
  AfterPage?: PluginHookFn<PageContext>;
  /** Runs once after every page is built. */
  AfterBuild?: PluginHookFn<BuildResultContext>;
  /** Request-time, before a page is served. */
  ServeRequest?: PluginHookFn<ServeRequestContext, ServeRequestResult>;
  /** Request-time, after a non-streaming response is buffered. */
  ServeResponse?: PluginHookFn<ServeResponseContext, ServeResponseResult>;
}

/** What a plugin factory returns (lands in the config `plugins[]`). */
export interface PluginDescriptor<Options = Record<string, unknown>> {
  /** Unique plugin name (defaults from config entry if omitted). */
  name?: string;
  /** Execution priority (lower runs first, default 50). */
  order?: number;
  /**
   * Path or file:// URL to the plugin module so Krate can bundle it. Filled
   * automatically by the factory via `import.meta.url`.
   */
  module?: string;
  /** Per-plugin options forwarded to each hook as the 2nd argument. */
  options?: Options;
}

/**
 * Go-plugin npm descriptor: the `module.exports` factory shape that declares a
 * native runtime + per-platform binaries. Krate discovers this by bundling and
 * running the module once (internal/plugin/gohook.go `runJSManifest`).
 */
export interface GoPluginDescriptor<Options = Record<string, unknown>>
  extends PluginDescriptor<Options> {
  runtime: "go";
  /** GOOS-GOARCH (e.g. "darwin-arm64") -> binary path relative to the package. */
  binaries: Record<string, string>;
}

/**
 * Identity helper that type-checks a JS plugin factory's return descriptor.
 * Use it inside a plugin module to build the default export factory:
 *
 * @example
 * export default function myPlugin(options: MyOptions = {}) {
 *   return definePlugin({
 *     name: "my-plugin",
 *     order: 10,
 *     module: import.meta.url,
 *     options,
 *   });
 * }
 */
export function definePlugin<Options = Record<string, unknown>>(
  descriptor: PluginDescriptor<Options>,
): PluginDescriptor<Options> {
  return descriptor;
}

/**
 * Type helper for authoring a JS plugin's `hooks`. Runtime no-op — erased at
 * build — but gives full type-checking + intellisense for every hook's ctx.
 *
 * @example
 * export const hooks = definePluginHooks({
 *   BeforeBuild(ctx, options, krate) { return { files: [...] }; },
 *   AfterRender(ctx, options, krate) { return { rawCSS: "..." }; },
 * });
 */
export function definePluginHooks(hooks: PluginHooks): PluginHooks {
  return hooks;
}

/**
 * Identity helper for authoring a Go-plugin npm descriptor (the `module.exports`
 * factory shape that declares `runtime: "go"` + per-platform binaries).
 *
 * @example
 * export default function demoGoPlugin() {
 *   return defineGoPlugin({
 *     name: "demo-go",
 *     runtime: "go",
 *     binaries: { "windows-amd64": "bin/demo.exe", "darwin-arm64": "bin/demo" },
 *     module: import.meta.url,
 *   });
 * }
 */
export function defineGoPlugin<Options = Record<string, unknown>>(
  descriptor: GoPluginDescriptor<Options>,
): GoPluginDescriptor<Options> {
  return descriptor;
}
