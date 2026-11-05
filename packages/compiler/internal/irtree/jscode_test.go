package irtree

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
)

func TestRenderVarInitJSOneKeyword(t *testing.T) {
	stmt := &ast.VarStmt{Kind: ast.VarVar, Decls: []*ast.VarDecl{
		{Name: "i", Init: &ast.Literal{Kind: ast.NumberLit, Value: "0"}},
		{Name: "n"},
	}}
	if got, want := renderVarInitJS(stmt, nil), "var i=0, n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
