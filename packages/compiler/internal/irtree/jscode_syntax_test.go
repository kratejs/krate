package irtree

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// TestCodegenModernSyntax checks that syntax the parser now represents
// structurally is rendered back to valid JS rather than dropped.
func TestCodegenModernSyntax(t *testing.T) {
	cases := []struct{ src, want string }{
		{"const { a, b = 1, ...r } = o;", "const {a,b=1,...r}=o;"},
		{"const [x, , y = 2] = arr;", "const [x,,y=2]=arr;"},
		{"x = [, a, ,];", "[,a,,]"},
		{"x = void 0;", "void 0"},
		{"x = - -y;", "- -y"},
		{"x = { get v() { return 1; }, [k]: 2, *g() {} };", "get v() {"},
		{"x = { get v() { return 1; }, [k]: 2, *g() {} };", "[k]:2"},
		{"x = { get v() { return 1; }, [k]: 2, *g() {} };", "*g() {}"},
		{"x = function* named(a) { yield a; };", "(function* named(a) {"},
		{"class A extends B { #n = 1; static s = 2; get v() { return this.#n; } }", "class A extends B {"},
		{"class A extends B { #n = 1; static s = 2; get v() { return this.#n; } }", "static s = 2;"},
		{"outer: for (;;) { break outer; }", "outer: for ("},
		{"x = css`a ${b}`;", "css`a ${b}`"},
		{"for await (const [k, v] of it) {}", "for await (const [k,v] of it)"},
		{"try {} catch {}", "catch {"},
		{"try {} catch ({ message }) {}", "catch ({message})"},
	}
	for _, tc := range cases {
		p := parser.New(lexer.New(tc.src).Tokenize())
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Fatalf("%q: %v", tc.src, errs)
		}
		var out strings.Builder
		for _, s := range prog.Body {
			out.WriteString(renderStmtJS(s, nil))
		}
		if !strings.Contains(out.String(), tc.want) {
			t.Errorf("%q rendered as:\n%s\nwant it to contain %q", tc.src, out.String(), tc.want)
		}
	}
}

func TestCodegenIssuesAcceptsNewNodes(t *testing.T) {
	src := "function C() { const K = class { m() {} }; label: for (;;) { break label; } const t = tag`x`; return new.target; }"
	p := parser.New(lexer.New(src).Tokenize())
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatal(errs)
	}
	if issues := codegenIssues(prog.Body[0].(*ast.FnDecl), nil); len(issues) > 0 {
		t.Fatalf("unexpected codegen issues: %v", issues)
	}
}
