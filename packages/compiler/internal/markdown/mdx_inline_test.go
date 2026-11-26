package markdown

import (
	"strings"
	"testing"
)

func compileMDX(t *testing.T, src string) *Result {
	t.Helper()
	return Compile(src, DefaultConfig(), Options{MDX: true})
}

func segsText(r *Result) string {
	var b strings.Builder
	for _, s := range r.Segments {
		b.WriteString(s.HTML)
		b.WriteString(s.JSX)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestMDXInlineJSXAndMarkdownChildren(t *testing.T) {
	r := compileMDX(t, "Status: <Badge tone=\"x\">**beta**</Badge> done.\n\n<Card title=\"T\">**bold** - item</Card>\n")
	all := segsText(r)
	mustContain(t, "inline jsx", all, `<Badge tone="x">`, "<strong>bold</strong>", "</Badge>", `<Card title="T">`)
	mustNotContain(t, "inline jsx", all, "&lt;Badge", "&lt;Card", "**")
	// A block-level component is not wrapped in a paragraph.
	if strings.Contains(all, "<p><Card") {
		t.Errorf("flow component wrapped in <p>:\n%s", all)
	}
}

func TestMDXClosingTagAfterParagraph(t *testing.T) {
	r := compileMDX(t, "<Note>\nSome text\n</Note>\n")
	if len(r.Diags) > 0 {
		t.Fatalf("diagnostics: %+v", r.Diags)
	}
	all := segsText(r)
	mustContain(t, "closing after paragraph", all, "<Note>", "Some text", "</Note>")
	mustNotContain(t, "closing after paragraph", all, "<p>Some text</Note>", "Some text</Note></p>")
}

func TestMDXNestedFlowComponents(t *testing.T) {
	r := compileMDX(t, "<Outer>\n<Inner>\nText here\n</Inner>\nMore text\n</Outer>\n")
	all := segsText(r)
	mustContain(t, "nested", all, "<Outer>", "<Inner>", "Text here", "</Inner>", "More text", "</Outer>")
	mustNotContain(t, "nested", all, "Text here</Inner>", "More text</Outer>")
}

func TestMDXInlineOpenerSpanningLines(t *testing.T) {
	r := compileMDX(t, "see <Badge>x\ny</Badge> now\n")
	all := segsText(r)
	mustContain(t, "inline opener", all, `<p>see <Badge>{"x\ny"}</Badge> now</p>`)
}

func TestMDXClosingTagAfterList(t *testing.T) {
	r := compileMDX(t, "<Note>\n- one\n- two\n</Note>\n")
	all := segsText(r)
	mustContain(t, "list", all, "<li>one</li>", "<li>two</li>", "</Note>")
	mustNotContain(t, "list", all, "two</Note>")
}

func TestMDXOpeningTagAfterParagraph(t *testing.T) {
	r := compileMDX(t, "Intro text\n<Note>\nBody\n</Note>\n")
	all := segsText(r)
	mustContain(t, "opening after paragraph", all, "Intro text", "<Note>", "Body", "</Note>")
	mustNotContain(t, "opening after paragraph", all, "Body</Note>", "<p>{\"Intro text\\n\"}<Note>")
}
