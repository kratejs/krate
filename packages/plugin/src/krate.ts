/**
 * The `krate` object passed as the third argument to every JS plugin hook.
 *
 * It carries build-wide metadata plus a small capability surface so a plugin
 * does not need raw `fs` guesswork or a hand-rolled `node_modules` walk. Every
 * file path is anchored to the project root and traversal outside it is
 * rejected by the host.
 *
 * The same capabilities exist on the Go SDK (`pluginsdk`) as `KrateInfo` fields
 * and the `EmitFile`/`InjectHead`/`InjectCSS`/`ResolveFile`/`ReadFile`/
 * `WriteFileToRoot` helpers, so both runtimes expose the same surface.
 */
export interface Krate {
  /** Absolute path to the project root. */
  root: string;
  /** Absolute path to the project root (alias of {@link Krate.root}). */
  projectRoot: string;
  /** Absolute path to the output directory. */
  outDir: string;
  /** Absolute path to the configured pages directory. */
  pagesDir: string;
  /** The resolved krate config object. */
  config: unknown;
  /** True during `krate dev`. */
  dev: boolean;
  /** True during `krate dev` (alias of {@link Krate.dev}). */
  devMode: boolean;
  /** Current page list (paths; the full built set at AfterBuild). */
  pages: string[];
  /** Running Krate compiler version. */
  version: string;

  /**
   * Resolve a file specifier to an absolute path: bare specifiers resolve
   * through `node_modules` (walking up from the project root), while relative
   * paths resolve against the root. Throws if it cannot be resolved or escapes
   * the project root.
   */
  resolveFile(spec: string): string;

  /**
   * Read a file resolved like {@link Krate.resolveFile} and return its contents
   * as a UTF-8 string.
   */
  readFile(spec: string): string;

  /**
   * Append a file to the build output. `path` is output-directory relative and
   * cannot escape it. Convenience over returning `{ files: [...] }`.
   */
  emitFile(path: string, content: string): void;

  /**
   * Write a file relative to the project root (for example `public/logo.svg`),
   * creating parent directories. Distinct from {@link Krate.emitFile}, which
   * targets the output directory. Cannot escape the project root.
   */
  writeFileToRoot(rel: string, content: string): void;

  /**
   * Append markup to the page `<head>` (effective in AfterRender/AfterPage).
   * Returns the accumulated head HTML and CSS so far.
   */
  injectHead(html: string): { headHTML: string; rawCSS: string };

  /**
   * Append raw CSS (effective in AfterRender). Returns the accumulated head HTML
   * and CSS so far.
   */
  injectCSS(css: string): { headHTML: string; rawCSS: string };

  /**
   * Diagnostic log line, prefixed with `[plugin:<name>]`. Shown only when the
   * build runs with `--verbose`.
   */
  log(...args: unknown[]): void;

  /**
   * Warning line written to stderr, prefixed with `[plugin:<name>]`. Always
   * shown (not gated by `--verbose`).
   */
  warn(...args: unknown[]): void;
}
