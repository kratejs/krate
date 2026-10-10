package parser

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

// These tests assert AST *shape*, not just "no errors". The robustness suite
// only checks that parsing succeeds, so several silent mis-parses (optional
// chaining dropped, left-associative assignment, bare for-in) previously went
// unnoticed. Each case here pins the structure a correct parse must produce.

func parseNoErrs(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := New(lexer.New(src).Tokenize())
	p.Filename = "test.tsx"
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected errors for %q: %v", src, errs)
	}
	return prog
}

func firstExpr(t *testing.T, src string) ast.Expr {
	t.Helper()
	prog := parseNoErrs(t, src)
	vs, ok := prog.Body[0].(*ast.VarStmt)
	if !ok || len(vs.Decls) == 0 {
		t.Fatalf("expected var decl, got %#v", prog.Body)
	}
	if vs.Decls[0].Init == nil {
		t.Fatalf("initializer dropped for %q", src)
	}
	return vs.Decls[0].Init
}

func TestOptionalChainingComputedKeepsAccess(t *testing.T) {
	// `a?.[0]` must be an optional computed MemberExpr, not a bare `a`.
	e := firstExpr(t, "const x = a?.[0];")
	m, ok := e.(*ast.MemberExpr)
	if !ok {
		t.Fatalf("expected MemberExpr, got %T", e)
	}
	if !m.Optional || !m.Computed {
		t.Fatalf("expected optional+computed member, got optional=%v computed=%v", m.Optional, m.Computed)
	}
}

func TestOptionalCallKeepsCall(t *testing.T) {
	// `obj?.()` must be an optional CallExpr, not a bare `obj`.
	e := firstExpr(t, "const x = obj?.();")
	c, ok := e.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expected CallExpr, got %T", e)
	}
	if !c.Optional {
		t.Fatal("expected optional call")
	}
}

func TestOptionalChainCallAtTail(t *testing.T) {
	// `a?.b?.()` - the tail must be an optional call on a?.b.
	e := firstExpr(t, "const x = a?.b?.();")
	c, ok := e.(*ast.CallExpr)
	if !ok || !c.Optional {
		t.Fatalf("expected optional call, got %T (%#v)", e, e)
	}
}

func TestAssignmentIsRightAssociative(t *testing.T) {
	// `a = b = c` must nest as `a = (b = c)`.
	e := firstExpr(t, "const x = a = b = c;")
	outer, ok := e.(*ast.BinaryExpr)
	if !ok || outer.Op != "=" {
		t.Fatalf("expected outer assignment, got %T (%#v)", e, e)
	}
	inner, ok := outer.Right.(*ast.BinaryExpr)
	if !ok || inner.Op != "=" {
		t.Fatalf("expected nested assignment on the right, got %T", outer.Right)
	}
	if _, bad := outer.Left.(*ast.BinaryExpr); bad {
		t.Fatal("assignment parsed left-associatively")
	}
}

func TestBareForInIsForIn(t *testing.T) {
	prog := parseNoErrs(t, "for (x in obj) { }")
	fs, ok := prog.Body[0].(*ast.ForInStmt)
	if !ok {
		t.Fatalf("expected ForInStmt, got %T", prog.Body[0])
	}
	if fs.IsForOf {
		t.Fatal("expected for-in, got for-of")
	}
}

func TestBareForOfIsForOf(t *testing.T) {
	prog := parseNoErrs(t, "for (x of obj) { }")
	fs, ok := prog.Body[0].(*ast.ForInStmt)
	if !ok {
		t.Fatalf("expected ForInStmt, got %T", prog.Body[0])
	}
	if !fs.IsForOf {
		t.Fatal("expected for-of, got for-in")
	}
}

func TestArrayDestructuredParam(t *testing.T) {
	// `([a, b]) => ...` must parse the array pattern as the first parameter.
	src := "const f = ([a, b]) => a + b;"
	prog := parseNoErrs(t, src)
	vs, ok := prog.Body[0].(*ast.VarStmt)
	if !ok || len(vs.Decls) == 0 {
		t.Fatalf("expected var decl, got %#v", prog.Body)
	}
	arrow, ok := vs.Decls[0].Init.(*ast.ArrowFn)
	if !ok {
		t.Fatalf("expected ArrowFn, got %T", vs.Decls[0].Init)
	}
	if len(arrow.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(arrow.Params))
	}
	if arrow.Params[0].Pattern != "[a,b]" {
		t.Errorf("expected pattern [a,b], got %q", arrow.Params[0].Pattern)
	}
}

func TestRestNamePopulated(t *testing.T) {
	// `const [a, ...rest] = arr` must record RestName.
	prog := parseNoErrs(t, "const [a, ...rest] = arr;")
	vs := prog.Body[0].(*ast.VarStmt)
	decl := vs.Decls[0]
	if !decl.IsDestructuring {
		t.Fatal("expected destructuring decl")
	}
	if decl.RestName != "rest" {
		t.Errorf("expected RestName 'rest', got %q", decl.RestName)
	}

	// Object rest form too.
	prog2 := parseNoErrs(t, "const { a, ...rest } = obj;")
	vs2 := prog2.Body[0].(*ast.VarStmt)
	if got := vs2.Decls[0].RestName; got != "rest" {
		t.Errorf("expected object RestName 'rest', got %q", got)
	}
}

func TestLeadingDotNumber(t *testing.T) {
	e := firstExpr(t, "const x = .5;")
	lit, ok := e.(*ast.Literal)
	if !ok || lit.Kind != ast.NumberLit {
		t.Fatalf("expected NumberLit, got %T (%#v)", e, e)
	}
	if lit.Value != ".5" {
		t.Errorf("expected .5, got %q", lit.Value)
	}
}

func TestTopLevelTypeAliasIsDropped(t *testing.T) {
	// A bare `type X = ...` must be recognized and dropped, and the following
	// statement must not be swallowed.
	p := New(lexer.New("type Foo = string;\nconst x = 1;").Tokenize())
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if p.DroppedTypes() == 0 {
		t.Error("expected DroppedTypes > 0 for a bare type alias")
	}
	if len(prog.Body) != 1 {
		t.Fatalf("expected 1 surviving statement, got %d: %#v", len(prog.Body), prog.Body)
	}
	if _, ok := prog.Body[0].(*ast.VarStmt); !ok {
		t.Fatalf("expected the const to survive, got %T", prog.Body[0])
	}
}

func TestSatisfiesDropsType(t *testing.T) {
	// `x satisfies T` keeps the runtime expr and drops the type.
	p := New(lexer.New("const lvl = Level.High satisfies Level;").Tokenize())
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if p.DroppedTypes() == 0 {
		t.Error("expected DroppedTypes > 0 for satisfies")
	}
	if len(prog.Body) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(prog.Body))
	}
	init := prog.Body[0].(*ast.VarStmt).Decls[0].Init
	if _, ok := init.(*ast.MemberExpr); !ok {
		t.Fatalf("expected MemberExpr for Level.High, got %T", init)
	}
}

func TestStringEscapesDecoded(t *testing.T) {
	e := firstExpr(t, `const x = "a\nb\tc";`)
	lit, ok := e.(*ast.Literal)
	if !ok || lit.Kind != ast.StringLit {
		t.Fatalf("expected StringLit, got %T", e)
	}
	if lit.Value != "a\nb\tc" {
		t.Errorf("expected decoded escapes, got %q", lit.Value)
	}
}

func TestUnicodeEscapeDecoded(t *testing.T) {
	e := firstExpr(t, `const x = "caf\u00e9";`)
	lit := e.(*ast.Literal)
	if lit.Value != "café" {
		t.Errorf("expected café, got %q", lit.Value)
	}
}

func TestReturnAsiStopsAtNewline(t *testing.T) {
	// `return\nfoo()` returns nothing; `foo()` is a separate statement.
	prog := parseNoErrs(t, "function f() { return\nfoo(); }")
	fn := prog.Body[0].(*ast.FnDecl)
	if len(fn.Body) < 2 {
		t.Fatalf("expected return + expr statements, got %d", len(fn.Body))
	}
	ret, ok := fn.Body[0].(*ast.ReturnStmt)
	if !ok {
		t.Fatalf("expected ReturnStmt first, got %T", fn.Body[0])
	}
	if ret.Value != nil {
		t.Errorf("expected bare return (ASI), got value %#v", ret.Value)
	}
}

func TestPostfixIncrementAsi(t *testing.T) {
	// `a\n++b` must be two statements: `a;` and `++b;`.
	prog := parseNoErrs(t, "a\n++b;")
	if len(prog.Body) != 2 {
		t.Fatalf("expected 2 statements, got %d: %#v", len(prog.Body), prog.Body)
	}
	if _, ok := prog.Body[1].(*ast.ExprStmt).Expression.(*ast.UnaryExpr); !ok {
		t.Fatalf("expected prefix ++ statement, got %T", prog.Body[1].(*ast.ExprStmt).Expression)
	}
}

func TestMalformedJSXReportsError(t *testing.T) {
	p := New(lexer.New("const el = <div>hello").Tokenize())
	p.Filename = "test.tsx"
	p.ParseProgram()
	if len(p.Errors()) == 0 {
		t.Fatal("expected a diagnostic for unterminated JSX element")
	}
}

func TestJSXTextGreaterThanPreserved(t *testing.T) {
	// `a > b` inside JSX children is literal text (D2).
	prog := parseNoErrs(t, "const el = <p>a > b</p>;")
	vs := prog.Body[0].(*ast.VarStmt)
	el := vs.Decls[0].Init.(*ast.JSXElement)
	var text string
	for _, c := range el.Children {
		if tx, ok := c.(*ast.JSXText); ok {
			text += tx.Value
		}
	}
	if text != "a > b" {
		t.Errorf("expected JSX text %q, got %q", "a > b", text)
	}
}

func TestJSXTextLessThanPreserved(t *testing.T) {
	// `a < b` inside JSX children is literal text (D2).
	prog := parseNoErrs(t, "const el = <p>a < b</p>;")
	vs := prog.Body[0].(*ast.VarStmt)
	el := vs.Decls[0].Init.(*ast.JSXElement)
	var text string
	for _, c := range el.Children {
		if tx, ok := c.(*ast.JSXText); ok {
			text += tx.Value
		}
	}
	if text != "a < b" {
		t.Errorf("expected JSX text %q, got %q", "a < b", text)
	}
}
