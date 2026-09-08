/**
 * Hook context types for Krate JS plugins.
 *
 * These mirror the JSON that Krate actually sends to a plugin hook — the same
 * wire shapes used by the Go-side hook contexts (internal/plugin/*.go). A JS
 * hook is invoked as `fn(ctx, options, krate)` where `ctx` is one of these.
 *
 * Mutating a context field does NOT write back to the build by itself: Krate
 * applies the *returned* {@link PluginOutput} from the hook. Contexts are
 * read-mostly inputs describing the current build state.
 */

/** A single file written to the output directory (path is outDir-relative). */
export interface PluginFile {
  path: string;
  content: string;
}

/** A virtual page synthesized by a plugin (GenerateRoutes). */
export interface PluginRoute {
  path: string;
  content: string;
  title?: string;
  layout?: string;
  data?: Record<string, string>;
}

/** A generated page file that enters the normal page pipeline. */
export interface GeneratedPage {
  path: string;
  route: string;
}

/** One built page's result (AfterBuild ctx.pages). */
export interface PageResult {
  page: string;
  outName: string;
  html: string;
  headHTML: string;
  hasJS: boolean;
}

/** Context passed to BeforeBuild and GenerateRoutes. */
export interface BuildContext {
  /** Absolute path to the project root. */
  root: string;
  /** Absolute path to the output directory. */
  outDir: string;
  /** Resolved krate config object. */
  config?: unknown;
  /** Absolute paths of all source pages. */
  pages: string[];
  /** Mutable: append {@link GeneratedPage}s to create pages. */
  generatedPages?: GeneratedPage[];
  devMode: boolean;
}

/** Context passed to AfterParse. `program` is the AST document (kind-tagged). */
export interface ParseContext {
  page: string;
  program?: unknown;
}

/** Context passed to AfterMarkdownParse. */
export interface MarkdownContext {
  page: string;
  html: string;
  title: string;
  route: string;
}

/** Context passed to AfterRender (before layout wrapping). */
export interface RenderContext {
  page: string;
  html: string;
  headHTML: string;
  hasJS: boolean;
  rawCSS: string;
}

/** Context passed to AfterPage (after layout wrapping). */
export interface PageContext {
  page: string;
  outName: string;
  html: string;
  headHTML: string;
  hasJS: boolean;
}

/** Context passed to AfterBuild once after all pages are built. */
export interface BuildResultContext {
  root: string;
  outDir: string;
  config?: unknown;
  pages: PageResult[];
  css: string;
}

/** Request-time context for the ServeRequest hook. */
export interface ServeRequestContext {
  url: string;
  method: string;
  path: string;
  headers: Record<string, string>;
}

/** Request-time context for the ServeResponse hook. */
export interface ServeResponseContext {
  url: string;
  method: string;
  path: string;
  status: number;
  headers: Record<string, string>;
  body: string;
}

/** Result of the ServeRequest hook. */
export interface ServeRequestResult {
  /** "continue" (fall through), "rewrite" (newURL), or "respond". */
  action?: "continue" | "rewrite" | "respond";
  status?: number;
  headers?: Record<string, string>;
  body?: string;
  newURL?: string;
}

/** Result of the ServeResponse hook (override only the fields returned). */
export interface ServeResponseResult {
  status?: number;
  headers?: Record<string, string>;
  body?: string;
}
