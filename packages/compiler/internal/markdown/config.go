package markdown

import "encoding/json"

type Config struct {
	Root           string `json:"root"`           // root directory of the project, used for resolving relative paths
	GFM            bool   `json:"gfm"`            // GitHub Flavored Markdown (tables, task lists, strikethrough)
	HeadingAnchors bool   `json:"headingAnchors"` // auto-generate id attributes on headings
	Admonitions    bool   `json:"admonitions"`    // :::note, :::tip, :::warning, :::danger
	CodeHighlight  bool   `json:"codeHighlight"`  // syntax highlighting for code blocks
	CodeTheme      string `json:"codeTheme"`      // chroma theme for light mode, or for both modes when CodeThemeDark equals it (default: github)
	// CodeThemeDark is the chroma theme used in dark mode (default:
	// github-dark). Configured as `codeTheme: { light, dark }`.
	CodeThemeDark string `json:"-"`
	Math          bool   `json:"math"`    // $...$ and $$...$$ math support
	Mermaid       bool   `json:"mermaid"` // render ```mermaid blocks as Mermaid diagrams
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
		CodeTheme:      "github",
		CodeThemeDark:  "github-dark",
		Math:           false,
	}
}

// SetCodeTheme applies a configured `codeTheme` value: a theme name used for
// both colour modes, or an object with `light` and/or `dark` names (a missing
// side keeps its default). It reports false for any other shape.
func (c *Config) SetCodeTheme(v interface{}) bool {
	switch t := v.(type) {
	case string:
		if t == "" {
			return true
		}
		c.CodeTheme, c.CodeThemeDark = t, t
		return true
	case map[string]interface{}:
		light, dark := "github", "github-dark"
		if s, ok := t["light"].(string); ok && s != "" {
			light = s
		}
		if s, ok := t["dark"].(string); ok && s != "" {
			dark = s
		}
		c.CodeTheme, c.CodeThemeDark = light, dark
		return true
	}
	return false
}

// UnmarshalJSON accepts `codeTheme` as a string or a { light, dark } object.
func (c *Config) UnmarshalJSON(data []byte) error {
	type plain Config
	aux := struct {
		*plain
		CodeTheme json.RawMessage `json:"codeTheme"`
	}{plain: (*plain)(c)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if len(aux.CodeTheme) > 0 {
		var v interface{}
		if err := json.Unmarshal(aux.CodeTheme, &v); err != nil {
			return err
		}
		c.SetCodeTheme(v)
	}
	return nil
}
