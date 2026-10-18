/**
 * Markdown/MDX plugin helpers.
 *
 * A markdown plugin is a module whose default export is created with
 * {@link defineMarkdownPlugin}. List it under `markdown.plugins` in
 * krate.config.ts and Krate runs it on the AfterMarkdownParse hook, where it can
 * rewrite the rendered HTML:
 *
 * ```ts
 * import { defineMarkdownPlugin } from "@krate/plugin";
 *
 * export default defineMarkdownPlugin((html) => html.replace(/TODO/g, "DONE"));
 * ```
 */

/** Context passed to a markdown transform. */
export interface MarkdownPluginContext {
  /** Source page path (e.g. "src/content/docs/guide.md"). */
  page: string;
  /** Rendered markdown HTML (mutable via the returned value). */
  html: string;
  /** Page title (may be empty for some pages). */
  title?: string;
  /** Output route (e.g. "docs/guide"). */
  route: string;
}

/** A transform receives the HTML and returns replacement HTML (or it). */
export type MarkdownTransform = (
  html: string,
  ctx: MarkdownPluginContext,
) => string | void | Promise<string | void>;

/** The shape Krate expects from a markdown plugin module's default export. */
export interface MarkdownPluginDescriptor {
  name?: string;
  hooks: {
    AfterMarkdownParse: (ctx: MarkdownPluginContext) => unknown;
  };
}

/**
 * Wrap a transform as a Krate plugin descriptor. Async transforms are awaited by
 * the hook runtime.
 */
export function defineMarkdownPlugin(transform: MarkdownTransform): MarkdownPluginDescriptor {
  return {
    hooks: {
      AfterMarkdownParse(ctx: MarkdownPluginContext) {
        const out = transform(ctx.html, ctx);
        const apply = (v: string | void) => (typeof v === "string" ? { html: v } : undefined);
        if (out && typeof (out as Promise<string | void>).then === "function") {
          return (out as Promise<string | void>).then(apply);
        }
        return apply(out as string | void);
      },
    },
  };
}
