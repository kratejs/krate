package irtree

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
)

func TestChildComponentHyphenatedPropKey(t *testing.T) {
	b := &builder{componentCtx: componentCtx{localSignals: map[string]ast.Expr{}}}
	got := b.buildChildPropsRegDecl("probe", map[string]ast.Expr{
		"data-testid": &ast.Literal{Kind: ast.StringLit, Value: "probe"},
	})
	if strings.Contains(got, "{data-testid:") {
		t.Fatalf("hyphenated child prop key was not quoted: %s", got)
	}
	if !strings.Contains(got, "'data-testid':'probe'") {
		t.Fatalf("expected a quoted data-testid prop registration: %s", got)
	}
}
