package diag

// KR835x: build hardening checks that turn a silent mistake into an error.
const (
	// CodeUnsafeOutDir: an outDir that is, or contains, the project's sources.
	CodeUnsafeOutDir Code = "KR8350"
	// CodeScopePartUnpaired: a trigger or content of a CSS-signal widget
	// whose value no counterpart answers.
	CodeScopePartUnpaired Code = "KR8351"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeUnsafeOutDir,
			Title: "outDir would delete project sources",
			Explanation: "The build empties outDir before it writes the site, and `krate clean` deletes it. An outDir " +
				"that is, or contains, the project root, the pages directory or the public directory (`outDir: \"..\"`, " +
				"`\".\"`, `\"src\"`) would delete the project's own files, so the build stops before anything is " +
				"removed. Point outDir at a dedicated directory such as `dist`; a directory outside the project that " +
				"holds none of your sources, such as `../site-dist`, is fine. Setting KRATE_ALLOW_UNSAFE_OUTDIR=1 " +
				"skips this check.",
		},
		CodeInfo{
			Code:  CodeScopePartUnpaired,
			Title: "Trigger and content do not share a value",
			Explanation: "`Dialog`, `Sheet`, `Popover`, `DropdownMenu` and the other widgets built on a CSS-signal " +
				"scope connect a trigger to its content by `value`: `<Dialog.Trigger value=\"a\">` opens " +
				"`<Dialog.Content value=\"a\">`. A trigger and a content that both leave `value` off are paired " +
				"with each other (the value is `on`). This one has no value, but the widget has no part without a " +
				"value to answer it (every content has a value, or every trigger does), so it would never open or " +
				"show anything. Give the trigger and the content the same `value`, or remove it from both.",
		},
	)
}
