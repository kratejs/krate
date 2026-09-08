/**
 * @krate/plugin — TypeScript types and helpers for writing Krate plugins.
 *
 * Covers JS community plugins (typed hook contexts + outputs) and Go-plugin
 * npm descriptors (runtime + binaries). Helpers are compile-time no-ops — they
 * are erased when the plugin is bundled into Krate's embedded runtime.
 */

export type {
  BuildContext,
  BuildResultContext,
  GeneratedPage,
  MarkdownContext,
  PageContext,
  PageResult,
  ParseContext,
  PluginFile,
  PluginRoute,
  RenderContext,
  ServeRequestContext,
  ServeRequestResult,
  ServeResponseContext,
  ServeResponseResult,
} from "./context.js";
export type { PluginHookFn, PluginOutput } from "./output.js";
export type { Krate } from "./krate.js";
export {
  defineGoPlugin,
  definePlugin,
  definePluginHooks,
} from "./plugin.js";
export type {
  GoPluginDescriptor,
  PluginDescriptor,
  PluginHooks,
} from "./plugin.js";
