package build

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func parseProgForBuild(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(src).Tokenize())
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return p.ParseProgram()
}

// TestFlattenComponentSpreadAttrs ensures a JSX spread of a same-function const
// object literal is rewritten into plain attributes, mirroring the docs plugin's
// <DocsLayout {...docsProps}>. Spreads that can't resolve to an object literal
// (non-object consts, unknown identifiers) are left untouched.
func TestFlattenComponentSpreadAttrs(t *testing.T) {
	src := `export default function DocPage() {
  const docsProps = {
    pageTitle: "Getting Started",
    sidebarItems: [{ "title": "Intro", "url": "/docs/" }],
  };
  const other = "not-an-object";
  return (
    <>
      <DocLayout {...docsProps} class="page" />
      <DocLayout {...other} />
      <DocLayout {...someExternal} />
    </>
  );
}`
	prog := parseProgForBuild(t, src)
	b := &Builder{}
	b.FlattenComponentSpreadAttrs(prog)

	els := collectDocLayouts(prog)
	if len(els) != 3 {
		t.Fatalf("expected 3 <DocLayout> elements, got %d", len(els))
	}

	first := els[0].Opening.Attributes
	names := map[string]bool{}
	for _, attr := range first {
		names[attr.Name] = true
	}
	for _, want := range []string{"pageTitle", "sidebarItems", "class"} {
		if !names[want] {
			t.Errorf("expected attribute %q after flattening, got %v", want, attrNames(first))
		}
	}
	spreadCount := 0
	for _, attr := range first {
		if attr.Spread {
			spreadCount++
		}
	}
	if spreadCount != 0 || len(first) != 3 {
		t.Errorf("expected const-object spread to be replaced by 3 plain attrs, got %v", attrNames(first))
	}

	// Identifier bound to a non-object value: spread must remain a spread.
	second := els[1].Opening.Attributes
	if !hasSpread(second) || len(second) != 1 {
		t.Errorf("expected unresolved spread to remain on non-object identifier, got %v", attrNames(second))
	}

	// Unknown identifier: spread must remain a spread.
	third := els[2].Opening.Attributes
	if !hasSpread(third) || len(third) != 1 {
		t.Errorf("expected unresolved spread to remain on unknown identifier, got %v", attrNames(third))
	}
}

func hasSpread(attrs []*ast.JSXAttr) bool {
	for _, a := range attrs {
		if a.Spread {
			return true
		}
	}
	return false
}

func attrNames(attrs []*ast.JSXAttr) []string {
	var names []string
	for _, a := range attrs {
		name := a.Name
		if a.Spread {
			name = "{...}"
		}
		names = append(names, name)
	}
	return names
}

// collectDocLayouts walks top-level fn bodies (including export defaults) and
// returns every <DocLayout> element in document order.
func collectDocLayouts(prog *ast.Program) []*ast.JSXElement {
	var out []*ast.JSXElement
	for _, stmt := range prog.Body {
		var fn *ast.FnDecl
		switch s := stmt.(type) {
		case *ast.FnDecl:
			fn = s
		case *ast.ExportStmt:
			if d, ok := s.Declaration.(*ast.FnDecl); ok {
				fn = d
			}
		}
		if fn == nil {
			continue
		}
		for _, s := range fn.Body {
			collectFromStmt(s, &out)
		}
	}
	return out
}

func collectFromStmt(s ast.Stmt, out *[]*ast.JSXElement) {
	switch t := s.(type) {
	case *ast.ReturnStmt:
		collectFromExpr(t.Value, out)
	case *ast.ExprStmt:
		collectFromExpr(t.Expression, out)
	case *ast.VarStmt:
		for _, d := range t.Decls {
			collectFromExpr(d.Init, out)
		}
	case *ast.IfStmt:
		for _, c := range t.Consequent {
			collectFromStmt(c, out)
		}
		for _, c := range t.Alternate {
			collectFromStmt(c, out)
		}
	case *ast.ExportStmt:
		if d, ok := t.Declaration.(*ast.FnDecl); ok {
			for _, c := range d.Body {
				collectFromStmt(c, out)
			}
		}
	}
}

func collectFromExpr(e ast.Expr, out *[]*ast.JSXElement) {
	if e == nil {
		return
	}
	switch t := e.(type) {
	case *ast.JSXElement:
		if t.Opening.Name == "DocLayout" {
			*out = append(*out, t)
		}
		collectFromChildren(t.Children, out)
	case *ast.JSXFragment:
		collectFromChildren(t.Children, out)
	case *ast.BinaryExpr:
		collectFromExpr(t.Left, out)
		collectFromExpr(t.Right, out)
	case *ast.ConditionalExpr:
		collectFromExpr(t.Consequent, out)
		collectFromExpr(t.Alternate, out)
	}
}

func collectFromChildren(children []ast.JSXChild, out *[]*ast.JSXElement) {
	for _, c := range children {
		switch ch := c.(type) {
		case *ast.JSXElementChild:
			collectFromExpr(ch.Element, out)
		case *ast.JSXFragmentChild:
			collectFromExpr(ch.Fragment, out)
		case *ast.JSXExprContainer:
			collectFromExpr(ch.Expression, out)
		}
	}
}
