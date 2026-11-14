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
