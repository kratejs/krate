package diag

// KR712x: imports the compiler can see are wrong before the page runs.
const (
	// CodeUnsupportedStylesheet: a Sass, Less or Stylus file is imported.
	CodeUnsupportedStylesheet Code = "KR7120"
	// CodeRuntimeExportMissing: a name is imported from @krate/runtime that
	// the package does not export.
	CodeRuntimeExportMissing Code = "KR7121"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeUnsupportedStylesheet,
			Title: "Sass, Less and Stylus files are not compiled",
			Explanation: "Krate compiles CSS (including CSS modules and Tailwind) but has no Sass, Less or " +
				"Stylus compiler, so importing a `.scss`, `.sass`, `.less` or `.styl` file would ship no " +
				"styles at all. Compile the file to CSS first (for example with the `sass` CLI in a " +
				"prebuild script) and import the `.css` result, or port the styles to CSS or Tailwind. " +
				"This is an error.",
		},
		CodeInfo{
			Code:  CodeRuntimeExportMissing,
			Title: "Name is not exported by @krate/runtime",
			Explanation: "A named import from `@krate/runtime` (or one of its subpaths) asks for something the " +
				"package does not export. The import is removed from the page, so the name would be " +
				"undefined in the browser and fail with a ReferenceError. Check the spelling against the " +
				"runtime API reference; names from other frameworks (`signal`, `usePathname`) have Krate " +
				"equivalents such as `createSignal` and `getNavigationState`. This is an error.",
		},
	)
}
