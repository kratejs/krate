package syntaxhighlight

import (
	"bytes"
	"strconv"
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

// DefaultTheme is the chroma theme name used when none is configured. It is
// shared with CSSForTheme so the class output and the generated CSS agree.
const DefaultTheme = "github-dark"

// Highlight returns HTML with CSS class annotations for the given code and
// language, using the default theme. Callers that know the configured code
// theme should use HighlightTheme so the emitted classes match the CSS.
func Highlight(code, lang string) string {
	return HighlightTheme(code, lang, DefaultTheme)
}

// HighlightTheme is Highlight with an explicit chroma theme. The theme affects
// the token->class mapping, so it must match the stylesheet produced by
// CSSForTheme or the highlighted code is styled (or unstyled) incorrectly.
func HighlightTheme(code, lang, theme string) string {
	return HighlightThemeLines(code, lang, theme, "")
}

// ParseLineRanges parses a highlight spec such as "1,3-5" into 1-based
// inclusive ranges. Malformed parts are skipped.
func ParseLineRanges(spec string) [][2]int {
	var out [][2]int
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(part, "-")
		a, err := strconv.Atoi(strings.TrimSpace(lo))
		if err != nil || a < 1 {
			continue
		}
		b := a
		if isRange {
			if b, err = strconv.Atoi(strings.TrimSpace(hi)); err != nil || b < a {
				continue
			}
		}
		out = append(out, [2]int{a, b})
	}
	return out
}

// HighlightThemeLines is HighlightTheme with the given lines (a spec such as
// "1,3-5") wrapped in <span class="hl"> so a stylesheet can mark them.
func HighlightThemeLines(code, lang, theme, lines string) string {
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

	style := styles.Get(theme)
	if style == nil {
		style = styles.Get(DefaultTheme)
	}

	f := formatter
	if ranges := ParseLineRanges(lines); len(ranges) > 0 {
		f = html.New(html.WithClasses(true), html.WithLineNumbers(false), html.HighlightLines(ranges))
	}
	var buf bytes.Buffer
	if err := f.Format(&buf, style, iter); err != nil {
		return escapeHTML(code)
	}

	raw := buf.String()
	// Strip the outer <pre class="chroma"><code>...</code></pre> wrapper that
	// chroma's HTML formatter adds by default - callers provide their own.
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
		theme = DefaultTheme
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
	return append([]string(nil), styles.Names()...)
}

// NormalizeLanguage normalizes common language aliases to chroma lexer names.
func NormalizeLanguage(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	aliases := map[string]string{
		"js":           "javascript",
		"ts":           "typescript",
		"jsx":          "jsx",
		"tsx":          "tsx",
		"py":           "python",
		"rb":           "ruby",
		"sh":           "bash",
		"shell":        "bash",
		"zsh":          "bash",
		"yml":          "yaml",
		"md":           "markdown",
		"rs":           "rust",
		"go":           "go",
		"golang":       "go",
		"cs":           "csharp",
		"c#":           "csharp",
		"cpp":          "cpp",
		"c++":          "cpp",
		"objc":         "objectivec",
		"tex":          "latex",
		"vim":          "viml",
		"dockerfile":   "docker",
		"docker":       "docker",
		"mdx":          "markdown",
		"astro":        "html",
		"prisma":       "graphql",
		"dotenv":       "env",
		"shellsession": "console",
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
