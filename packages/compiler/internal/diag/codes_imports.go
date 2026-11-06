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
	CodeAssetBindingUnresolved Code = "KR7126"
	// CodeTSConfigBaseURL: tsconfig baseUrl names a subdirectory that the
	// paths targets repeat; the targets are read from the project root.
	CodeTSConfigBaseURL Code = "KR7125"
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
				"reads of the binding with that URL, in the module that imports the file and in every module " +
				"that imports it through `export { logo }`, `export default logo`, " +
				"`export { default as logo } from \"./logo.png\"`, `export *` or `export * as ns` and " +
				"`ns.logo`. A read it cannot reach, such as a computed property key (`{ [logo]: 1 }`) or " +
				"`ns.default` of a namespace that is passed on whole, has no value in the browser script: " +
				"it is a ReferenceError there, or renders as the bare name. Use the binding as a plain value " +
				"in JSX or a component, or import the file with a `?url` suffix. This is a warning.",
		},
		CodeInfo{
			Code:  CodeTSConfigBaseURL,
			Title: "tsconfig baseUrl and paths disagree",
			Explanation: "`compilerOptions.paths` targets resolve against `baseUrl`, in tsc and in Krate. A " +
				"`baseUrl` of `./src` together with `\"@/*\": [\"./src/*\"]` points at `src/src`, which tsc " +
				"cannot find. Krate 0.4 ignored `baseUrl` for aliases, so a project scaffolded then still has " +
				"this shape. Krate reads such a target relative to the project root when it exists there and " +
				"not under `baseUrl`, so the build works, and reports this warning once. `krate fix --apply` " +
				"sets `baseUrl` to `\".\"` when every target repeats the baseUrl directory, which also makes " +
				"`tsc` and your editor agree. A target that exists in neither place is still an error " +
				"(\"cannot resolve import ... matched tsconfig path alias\"). This is a warning.",
		},
	)
}
