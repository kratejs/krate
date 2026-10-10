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
import type { PluginHookFn, PluginOutput } from "./output.js";

/**
 * A hook map keyed by the Krate lifecycle hook names.
 *
 * The optional `Options` generic types the second argument every hook receives
 * (the per-plugin options object). Pass it to {@link definePluginHooks} so
 * `options` is typed instead of `unknown`:
 *
 * @example
 * interface MyOptions { greeting?: string }
 * export const hooks = definePluginHooks<MyOptions>({
 *   BeforeBuild(ctx, options) { return { html: options.greeting }; },
 * });
 */
export interface PluginHooks<Options = unknown> {
  /** Runs before any page is built. Generate pages / emit files. */
  BeforeBuild?: PluginHookFn<BuildContext, PluginOutput, Options>;
  /** Runs after a page's source parses into an AST. */
  AfterParse?: PluginHookFn<ParseContext, PluginOutput, Options>;
  /** Runs after a markdown page renders to HTML (pre-layout). */
  AfterMarkdownParse?: PluginHookFn<MarkdownContext, PluginOutput, Options>;
  /** Runs after a page renders to HTML, before layout wrapping. */
  AfterRender?: PluginHookFn<RenderContext, PluginOutput, Options>;
  /** Runs after BeforeBuild to synthesize virtual pages. */
  GenerateRoutes?: PluginHookFn<BuildContext, PluginOutput, Options>;
  /** Runs after a page is fully built including layout wrapping. */
  AfterPage?: PluginHookFn<PageContext, PluginOutput, Options>;
  /** Runs once after every page is built. */
  AfterBuild?: PluginHookFn<BuildResultContext, PluginOutput, Options>;
  /** Request-time, before a page is served. */
  ServeRequest?: PluginHookFn<ServeRequestContext, ServeRequestResult, Options>;
  /** Request-time, after a non-streaming response is buffered. */
  ServeResponse?: PluginHookFn<ServeResponseContext, ServeResponseResult, Options>;
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
 * Type helper for authoring a JS plugin's `hooks`. Runtime no-op - erased at
 * build - but gives full type-checking + intellisense for every hook's ctx.
 *
 * Pass your plugin's options type as the generic so the second hook argument
 * (`options`) is typed:
 *
 * @example
 * interface MyOptions { greeting?: string }
 * export const hooks = definePluginHooks<MyOptions>({
 *   BeforeBuild(ctx, options, krate) { return { files: [...] }; },
 *   AfterRender(ctx, options, krate) { return { rawCSS: options.greeting }; },
 * });
 */
export function definePluginHooks<Options = unknown>(
  hooks: PluginHooks<Options>,
): PluginHooks<Options> {
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
