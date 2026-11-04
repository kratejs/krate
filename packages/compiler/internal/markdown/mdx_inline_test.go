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

func TestMDXExportsAndExpressions(t *testing.T) {
	src := "---\ntitle: Hello\n---\n\nexport const v = 42\n\nValue {v} and sum {1+2} and {frontmatter.title}.\n\n# {frontmatter.title}\n"
	r := compileMDX(t, src)
	if len(r.ESM) != 1 || r.ESM[0] != "export const v = 42" {
		t.Fatalf("ESM = %#v", r.ESM)
	}
	all := segsText(r)
	mustContain(t, "expr", all, "Value 42 and sum ", "{1+2}", "and Hello.", `id="hello"`, ">Hello</h1>")
	mustNotContain(t, "expr", all, "export const", "{v}", "{frontmatter.title}", "frontmattertitle")
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

func TestMDXCommentsAndEscapes(t *testing.T) {
	r := compileMDX(t, "a {/* hidden */} b \\{literal\\} c\n")
	all := segsText(r)
	mustNotContain(t, "comment", all, "hidden", "<em>")
	mustContain(t, "escaped brace", all, "{literal}")
}

func TestMDXExpressionAttrsAndMultiline(t *testing.T) {
	src := "<Chart\n  data={{ a: 1, b: [1, 2] }}\n  label=\"x\"\n/>\n\n<Box style={{color: 'red'}}>text {1 + 1}</Box>\n"
	r := compileMDX(t, src)
	all := segsText(r)
	mustContain(t, "attrs", all, "data={{ a: 1, b: [1, 2] }}", "<Box style={{color: 'red'}}>", "{1 + 1}")
}

func TestMDXCodeFenceStaysLiteral(t *testing.T) {
	r := compileMDX(t, "```jsx\nconst x = <Card a={1}>{v}</Card>\nexport const z = 1\n```\n")
	if len(r.ESM) != 0 {
		t.Fatalf("fenced export hoisted: %v", r.ESM)
	}
	if len(r.Segments) != 1 || r.Segments[0].Code == nil || !strings.Contains(r.Segments[0].Code.Code, "<Card a={1}>{v}</Card>") {
		t.Fatalf("fence not preserved: %#v", r.Segments)
	}
	r = compileMDX(t, "Use `<Card a={1}>` and `{v}` inline.\n")
	mustContain(t, "inline code", segsText(r), "<code>&lt;Card a={1}&gt;</code>", "<code>{v}</code>")
}

func TestMDXUnterminatedExpressionDiagnostic(t *testing.T) {
	src := "---\ntitle: x\n---\n\nok\n\nbroken {oops here\n"
	r := compileMDX(t, src)
	if len(r.Diags) == 0 {
		t.Fatal("expected a diagnostic for the unterminated expression")
	}
	d := r.Diags[0]
	if d.Line != 7 || d.Warning {
		t.Errorf("diag = %+v, want error at line 7", d)
	}
}

func TestMDXMultiLineESM(t *testing.T) {
	r := compileMDX(t, "import {\n  A,\n  B,\n} from './x'\nexport const meta = {\n  a: 1,\n}\n\n# T\n")
	if len(r.ESM) != 2 {
		t.Fatalf("ESM = %#v", r.ESM)
	}
	if !strings.HasPrefix(r.ESM[0], "import {") || !strings.HasPrefix(r.ESM[1], "export const meta") {
		t.Fatalf("ESM = %#v", r.ESM)
	}
}

func TestPlainMarkdownBracesAreLiteral(t *testing.T) {
	r := Compile("Use {braces} and <Foo> here\n\n<Card>\nx\n</Card>\n", DefaultConfig(), Options{})
	all := segsText(r)
	mustContain(t, "md braces", all, "{braces}")
	mustContain(t, "md block component", all, "<Card>")
}

func TestStaticHTMLHighlightsCodeAndDropsComponents(t *testing.T) {
	r := Compile("```ts\nconst a: number = 1\n```\n\n<Card>**x**</Card>\n", DefaultConfig(), Options{MDX: true})
	h := r.StaticHTML(DefaultConfig())
	mustContain(t, "static", h, `class="chroma"`, `language-ts`, "<strong>x</strong>")
	mustNotContain(t, "static", h, "<Card")
}
