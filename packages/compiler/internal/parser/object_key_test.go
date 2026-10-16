package parser

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// TestObjectQuotedStringKeysUnquoted guards against double-quoted object keys
// (as emitted in the docs plugin's JSON literals, e.g. {"title":"Docs"}) being
// kept with their surrounding quotes. Keys must be stored unquoted so member
// lookups (item.title) match.
func TestObjectQuotedStringKeysUnquoted(t *testing.T) {
	src := `export default function F() {
  const data = [{ "title": "Docs", "url": "/docs/" }, { "id": "a-b", "depth": 2 }];
  return <div>{data.map((item) => <a href={item.url}>{item.title}</a>)}</div>;
}`
	prog, errs := parse(t, src)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	var found []string
	var walkExpr func(e ast.Expr)
	walkExpr = func(e ast.Expr) {
		switch x := e.(type) {
		case *ast.ObjectExpr:
			for _, p := range x.Properties {
				found = append(found, p.Key)
			}
		case *ast.ArrayExpr:
			for _, el := range x.Elements {
				walkExpr(el)
			}
		}
	}
	var walkStmts func(stmts []ast.Stmt)
	walkStmts = func(stmts []ast.Stmt) {
		for _, s := range stmts {
			switch st := s.(type) {
			case *ast.VarStmt:
				for _, d := range st.Decls {
					walkExpr(d.Init)
				}
			case *ast.ExportStmt:
				if fn, ok := st.Declaration.(*ast.FnDecl); ok {
					walkStmts(fn.Body)
				}
			}
		}
	}
	walkStmts(prog.Body)
	want := map[string]bool{"title": true, "url": true, "id": true, "depth": true}
	if len(found) != len(want) {
		t.Fatalf("expected %d keys, got %d (%v)", len(want), len(found), found)
	}
	for _, k := range found {
		if !want[k] {
			t.Errorf("unexpected key %q (still quoted?) keys=%v", k, found)
		}
	}
}
