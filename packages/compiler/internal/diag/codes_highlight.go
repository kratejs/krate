package diag

// KR715x: code highlighting configuration.
const (
	// CodeUnknownCodeTheme: markdown.codeTheme names a theme chroma does not have.
	CodeUnknownCodeTheme Code = "KR7150"
	// CodeHighlightLanguageUnavailable: code highlighted in the browser or at
	// request time uses a language the runtime tokenizer does not ship.
	CodeHighlightLanguageUnavailable Code = "KR7151"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeUnknownCodeTheme,
			Title: "Unknown code theme",
			Explanation: "`markdown.codeTheme` names a syntax theme that Krate does not ship, so code is " +
				"highlighted with the default theme for that colour mode instead (`github` for light, " +
				"`github-dark` for dark). Use a chroma style name such as `github`, `github-dark`, " +
				"`monokai`, `dracula` or `nord`. Give one name to use it in both modes, or " +
				"`{ light: \"github\", dark: \"github-dark\" }` to pick a theme per mode. This is a warning.",
		},
		CodeInfo{
			Code:  CodeHighlightLanguageUnavailable,
			Title: "Language cannot be highlighted after the build",
			Explanation: "Code that is highlighted in the browser (a `<Code>` whose text changes at run time) or at " +
				"request time is coloured by a small tokenizer that runs chroma's lexers exactly, and it ships " +
				"only for a fixed list of languages. The language used here is not on it, so the code is shown " +
				"as plain text. Build-time highlighting is unaffected and supports every chroma language. Use one " +
				"of the listed languages (`markdown.codeHighlight` docs), or make the code known at build time. " +
				"This is a warning.",
		},
	)
}
