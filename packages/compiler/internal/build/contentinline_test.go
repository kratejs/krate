package build

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/content"
)

func firstExpr(prog *ast.Program) ast.Expr {
	for _, stmt := range prog.Body {
		if es, ok := stmt.(*ast.ExprStmt); ok {
			return es.Expression
		}
		if vs, ok := stmt.(*ast.VarStmt); ok {
			for _, d := range vs.Decls {
				if d.Init != nil {
					return d.Init
				}
			}
		}
	}
	return nil
}

func TestFoldCollectionChainSort(t *testing.T) {
	entries := map[string][]content.Entry{
		"blog": {
			{Slug: "a", Data: map[string]any{"title": "A", "order": int64(2)}},
			{Slug: "b", Data: map[string]any{"title": "B", "order": int64(1)}},
		},
	}
	prog := parseInlineSource(`getCollection('blog').sort((a, b) => a.data.order - b.data.order)`)
	folded := foldCollectionChain(firstExpr(prog), entries, nil)
	arr, ok := folded.(*ast.ArrayExpr)
	if !ok {
		t.Fatalf("expected *ast.ArrayExpr, got %T", folded)
	}
	if len(arr.Elements) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(arr.Elements))
	}
	// Sorted ascending by order: b (1) then a (2).
	first, _ := arr.Elements[0].(*ast.ObjectExpr)
	if first == nil {
		t.Fatalf("expected object literal, got %#v", arr.Elements[0])
	}
	if slug := objectPropString(first, "slug"); slug != "b" {
		t.Errorf("expected first entry slug 'b', got %q", slug)
	}
}

func objectPropString(obj *ast.ObjectExpr, key string) string {
	for _, p := range obj.Properties {
		if p.Key == key {
			if lit, ok := p.Value.(*ast.Literal); ok {
				return lit.Value
			}
		}
	}
	return ""
}

func TestFoldCollectionChainFilter(t *testing.T) {
	entries := map[string][]content.Entry{
		"blog": {
			{Slug: "a", Data: map[string]any{"draft": false}},
			{Slug: "b", Data: map[string]any{"draft": true}},
			{Slug: "c", Data: map[string]any{"draft": false}},
		},
	}
	prog := parseInlineSource(`getCollection('blog').filter((p) => !p.data.draft)`)
	folded := foldCollectionChain(firstExpr(prog), entries, nil)
	arr, ok := folded.(*ast.ArrayExpr)
	if !ok {
		t.Fatalf("expected *ast.ArrayExpr, got %T", folded)
	}
	if len(arr.Elements) != 2 {
		t.Errorf("expected 2 non-draft entries, got %d", len(arr.Elements))
	}
}

func TestFoldCollectionChainNotRooted(t *testing.T) {
	prog := parseInlineSource(`someArray.filter((x) => x)`)
	if folded := foldCollectionChain(firstExpr(prog), nil, nil); folded != nil {
		t.Errorf("expected nil for non-collection chain, got %T", folded)
	}
}

func TestInlineContentReplacesGetCollection(t *testing.T) {
	entries := map[string][]content.Entry{
		"blog": {{Slug: "a", HTML: "<p>A</p>", Data: map[string]any{"title": "A"}}},
	}
	b := &Builder{contentCollections: entries}
	src := `export default function Page() {
  const posts = getCollection('blog').filter((p) => !p.data.draft);
  return <ul>{posts.map((p) => <li>{p.data.title}</li>)}</ul>;
}`
	prog := parseInlineSource(src)
	b.InlineContent(prog)

	found := false
	var walk func(e ast.Expr)
	walk = func(e ast.Expr) {
		if e == nil {
			return
		}
		if call, ok := e.(*ast.CallExpr); ok {
			if getCollectionName(call) != "" {
				found = true
			}
			walk(call.Callee)
			for _, a := range call.Args {
				walk(a)
			}
		}
		if mem, ok := e.(*ast.MemberExpr); ok {
			walk(mem.Object)
		}
		if arr, ok := e.(*ast.ArrayExpr); ok {
			for _, el := range arr.Elements {
				walk(el)
			}
		}
	}
	for _, stmt := range prog.Body {
		if fn, ok := stmt.(*ast.ExportStmt); ok {
			if fd, ok := fn.Declaration.(*ast.FnDecl); ok {
				for _, s := range fd.Body {
					if vs, ok := s.(*ast.VarStmt); ok {
						for _, d := range vs.Decls {
							walk(d.Init)
						}
					}
				}
			}
		}
	}
	if found {
		t.Error("expected getCollection to be inlined away")
	}
}
