package parser

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

func parse(t *testing.T, src string) (*ast.Program, []error) {
	t.Helper()
	l := lexer.New(src)
	tokens := l.Tokenize()
	p := New(tokens)
	p.Filename = "test.tsx"
	prog := p.ParseProgram()
	return prog, p.Errors()
}

func firstVarStmt(t *testing.T, prog *ast.Program) *ast.VarStmt {
	t.Helper()
	if len(prog.Body) == 0 {
		t.Fatal("body is empty")
	}
	vs, ok := prog.Body[0].(*ast.VarStmt)
	if !ok {
		t.Fatalf("expected VarStmt, got %T", prog.Body[0])
	}
	return vs
}

func firstVarDecl(t *testing.T, prog *ast.Program) *ast.VarDecl {
	t.Helper()
	vs := firstVarStmt(t, prog)
	if len(vs.Decls) == 0 {
		t.Fatal("no decls in VarStmt")
	}
	return vs.Decls[0]
}

func TestJSXElement(t *testing.T) {
	prog, errs := parse(t, "const el = <div>hello</div>;")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	decl := firstVarDecl(t, prog)
	jsx, ok := decl.Init.(*ast.JSXElement)
	if !ok {
		t.Fatalf("expected JSXElement, got %T", decl.Init)
	}
	if jsx.Opening.Name != "div" {
		t.Errorf("expected element name 'div', got %q", jsx.Opening.Name)
	}
}

// TestMalformedInputDiagnostics locks in that malformed input surfaces a
// diagnostic rather than being silently accepted or dropped.
func TestMalformedInputDiagnostics(t *testing.T) {
	cases := []struct{ name, src string }{
		{"unterminated-jsx", "const el = <div>hello"},
		{"unterminated-jsx-fragment", "const el = <>hello"},
		{"unterminated-jsx-tag", "const el = <div class=\"x\" "},
		{"missing-rparen", "const x = (1 + 2;"},
		{"missing-rbrace", "function f() { return 1;"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			p := New(lexer.New(tt.src).Tokenize())
			p.Filename = "test.tsx"
			p.ParseProgram()
			if len(p.Errors()) == 0 {
				t.Errorf("expected a diagnostic for %q", tt.src)
			}
		})
	}
}
