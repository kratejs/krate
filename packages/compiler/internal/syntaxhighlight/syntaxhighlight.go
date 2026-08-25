package syntaxhighlight

import (
	"bytes"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

var formatter = html.New(
	html.WithClasses(true),
	html.WithLineNumbers(false),
)

// Highlight returns HTML with CSS class annotations for the given code and language.
// If lang is empty or unrecognized, the code is returned HTML-escaped as plain text.
// Returns only the inner token spans — callers wrap in <pre>/<code> as needed.
func Highlight(code string, lang string) string {
	lexer := chroma.Lexer(nil)
	if lang != "" {
		l := lexers.Get(lang)
		if l != nil {
			lexer = l
		}
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}

	iter, err := lexer.Tokenise(nil, code)
	if err != nil {
		return escapeHTML(code)
	}

	var buf bytes.Buffer
	if err := formatter.Format(&buf, styles.Get("monokai"), iter); err != nil {
		return escapeHTML(code)
	}

	raw := buf.String()
	// Strip the outer <pre class="chroma"><code>...</code></pre> wrapper that
	// chroma's HTML formatter adds by default — callers provide their own.
	raw = stripWrapper(raw)
	return raw
}

// stripWrapper removes the outer <pre class="chroma"><code>...</code></pre>
// that chroma's HTML formatter produces, returning only the inner token spans.
func stripWrapper(s string) string {
	s = strings.TrimSpace(s)
	// Strip opening <pre...><code...>
	if i := strings.Index(s, ">"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.Index(s, ">"); i >= 0 {
		s = s[i+1:]
	}
	// Strip closing </code></pre>
	if i := strings.LastIndex(s, "</code>"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "</pre>"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// CSSForTheme returns the CSS stylesheet for the given chroma theme name.
// The returned CSS targets the class-based output from the HTML formatter.
func CSSForTheme(theme string) string {
	if theme == "" {
		theme = "github-dark"
	}

	style := styles.Get(theme)
	if style == nil {
		style = styles.Get("monokai")
	}

	var buf bytes.Buffer
	if err := formatter.WriteCSS(&buf, style); err != nil {
		return ""
	}

	return buf.String()
}

// AvailableThemes returns a list of built-in chroma theme names.
func AvailableThemes() []string {
	var names []string
	for _, s := range styles.Names() {
		names = append(names, s)
	}
	return names
}

// NormalizeLanguage normalizes common language aliases to chroma lexer names.
func NormalizeLanguage(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	aliases := map[string]string{
		"js":     "javascript",
		"ts":     "typescript",
		"jsx":    "jsx",
		"tsx":    "tsx",
		"py":     "python",
		"rb":     "ruby",
		"sh":     "bash",
		"shell":  "bash",
		"zsh":    "bash",
		"yml":    "yaml",
		"md":     "markdown",
		"rs":     "rust",
		"go":     "go",
		"golang": "go",
		"cs":     "csharp",
		"c#":     "csharp",
		"cpp":    "cpp",
		"c++":    "cpp",
		"objc":   "objectivec",
		"tex":    "latex",
		"vim":    "viml",
		"dockerfile": "docker",
		"docker":     "docker",
	}
	if normalized, ok := aliases[lang]; ok {
		return normalized
	}
	return lang
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}
