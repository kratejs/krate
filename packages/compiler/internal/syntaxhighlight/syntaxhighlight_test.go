package syntaxhighlight

import (
	htmlstd "html"
	"regexp"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2/lexers"
)

// TestIndentationPreserved verifies that indentation and content survive
// chroma's class-based HTML output. Chroma wraps every token in a span, so
// indented lines carry their leading spaces as literal text *between* tags
// (e.g. `<span class="cl">    <span class="nx">console</span>`). Rendered
// inside <pre> those spaces display correctly; stripping the tags and
// unescaping entities must reproduce the source verbatim.
func TestIndentationPreserved(t *testing.T) {
	code := "function hello() {\n    console.log(\"world\");\n        console.log(\"deep indent\");\n}"
	hl := Highlight(code, "javascript")

	if !strings.Contains(hl, ">    <span") {
		t.Error("4-space indent not present between tags")
	}
	if !strings.Contains(hl, ">        <span") {
		t.Error("8-space indent not present between tags")
	}

	tagRe := regexp.MustCompile(`<[^>]+>`)
	plain := htmlstd.UnescapeString(tagRe.ReplaceAllString(hl, ""))
	if want := strings.TrimSpace(code); plain != want {
		t.Errorf("round-trip mismatch - highlighting lost content or whitespace:\n got: %q\nwant: %q", plain, want)
	}
}

func TestParseLineRanges(t *testing.T) {
	got := ParseLineRanges("1, 3-5,x,7-2,0")
	want := [][2]int{{1, 1}, {3, 5}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("ParseLineRanges = %v, want %v", got, want)
	}
}

func TestHighlightThemeLinesMarksLines(t *testing.T) {
	out := HighlightThemeLines("a := 1\nb := 2\nc := 3\n", "go", "github-dark", "2")
	if strings.Count(out, `class="line hl"`) != 1 || !strings.Contains(out, "b") {
		t.Errorf("want exactly one highlighted line, got %q", out)
	}
	if plain := HighlightTheme("a := 1\nb := 2\n", "go", "github-dark"); strings.Contains(plain, " hl") {
		t.Errorf("no spec must not highlight: %q", plain)
	}
}

func TestNormalizeLanguageAliasesWithoutLexer(t *testing.T) {
	for in, want := range map[string]string{"mdx": "markdown", "astro": "html", "prisma": "graphql"} {
		if got := NormalizeLanguage(in); got != want {
			t.Errorf("NormalizeLanguage(%q) = %q, want %q", in, got, want)
		}
		if lexers.Get(NormalizeLanguage(in)) == nil {
			t.Errorf("no lexer for %q after alias", in)
		}
	}
}
