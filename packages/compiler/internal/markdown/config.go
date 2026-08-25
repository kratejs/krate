package markdown

type Config struct {
	Root           string `json:"root"`            // root directory of the project, used for resolving relative paths
	GFM            bool   `json:"gfm"`             // GitHub Flavored Markdown (tables, task lists, strikethrough)
	HeadingAnchors bool   `json:"headingAnchors"`  // auto-generate id attributes on headings
	Admonitions    bool   `json:"admonitions"`     // :::note, :::tip, :::warning, :::danger
	CodeHighlight  bool   `json:"codeHighlight"`   // syntax highlighting for code blocks
	CodeTheme      string `json:"codeTheme"`       // chroma theme name for syntax highlighting (default: github-dark)
	Math           bool   `json:"math"`            // $...$ and $$...$$ math support
}

func DefaultConfig() Config {
	return Config{
		Root:           "",
		GFM:            true,
		HeadingAnchors: true,
		Admonitions:    true,
		CodeHighlight:  true,
		CodeTheme:      "github-dark",
		Math:           false,
	}
}
