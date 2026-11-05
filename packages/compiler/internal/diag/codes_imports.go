package diag

// KR712x: imports the compiler can see are wrong before the page runs.
const (
	// CodeUnsupportedStylesheet: a Sass, Less or Stylus file is imported.
	CodeUnsupportedStylesheet Code = "KR7123"
	// CodeRuntimeExportMissing: a name is imported from @krate/runtime that
	// the package does not export.
	CodeRuntimeExportMissing Code = "KR7124"
	// CodeAssetBindingUnresolved: an imported asset (image, font, ...) is read
	// where the compiler cannot replace the binding with the file's URL.
	CodeAssetBindingUnresolved Code = "KR7125"
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
		CodeInfo{
			Code:  CodeAssetBindingUnresolved,
			Title: "Imported asset is used where its URL cannot be inlined",
			Explanation: "`import logo from \"./logo.png\"` binds the file's hashed URL. The compiler replaces " +
				"reads of the binding in the module that imports the file, so a binding that another module " +
				"re-exports (`export { logo }`), or a read the rewrite cannot reach, has no value in the " +
				"browser script: it is a ReferenceError there, or renders as the bare name. Import the asset " +
				"directly where it is used, or export the URL as a string constant. This is a warning.",
		},
	)
}
