package diag

// KR715x: code highlighting configuration.
const (
	// CodeUnknownCodeTheme: markdown.codeTheme names a theme chroma does not have.
	CodeUnknownCodeTheme Code = "KR7150"
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
	)
}
