package irtree

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func parseTestExpr(t *testing.T, src string) ast.Expr {
	t.Helper()
	prog := parser.New(lexer.New("const __x = " + src + ";").Tokenize()).ParseProgram()
	if len(prog.Body) == 0 {
		t.Fatalf("failed to parse %q", src)
	}
	vs, ok := prog.Body[0].(*ast.VarStmt)
	if !ok || len(vs.Decls) == 0 || vs.Decls[0].Init == nil {
		t.Fatalf("unexpected parse for %q", src)
	}
	return vs.Decls[0].Init
}

func TestFoldStructuralReact(t *testing.T) {
	fold := func(src string) string {
		expr, ok := foldStructuralReact(parseTestExpr(t, src))
		if !ok {
			t.Fatalf("not folded: %s", src)
		}
		return astprint.PrintExpr(expr)
	}

	if got := fold(`isValidElement(<div/>)`); got != "true" {
		t.Errorf("isValidElement(element) = %q, want true", got)
	}
	if got := fold(`isValidElement(null)`); got != "false" {
		t.Errorf("isValidElement(null) = %q, want false", got)
	}
	if got := fold(`Children.count([<a/>, <b/>])`); got != "2" {
		t.Errorf("Children.count = %q, want 2", got)
	}
	if got := fold(`Children.count(null)`); got != "0" {
		t.Errorf("Children.count(null) = %q, want 0", got)
	}

	cloned := fold(`cloneElement(<div class="a" />, { id: "x" })`)
	if !strings.Contains(cloned, `class="a"`) || !strings.Contains(cloned, `id="x"`) {
		t.Errorf("cloneElement merge = %q, want class and id", cloned)
	}

	only := fold(`Children.only(<span/>)`)
	if !strings.HasPrefix(only, "<span") {
		t.Errorf("Children.only = %q, want the span element", only)
	}

	mapped := fold(`Children.map([<a/>, <b/>], (c) => <li>{c}</li>)`)
	if strings.Count(mapped, "<li") != 2 {
		t.Errorf("Children.map = %q, want two li elements", mapped)
	}

	mappedIdx := fold(`Children.map(["x", "y"], (c, i) => <li id={i}>{c}</li>)`)
	if !strings.Contains(mappedIdx, "id={0}") || !strings.Contains(mappedIdx, "id={1}") {
		t.Errorf("Children.map index = %q, want ids 0 and 1", mappedIdx)
	}

	block := fold(`Children.map([<a/>], (c) => { return <li>{c}</li>; })`)
	if !strings.Contains(block, "<li") {
		t.Errorf("Children.map block body = %q, want a li", block)
	}

	expr := parseTestExpr(t, `Children.map([<a/>], (c) => <li>{c}</li>)`)
	first, ok1 := foldStructuralReact(expr)
	second, ok2 := foldStructuralReact(expr)
	if !ok1 || !ok2 || astprint.PrintExpr(first) != astprint.PrintExpr(second) {
		t.Error("folding Children.map mutated the shared callback AST")
	}

	if _, ok := foldStructuralReact(parseTestExpr(t, `Children.map(props.children, (c) => <li>{c}</li>)`)); ok {
		t.Error("dynamic Children.map should not fold")
	}
}
