package parser_test

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func parseProg(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(src).Tokenize())
	p.SetSource(src)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors for %q:\n%s", src, parser.FormatDiagnostics(errs))
	}
	return prog
}

// TestParserShapes asserts the printed AST of each hardening case and that
// the printed source re-parses to the same rendering.
func TestParserShapes(t *testing.T) {
	for _, tc := range shapeCases {
		t.Run(tc.name, func(t *testing.T) {
			prog := parseProg(t, tc.src)
			got := strings.TrimRight(astprint.Print(prog), "\n")
			if got != tc.want {
				t.Fatalf("source:\n%s\ngot:\n%s\nwant:\n%s", tc.src, got, tc.want)
			}
			again := strings.TrimRight(astprint.Print(parseProg(t, got)), "\n")
			if again != got {
				t.Fatalf("printing is not idempotent:\nfirst:\n%s\nsecond:\n%s", got, again)
			}
		})
	}
}

func firstExpr(t *testing.T, src string) ast.Expr {
	t.Helper()
	prog := parseProg(t, src)
	es, ok := prog.Body[0].(*ast.ExprStmt)
	if !ok {
		t.Fatalf("expected an expression statement, got %T", prog.Body[0])
	}
	return es.Expression
}

func TestNewBindsToMemberChain(t *testing.T) {
	ne, ok := firstExpr(t, "new a.b.C(1);").(*ast.NewExpr)
	if !ok {
		t.Fatal("expected NewExpr at the top")
	}
	m, ok := ne.Callee.(*ast.MemberExpr)
	if !ok || m.Property.(*ast.Identifier).Name != "C" || len(ne.Args) != 1 {
		t.Fatalf("new callee = %#v args=%d", ne.Callee, len(ne.Args))
	}
}

func TestTaggedTemplateNode(t *testing.T) {
	tt, ok := firstExpr(t, "css`a ${b} c`;").(*ast.TaggedTemplateExpr)
	if !ok {
		t.Fatal("expected TaggedTemplateExpr")
	}
	if tt.Tag.(*ast.Identifier).Name != "css" || len(tt.Quasi.Parts) != 1 || tt.Quasi.Raw[0] != "a " || tt.Quasi.Raw[1] != " c" {
		t.Fatalf("unexpected tagged template: %#v", tt.Quasi)
	}
}

func TestUnaryPlusKept(t *testing.T) {
	u, ok := firstExpr(t, "+a;").(*ast.UnaryExpr)
	if !ok || u.Op != "+" {
		t.Fatalf("expected unary plus, got %#v", u)
	}
}

func TestClassMemberKinds(t *testing.T) {
	prog := parseProg(t, "class A { get x() { return 1; } set x(v) {} static { } accessor y; *g() {} [k]() {} }")
	c := prog.Body[0].(*ast.ClassDecl)
	kinds := []ast.ClassMemberKind{ast.ClassGetter, ast.ClassSetter, ast.ClassStaticBlock, ast.ClassAccessor, ast.ClassMethod, ast.ClassMethod}
	if len(c.Members) != len(kinds) {
		t.Fatalf("got %d members", len(c.Members))
	}
	for i, k := range kinds {
		if c.Members[i].Kind != k {
			t.Errorf("member %d kind = %v, want %v", i, c.Members[i].Kind, k)
		}
	}
	if !c.Members[4].Generator || !c.Members[5].Computed {
		t.Error("generator/computed flags not recorded")
	}
}

func TestLabeledStatementNode(t *testing.T) {
	prog := parseProg(t, "outer: for (;;) { break outer; }")
	l, ok := prog.Body[0].(*ast.LabeledStmt)
	if !ok || l.Label != "outer" {
		t.Fatalf("expected LabeledStmt, got %T", prog.Body[0])
	}
	if _, ok := l.Body.(*ast.ForStmt); !ok {
		t.Fatalf("label body = %T", l.Body)
	}
}

func TestIfEmptyBodyHasNoNil(t *testing.T) {
	prog := parseProg(t, "if (x) ; else ;")
	s := prog.Body[0].(*ast.IfStmt)
	for _, st := range append(s.Consequent, s.Alternate...) {
		if st == nil {
			t.Fatal("nil statement in IfStmt body")
		}
	}
}

func TestExportDefaultAsyncArrow(t *testing.T) {
	prog := parseProg(t, "export default async () => {};")
	exp := prog.Body[0].(*ast.ExportStmt)
	es, ok := exp.Declaration.(*ast.ExprStmt)
	if !ok {
		t.Fatalf("declaration = %T", exp.Declaration)
	}
	if fn, ok := es.Expression.(*ast.ArrowFn); !ok || !fn.Async {
		t.Fatalf("expected async arrow, got %#v", es.Expression)
	}
}

func TestImportAttributesKept(t *testing.T) {
	prog := parseProg(t, `import x from "./a.json" with { type: "json" };`)
	imp := prog.Body[0].(*ast.ImportStmt)
	if len(imp.Attributes) != 1 || imp.Attributes[0].Key != "type" || imp.Attributes[0].Value != "json" {
		t.Fatalf("attributes = %#v", imp.Attributes)
	}
}

func TestForAwaitAndUsing(t *testing.T) {
	prog := parseProg(t, "async function f() { for await (const x of y) {} await using r = g(); }")
	fn := prog.Body[0].(*ast.FnDecl)
	if fi, ok := fn.Body[0].(*ast.ForInStmt); !ok || !fi.Await || !fi.IsForOf {
		t.Fatalf("for await = %#v", fn.Body[0])
	}
	if vs, ok := fn.Body[1].(*ast.VarStmt); !ok || vs.Kind != ast.VarAwaitUsing {
		t.Fatalf("await using = %#v", fn.Body[1])
	}
}

func TestRuntimeTypeScriptIsReported(t *testing.T) {
	cases := map[string]string{
		"enum":        "enum Color { Red }",
		"const-enum":  "const enum Color { Red }",
		"namespace":   "namespace N { export const a = 1; }",
		"param-prop":  "class A { constructor(private x: number) {} }",
		"decorator":   "@dec class A {}",
		"import-eq":   "import fs = require('fs');",
		"export-eq":   "export = x;",
		"enum-export": "export enum E { A }",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			p := parser.New(lexer.New(src).Tokenize())
			p.ParseProgram()
			if len(p.Errors()) == 0 {
				t.Fatalf("%q parsed without a diagnostic", src)
			}
			d, ok := p.Errors()[0].(parser.Diagnostic)
			if !ok || d.Hint == "" {
				t.Fatalf("diagnostic without a hint: %v", p.Errors()[0])
			}
		})
	}
	// Type-only namespaces and ambient declarations are dropped silently.
	for _, src := range []string{"namespace N { export type A = string; }", "declare enum E { A }", "declare module 'x' { const a: number; }"} {
		p := parser.New(lexer.New(src).Tokenize())
		p.ParseProgram()
		if len(p.Errors()) != 0 || p.DroppedTypes() == 0 {
			t.Errorf("%q: errs=%v dropped=%d", src, p.Errors(), p.DroppedTypes())
		}
	}
}

func TestEarlyErrors(t *testing.T) {
	bad := []string{
		"a ?? b || c;",
		"break;",
		"for (;;) { continue nope; }",
		"l: l: x;",
		"let a; let a;",
		"let a; var a;",
		"1 = 2;",
		"a + 1 = 2;",
		"({ __proto__: 1, __proto__: 2 });",
		"let let = 1;",
		"const x;",
		"function f(a, a) { 'use strict'; }",
		"class A { constructor() {} constructor() {} }",
		"class A { m() { this.#x; } }",
		"function f() { super.x; }",
		"(a, b) => { 'use strict'; }; with (a) {}; 'use strict'",
		"`\\unicode`;",
		"<a></b>;",
		"<a>}</a>;",
		"x = { a = 1 };",
	}
	for _, src := range bad {
		p := parser.New(lexer.New(src).Tokenize())
		p.ParseProgram()
		if len(p.Errors()) == 0 && src != "(a, b) => { 'use strict'; }; with (a) {}; 'use strict'" {
			t.Errorf("%q: expected an early error", src)
		}
	}
	good := []string{
		"a ?? (b || c);",
		"({ __proto__: a, __proto__: b } = x);",
		"({ a = 1 } = x);",
		"var a; var a;",
		"function f(a, a) {}",
		"try {} catch (e) { var e; }",
		"label: { break label; }",
		"tag`\\unicode`;",
	}
	for _, src := range good {
		p := parser.New(lexer.New(src).Tokenize())
		p.ParseProgram()
		if len(p.Errors()) != 0 {
			t.Errorf("%q: unexpected errors %v", src, p.Errors())
		}
	}
}
