package irtree

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// TestBridgeScopeExpr verifies page-scope signal/prop reads inside an external
// island subtree are rewritten to island props (and locals are inlined).
func TestBridgeScopeExpr(t *testing.T) {
	b := &builder{
		localSignals: map[string]ast.Expr{
			"open": &ast.Literal{Kind: ast.BoolLit, Value: "true"},
		},
		localProps: valueMap{"title": jsvalue.String("Hi")},
	}
	used := map[string]bool{}

	got := astprint.PrintExpr(b.bridgeScopeExpr(&ast.CallExpr{Callee: &ast.Identifier{Name: "open"}}, used))
	if got != "props.open" {
		t.Fatalf("signal call = %q, want props.open", got)
	}
	if !used["open"] {
		t.Fatal("expected open to be recorded as a bridged value")
	}

	got = astprint.PrintExpr(b.bridgeScopeExpr(&ast.Identifier{Name: "title"}, used))
	if got != `"Hi"` {
		t.Fatalf("local prop = %q, want %q", got, `"Hi"`)
	}

	got = astprint.PrintExpr(b.bridgeScopeExpr(&ast.Identifier{Name: "Other"}, used))
	if got != "Other" {
		t.Fatalf("component identifier = %q, want Other (unchanged)", got)
	}
}
