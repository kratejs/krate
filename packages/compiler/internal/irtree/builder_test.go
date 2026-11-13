package irtree_test

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

func TestBuildFragmentEscapesTextExpression(t *testing.T) {
	tree := annotateAndBuild(t, "export default function App() {\n\treturn <>{`a < b`}</>;\n}")
	if len(tree.Root.Children) == 0 {
		t.Fatal("expected children")
	}
	sh, ok := tree.Root.Children[0].(*irtree.StaticHTML)
	if !ok {
		t.Fatalf("expected StaticHTML, got %T", tree.Root.Children[0])
	}
	if !strings.Contains(sh.HTML, "a &lt; b") {
		t.Errorf("expected escaped text 'a &lt; b', got %q", sh.HTML)
	}
}

// Nested components

func TestBuildNestedComponentSlot(t *testing.T) {
	src := `function Header() { return <h1>hi</h1>; }
export default function App() { return <div><Header /></div>; }`
	tree := annotateAndBuild(t, src)
	var comp *irtree.ComponentSlot
	for _, child := range tree.Root.Children {
		if cs, ok := child.(*irtree.ComponentSlot); ok {
			comp = cs
			break
		}
	}
	if comp == nil {
		t.Fatal("expected ComponentSlot for nested <Header />")
	}
	if comp.Component == nil {
		t.Fatal("expected non-nil Component on slot")
	}
	if comp.Component.Name != "Header" {
		t.Errorf("expected Header component, got %q", comp.Component.Name)
	}
	if !comp.Component.IsSSREval {
		t.Error("expected signal-less nested component to be SSR-evaluated")
	}
}

// Text escaping in static value slots

func TestBuildEscapesTemplateLiteralInTextPosition(t *testing.T) {
	// Mirrors the <Code>{`function App() { return <h1>Hello</h1>; }`}</Code> case:
	// a template literal rendered as JSX text must be HTML-escaped.
	tree := annotateAndBuild(t, "export default function App() {\n\treturn <code>{`return <h1>Hello</h1>;`}</code>;\n}")
	var found bool
	for _, child := range tree.Root.Children {
		if sh, ok := child.(*irtree.StaticHTML); ok {
			if strings.Contains(sh.HTML, "&lt;h1&gt;") {
				found = true
			}
			if strings.Contains(sh.HTML, "<h1>Hello") {
				t.Error("template literal leaked raw <h1> into static HTML")
			}
		}
	}
	if !found {
		t.Error("expected escaped template literal (&lt;h1&gt;) in static HTML")
	}
}

func TestBuildEscapesStringBinaryConcat(t *testing.T) {
	tree := annotateAndBuild(t, "export default function App() {\n\treturn <p>{`a ` + `&` + ` b`}</p>;\n}")
	var found bool
	for _, child := range tree.Root.Children {
		if sh, ok := child.(*irtree.StaticHTML); ok {
			if strings.Contains(sh.HTML, "a &amp; b") {
				found = true
			}
			if strings.Contains(sh.HTML, "a & b") {
				t.Errorf("binary string concat leaked raw & into static HTML: %q", sh.HTML)
			}
		}
	}
	if !found {
		t.Error("expected escaped binary string concat in static HTML")
	}
}

// renderStaticHTML walks slot nodes collecting StaticHTML/initial text.
func renderStaticHTML(children []irtree.SlotNode) string {
	var b strings.Builder
	var walk func(nodes []irtree.SlotNode)
	walk = func(nodes []irtree.SlotNode) {
		for _, n := range nodes {
			switch c := n.(type) {
			case *irtree.StaticHTML:
				b.WriteString(c.HTML)
			case *irtree.TextSlot:
				b.WriteString(c.Initial)
			case *irtree.ExprSlot:
				b.WriteString(c.Initial)
			case *irtree.ComponentSlot:
				if c.Component != nil {
					walk(c.Component.Children)
					walk(c.Component.ReturnSlots)
				}
			case *irtree.ConditionalSlot:
				walk(c.Consequent)
				walk(c.Alternate)
			case *irtree.ListSlot:
				for _, li := range c.Items {
					walk(li.Contents)
				}
			}
		}
	}
	walk(children)
	return b.String()
}
