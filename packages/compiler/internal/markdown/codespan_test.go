package markdown

import (
	"strings"
	"testing"
)

// A single-backtick code span that contains a longer backtick run is still one
// span: its `{...}` is literal text, not an MDX expression.
func TestMDXCodeSpanHoldingBacktickRun(t *testing.T) {
	src := "Fenced code takes `title=\"a\"` and lines: ` ```ts {1,3-5} ` marks them.\n\nLater `{1+2}` stays literal.\n"
	r := compileMDX(t, src)
	// Literal code is plain HTML: a JSX segment would carry `{1,3-5}` as an
	// expression (the comma expression that evaluates to -2).
	for _, seg := range r.Segments {
		if seg.JSX != "" {
			t.Errorf("code span produced a JSX segment: %s", seg.JSX)
		}
	}
	mustContain(t, "code span", segsText(r), "<code>```ts {1,3-5}</code>", "<code>{1+2}</code>")
}

func TestMDXCodeSpanDoesNotCrossParagraphs(t *testing.T) {
	// The lone backtick is literal text, so the expression after it evaluates.
	src := "A lone ` backtick.\n\nSum {1+2} and `code`.\n"
	all := segsText(compileMDX(t, src))
	mustContain(t, "paragraph bound", all, "{1+2}", "<code>code</code>")
}

// An escaped pipe in a table cell is a pipe, also inside a code span, in both
// markdown and MDX documents.
func TestTableEscapedPipeInCodeSpan(t *testing.T) {
	src := "| Context | Behavior |\n|---|---|\n| Go API route | `krate.RevalidatePath(path, \"page\"\\|\"layout\")` / `krate.RevalidateTag(tag)` |\n| Plain | a \\| b |\n"
	for _, mdx := range []bool{false, true} {
		r := Compile(src, DefaultConfig(), Options{MDX: mdx})
		html := r.StaticHTML(DefaultConfig())
		if !strings.Contains(html, "<td><code>krate.RevalidatePath(path, &quot;page&quot;|&quot;layout&quot;)</code> / <code>krate.RevalidateTag(tag)</code></td>") ||
			!strings.Contains(html, "<td>a | b</td>") {
			t.Errorf("mdx=%v: escaped pipes rendered wrongly:\n%s", mdx, html)
		}
		if strings.Contains(html, `\|`) {
			t.Errorf("mdx=%v: backslash leaked:\n%s", mdx, html)
		}
	}
}
