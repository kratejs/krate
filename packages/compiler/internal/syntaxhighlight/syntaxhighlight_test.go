package syntaxhighlight

import (
	htmlstd "html"
	"regexp"
	"strings"
	"testing"
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
		t.Errorf("round-trip mismatch — highlighting lost content or whitespace:\n got: %q\nwant: %q", plain, want)
	}
}
