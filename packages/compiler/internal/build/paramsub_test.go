package build

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/content"
)

func TestSubstituteParamBindings(t *testing.T) {
	src := `export default function P(props) {
  const slug = props.params?.slug;
  return <h1>{slug}</h1>;
}`
	prog := parseInlineSource(src)
	substituteParamBindings(prog, map[string]string{"slug": "hello-collections"})

	if !programReferencesString(prog, "hello-collections") {
		t.Error("expected params.slug to be substituted with the literal value")
	}
}

func TestSubstituteParamsDirectForm(t *testing.T) {
	src := `export default function P(props) {
  return <h1>{params.slug}</h1>;
}`
	prog := parseInlineSource(src)
	substituteParamBindings(prog, map[string]string{"slug": "abc"})
	if !programReferencesString(prog, "abc") {
		t.Error("expected params.slug to be substituted")
	}
}

func TestFoldCollectionFindWithBinding(t *testing.T) {
	entries := map[string][]content.Entry{
		"blog": {
			{Slug: "alpha", Data: map[string]any{"title": "Alpha"}},
			{Slug: "beta", Data: map[string]any{"title": "Beta"}},
		},
	}
	// Mirrors [slug].tsx: params.slug is substituted to a literal, then the
	// find() predicate referencing the local `slug` must fold.
	src := `export default function Page(props) {
  const slug = props.params?.slug;
  const post = getCollection('blog').find((x) => x.slug === slug);
  return <h1>{post.data.title}</h1>;
}`
	prog := parseInlineSource(src)
	substituteParamBindings(prog, map[string]string{"slug": "beta"})
	b := &Builder{contentCollections: entries}
	b.InlineContent(prog)

	if !programReferencesString(prog, "Beta") {
		t.Error("expected find() to fold to the matching entry (Beta)")
	}
}

func TestFoldUnknownPredicateDoesNotFold(t *testing.T) {
	entries := map[string][]content.Entry{
		"blog": {{Slug: "a", Data: map[string]any{"title": "A"}}},
	}
	// `mystery` is an unbound identifier: the predicate can't be evaluated, so
	// the chain must be left intact (not folded to empty).
	src := `getCollection('blog').filter((p) => p.data.title === mystery)`
	prog := parseInlineSource(src)
	if folded := foldCollectionChain(firstExpr(prog), entries, nil); folded != nil {
		t.Errorf("expected no fold for unknown identifier, got %T", folded)
	}
}

// programReferencesString reports whether any string literal in the program
// equals want.
func programReferencesString(prog *ast.Program, want string) bool {
	found := false
	var walkExpr func(e ast.Expr)
	var walkStmts func(stmts []ast.Stmt)
	walkExpr = func(e ast.Expr) {
		if e == nil || found {
			return
		}
		switch x := e.(type) {
		case *ast.Literal:
			if x.Value == want {
				found = true
			}
		case *ast.MemberExpr:
			walkExpr(x.Object)
		case *ast.CallExpr:
			walkExpr(x.Callee)
			for _, a := range x.Args {
				walkExpr(a)
			}
		case *ast.BinaryExpr:
			walkExpr(x.Left)
			walkExpr(x.Right)
		case *ast.UnaryExpr:
			walkExpr(x.Arg)
		case *ast.ConditionalExpr:
			walkExpr(x.Test)
			walkExpr(x.Consequent)
			walkExpr(x.Alternate)
		case *ast.ArrayExpr:
			for _, el := range x.Elements {
				walkExpr(el)
			}
		case *ast.ObjectExpr:
			for _, p := range x.Properties {
				walkExpr(p.Value)
			}
		case *ast.ArrowFn:
			walkStmts(x.Body)
		case *ast.TemplateExpr:
			for _, p := range x.Parts {
				walkExpr(p)
			}
		case *ast.JSXElement:
			for _, a := range x.Opening.Attributes {
				if a.Value != nil {
					walkExpr(a.Value)
				}
			}
			for _, c := range x.Children {
				if ec, ok := c.(*ast.JSXExprContainer); ok {
					walkExpr(ec.Expression)
				}
				if el, ok := c.(*ast.JSXElementChild); ok {
					walkExpr(el.Element)
				}
			}
		case *ast.JSXFragment:
			for _, c := range x.Children {
				if ec, ok := c.(*ast.JSXExprContainer); ok {
					walkExpr(ec.Expression)
				}
			}
		}
	}
	walkStmts = func(stmts []ast.Stmt) {
		for _, s := range stmts {
			switch st := s.(type) {
			case *ast.VarStmt:
				for _, d := range st.Decls {
					walkExpr(d.Init)
				}
			case *ast.ReturnStmt:
				walkExpr(st.Value)
			case *ast.ExprStmt:
				walkExpr(st.Expression)
			case *ast.FnDecl:
				walkStmts(st.Body)
			case *ast.ExportStmt:
				if st.Declaration != nil {
					walkStmts([]ast.Stmt{st.Declaration})
				}
			}
		}
	}
	walkStmts(prog.Body)
	return found
}
