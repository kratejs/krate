package parser_test

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
)

func jsxRoot(t *testing.T, src string) *ast.JSXElement {
	t.Helper()
	el, ok := firstExpr(t, src).(*ast.JSXElement)
	if !ok {
		t.Fatalf("expected a JSX element for %q", src)
	}
	return el
}

func TestJSXNamespacedAndHyphenatedNames(t *testing.T) {
	el := jsxRoot(t, `<svg xmlns:xlink="http://www.w3.org/1999/xlink"><use xlink:href="#a" /><my-element data-x="1"></my-element><svg:rect /></svg>;`)
	if el.Opening.Attributes[0].Name != "xmlns:xlink" {
		t.Errorf("attr name = %q", el.Opening.Attributes[0].Name)
	}
	var names []string
	for _, c := range el.Children {
		if ec, ok := c.(*ast.JSXElementChild); ok {
			names = append(names, ec.Element.Opening.Name)
			if ec.Element.Opening.Name == "use" && ec.Element.Opening.Attributes[0].Name != "xlink:href" {
				t.Errorf("use attr = %q", ec.Element.Opening.Attributes[0].Name)
			}
		}
	}
	want := []string{"use", "my-element", "svg:rect"}
	if len(names) != len(want) {
		t.Fatalf("children = %v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("child %d = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestJSXSpreadChild(t *testing.T) {
	el := jsxRoot(t, `<div>{...items}</div>;`)
	ec, ok := el.Children[0].(*ast.JSXExprContainer)
	if !ok {
		t.Fatalf("child = %T", el.Children[0])
	}
	if u, ok := ec.Expression.(*ast.UnaryExpr); !ok || u.Op != "..." {
		t.Fatalf("spread child = %#v", ec.Expression)
	}
}

func TestJSXAttributeStringBackslash(t *testing.T) {
	el := jsxRoot(t, `<a title="C:\" b='x\y'>t</a>;`)
	if v := el.Opening.Attributes[0].Value.(*ast.Literal).Value; v != `C:\` {
		t.Errorf("title = %q", v)
	}
	if v := el.Opening.Attributes[1].Value.(*ast.Literal).Value; v != `x\y` {
		t.Errorf("b = %q", v)
	}
}

func TestJSXEntities(t *testing.T) {
	cases := []struct{ src, text string }{
		{`<p>&copy; &amp; &#123; &#x1F600; &nbsp;</p>;`, "\u00a9 & { \U0001F600 \u00a0"},
		// No semicolon, unknown or HTML5-only names stay literal.
		{`<p>&copy b &rarrb; &bogus; &#xZZ;</p>;`, "&copy b &rarrb; &bogus; &#xZZ;"},
		{`<p>&lang;&rang;</p>;`, "\u2329\u232a"},
	}
	for _, tc := range cases {
		el := jsxRoot(t, tc.src)
		got := el.Children[0].(*ast.JSXText).Value
		if got != tc.text {
			t.Errorf("%s: text = %q, want %q", tc.src, got, tc.text)
		}
	}
	el := jsxRoot(t, `<a title="&quot;x&quot; &amp b">t</a>;`)
	if v := el.Opening.Attributes[0].Value.(*ast.Literal).Value; v != `"x" &amp b` {
		t.Errorf("attr = %q", v)
	}
}

func TestJSXMismatchedClosingTag(t *testing.T) {
	for _, src := range []string{"<a></b>;", "<a.b></a.c>;", "<x:y></x:z>;"} {
		p := newParser(src)
		p.ParseProgram()
		if len(p.Errors()) == 0 {
			t.Errorf("%q: expected an error", src)
		}
	}
}
