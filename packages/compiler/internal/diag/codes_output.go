package diag

// KR714x: input that would otherwise make the build output silently wrong.
const (
	// CodeTailwindDisabled: Tailwind input is present but tailwind.enabled is off.
	CodeTailwindDisabled Code = "KR7140"
	// CodeCSSSyntax: unbalanced braces, or an unclosed comment or string, in authored CSS.
	CodeCSSSyntax Code = "KR7141"
	// CodeStaticParamsEntry: a generateStaticParams entry that cannot produce a page.
	CodeStaticParamsEntry Code = "KR7142"
	// CodeMiddlewareExport: a middleware file with no handler to run.
	CodeMiddlewareExport Code = "KR7143"
	// CodeBasePathURL: a plain tag with a root-relative URL under a base path.
	CodeBasePathURL Code = "KR7144"
	// CodeCSSImportMissing: an @import of a local stylesheet that does not exist.
	CodeCSSImportMissing Code = "KR7145"
	// CodeChunkMissing: the build output references a /chunks/ file it did not write.
	CodeChunkMissing Code = "KR7146"
	// CodePublicStylesheet: a page links a .css file from public/ with rel=stylesheet.
	CodePublicStylesheet Code = "KR7147"
	// CodeDynamicRouteUnserved: a dynamic route with closed params that has no page and no server route.
	CodeDynamicRouteUnserved Code = "KR7148"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeDynamicRouteUnserved,
			Title: "Dynamic route has no page to serve",
			Explanation: "A dynamic route whose params are closed (`export const dynamicParams = false`, or `output: \"static\"`) " +
				"is served only by the pages `generateStaticParams` lists: prerendered files, or server routes when the " +
				"page or one of its layouts renders per request. Without a `generateStaticParams` the build can name no " +
				"param, so the route would answer 404 for every URL and nothing would be written. This is a build " +
				"error. Export `generateStaticParams` from the page (as a function or a const), or remove " +
				"`dynamicParams = false` so the route renders any param on demand.",
		},
		CodeInfo{
			Code:  CodePublicStylesheet,
			Title: "Stylesheet linked from public/ is not minified or hashed",
			Explanation: "A page or layout links a `.css` file that lives in `public/` with `<link rel=\"stylesheet\">`. " +
				"Files in `public/` are copied as written, so the stylesheet is neither minified nor given a content " +
				"hash, cannot be cached as immutable, and is a separate render-blocking request in front of the " +
				"bundled styles. Move the file under `src/` and import it from the layout or page " +
				"(`import \"../styles/site.css\"`) so it goes through the CSS pipeline. Import it before other " +
				"stylesheets if it used to be linked before them, since the bundled order follows import order. " +
				"This is a warning.",
		},
		CodeInfo{
			Code:  CodeChunkMissing,
			Title: "Build output references a chunk that was not written",
			Explanation: "After writing the output, Krate checks that every quoted `/chunks/...` URL in the page HTML, " +
				"page and island scripts, server bundles and manifests names a file in the `chunks` directory. A " +
				"missing chunk means the browser gets a 404 for it, so the island or page script that imports it " +
				"never runs. This points at a bug in Krate, not in the app: the build fails instead of shipping the " +
				"broken page. Files copied from the public directory are not checked. Report the error with the URLs " +
				"it lists. This is an error.",
		},
		CodeInfo{
			Code:  CodeBasePathURL,
			Title: "Root-relative URL on a plain tag ignores the base path",
			Explanation: "The site is served under a base path, but a plain `<a href=\"/x\">` or `<img src=\"/x\">` " +
				"is emitted exactly as written, so the browser requests it from the domain root and misses the " +
				"site. Use `<Link href=\"/x\">` for pages, and `<Image>` or an import for images: both add the base " +
				"path. Write the full path only when the URL really does belong outside the site.",
		},
		CodeInfo{
			Code:  CodeTailwindDisabled,
			Title: "Tailwind input found but Tailwind is not enabled",
			Explanation: "A project stylesheet with `@import \"tailwindcss\"` or `@tailwind` turns Tailwind on, " +
				"unless `krate.config.ts` sets `tailwind: { enabled: false }`. While Tailwind is off, Krate generates " +
				"no Tailwind CSS: no preflight reset and no utilities, so buttons and form controls keep the browser's " +
				"default look and utility classes do nothing. The warning appears once per build when Tailwind is off " +
				"and a `tailwind.config.{ts,js,mjs}` file exists, a directive is turned off by the config, or a " +
				"directive was added while `krate dev` was running (restart it). Enable Tailwind, or delete the config " +
				"file and directives if you do not use it. This is a warning.",
		},
		CodeInfo{
			Code:  CodeCSSImportMissing,
			Title: "CSS @import points at a file that does not exist",
			Explanation: "A relative or package `@import` in a stylesheet could not be resolved, for example " +
				"`@import './missing.css'` after the file was moved or deleted. Krate cannot inline it, and emitting " +
				"it as written would make the browser request a URL that 404s, so the build fails with the file, " +
				"line and column. Fix the path or restore the file. Remote `@import url(https://...)` statements " +
				"are passed through unchanged, and imports outside the project root keep their separate warning " +
				"(see `cssImportRoots`). This is an error.",
		},
		CodeInfo{
			Code:  CodeCSSSyntax,
			Title: "CSS has unbalanced braces or an unclosed comment",
			Explanation: "A stylesheet with a `{` that is never closed, a `}` with no `{`, or a comment that never ends " +
				"is not valid CSS, and browsers recover by dropping or merging the rules that follow it. Krate reports " +
				"the line and column, and closes the damage at the end of the file so it cannot swallow the next " +
				"stylesheet in the page: a stray `}` is removed and a missing one is added. The file is still shipped " +
				"as written otherwise, so fix the source. This is a warning.",
		},
		CodeInfo{
			Code:  CodeStaticParamsEntry,
			Title: "generateStaticParams entry cannot produce a page",
			Explanation: "Each object returned by `generateStaticParams` becomes one page, so it needs a value for every " +
				"dynamic segment of the route (`{ slug: \"a\" }` for `[slug].tsx`), and each value becomes a directory " +
				"name: it must be non-empty and contain no `..`, backslash or, outside a catch-all route, `/`. An entry " +
				"that misses a param would have been written to the literal path `[slug]/index.html`; two different " +
				"entries that produce the same path would overwrite each other. Both are build errors that name the " +
				"entry. An exact duplicate entry is dropped. Fix the function, or remove the entry.",
		},
		CodeInfo{
			Code:  CodeMiddlewareExport,
			Title: "Middleware file has no handler",
			Explanation: "`middleware.ts` must export the function that runs for each request, as " +
				"`export function middleware(request)` or as the default export. Without one, requests could not be " +
				"checked, so this is a build error, and a server running such a module answers 503 instead of letting " +
				"requests through. Export the handler, or delete the file.",
		},
	)
}
