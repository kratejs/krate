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
)

func init() {
	Register(
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
			Explanation: "Krate generates Tailwind utility CSS only when `tailwind: { enabled: true }` is set in " +
				"`krate.config.ts`. Without it, `@tailwind base|components|utilities` and `@import \"tailwindcss\"` " +
				"are removed from your CSS and no utilities are generated, so the page ships almost no styles. " +
				"The warning appears once per build when a `tailwind.config.{ts,js,mjs}` file exists or one of those " +
				"directives is found. Enable Tailwind, or delete the config file and directives if you do not use it. " +
				"This is a warning.",
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
