package markdown

type Config struct {
	Root           string `json:"root"`           // root directory of the project, used for resolving relative paths
	GFM            bool   `json:"gfm"`            // GitHub Flavored Markdown (tables, task lists, strikethrough)
	HeadingAnchors bool   `json:"headingAnchors"` // auto-generate id attributes on headings
	Admonitions    bool   `json:"admonitions"`    // :::note, :::tip, :::warning, :::danger
	CodeHighlight  bool   `json:"codeHighlight"`  // syntax highlighting for code blocks
	CodeTheme      string `json:"codeTheme"`      // chroma theme name for syntax highlighting (default: github-dark)
	Math           bool   `json:"math"`           // $...$ and $$...$$ math support
	Mermaid        bool   `json:"mermaid"`        // render ```mermaid blocks as Mermaid diagrams
	// Plugins lists JS module paths whose default export is a markdown transform
	// (see defineMarkdownPlugin). Each is registered as a community plugin that
	// runs on AfterMarkdownParse, so it can rewrite rendered HTML.
	Plugins []string `json:"plugins,omitempty"`
	// ImageURL, when set, maps a relative image destination (./local.png) to a
	// site URL, typically after copying the file into the build output. It
	// reports false to leave the destination as written. Not part of the
	// user-facing config.
	ImageURL func(dest string) (string, bool) `json:"-"`
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
