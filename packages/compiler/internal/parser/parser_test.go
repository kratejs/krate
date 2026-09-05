package parser

import (
	"testing"

	"krate-compiler/internal/ast"
	"krate-compiler/internal/lexer"
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

func TestEmptyProgram(t *testing.T) {
	prog, errs := parse(t, "")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if prog == nil {
		t.Fatal("prog is nil")
	}
}

func TestVariableDeclaration(t *testing.T) {
	prog, errs := parse(t, "const x = 42;")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	vs := firstVarStmt(t, prog)
	if vs.Kind != ast.VarConst {
		t.Errorf("expected VarConst, got %v", vs.Kind)
	}
	decl := vs.Decls[0]
	if decl.Name != "x" {
		t.Errorf("expected name x, got %q", decl.Name)
	}
}

func TestLetDeclaration(t *testing.T) {
	prog, errs := parse(t, "let y = 10;")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	vs := firstVarStmt(t, prog)
	if vs.Kind != ast.VarLet {
		t.Errorf("expected VarLet, got %v", vs.Kind)
	}
}

func TestFunctionDeclaration(t *testing.T) {
	prog, errs := parse(t, "function foo() { return 1; }")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	fn, ok := prog.Body[0].(*ast.FnDecl)
	if !ok {
		t.Fatalf("expected FnDecl, got %T", prog.Body[0])
	}
	if fn.Name != "foo" {
		t.Errorf("expected name foo, got %q", fn.Name)
	}
}

func TestExportFunction(t *testing.T) {
	prog, errs := parse(t, "export default function Page() { return <div>hello</div>; }")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	exp, ok := prog.Body[0].(*ast.ExportStmt)
	if !ok {
		t.Fatalf("expected ExportStmt, got %T", prog.Body[0])
	}
	if !exp.Default {
		t.Error("expected default export")
	}
}

func TestImport(t *testing.T) {
	prog, errs := parse(t, `import { foo as bar } from "./utils";`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	imp, ok := prog.Body[0].(*ast.ImportStmt)
	if !ok {
		t.Fatalf("expected ImportStmt, got %T", prog.Body[0])
	}
	if imp.Source != `"./utils"` {
		t.Errorf("expected source ./utils, got %q", imp.Source)
	}
	if len(imp.Named) != 1 {
		t.Fatalf("expected 1 named import, got %d", len(imp.Named))
	}
	if imp.Named[0].Local != "bar" {
		t.Errorf("expected local name 'bar', got %q", imp.Named[0].Local)
	}
	if imp.Named[0].Remote != "foo" {
		t.Errorf("expected remote name 'foo', got %q", imp.Named[0].Remote)
	}
}

func TestIfStatement(t *testing.T) {
	prog, errs := parse(t, "if (x > 0) { return 1; } else { return 2; }")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	_, ok := prog.Body[0].(*ast.IfStmt)
	if !ok {
		t.Fatalf("expected IfStmt, got %T", prog.Body[0])
	}
}

func TestForStatement(t *testing.T) {
	prog, errs := parse(t, "for (let i = 0; i < 10; i++) { }")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	_, ok := prog.Body[0].(*ast.ForStmt)
	if !ok {
		t.Fatalf("expected ForStmt, got %T", prog.Body[0])
	}
}

func TestWhileStatement(t *testing.T) {
	prog, errs := parse(t, "while (true) { break; }")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	_, ok := prog.Body[0].(*ast.WhileStmt)
	if !ok {
		t.Fatalf("expected WhileStmt, got %T", prog.Body[0])
	}
}

func TestDoWhileStatement(t *testing.T) {
	prog, errs := parse(t, "do { x--; } while (x > 0);")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	_, ok := prog.Body[0].(*ast.DoWhileStmt)
	if !ok {
		t.Fatalf("expected DoWhileStmt, got %T", prog.Body[0])
	}
}

func TestSwitchStatement(t *testing.T) {
	prog, errs := parse(t, "switch (x) { case 1: break; default: break; }")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	_, ok := prog.Body[0].(*ast.SwitchStmt)
	if !ok {
		t.Fatalf("expected SwitchStmt, got %T", prog.Body[0])
	}
}

func TestTryCatch(t *testing.T) {
	prog, errs := parse(t, "try { } catch (e) { }")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	_, ok := prog.Body[0].(*ast.TryStmt)
	if !ok {
		t.Fatalf("expected TryStmt, got %T", prog.Body[0])
	}
}

func TestThrow(t *testing.T) {
	prog, errs := parse(t, "throw new Error('fail');")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	_, ok := prog.Body[0].(*ast.ThrowStmt)
	if !ok {
		t.Fatalf("expected ThrowStmt, got %T", prog.Body[0])
	}
}

func TestNewExpression(t *testing.T) {
	prog, errs := parse(t, "const x = new Foo();")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	decl := firstVarDecl(t, prog)
	_, ok := decl.Init.(*ast.NewExpr)
	if !ok {
		t.Fatalf("expected NewExpr, got %T", decl.Init)
	}
}

func TestDynamicImport(t *testing.T) {
	prog, errs := parse(t, "const x = import('./widget.js');")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	decl := firstVarDecl(t, prog)
	di, ok := decl.Init.(*ast.DynamicImport)
	if !ok {
		t.Fatalf("expected DynamicImport, got %T", decl.Init)
	}
	lit, ok := di.Arg.(*ast.Literal)
	if !ok || lit.Kind != ast.StringLit {
		t.Fatalf("expected string literal arg, got %#v", di.Arg)
	}
}

func TestImportMeta(t *testing.T) {
	prog, errs := parse(t, "const u = new URL('../scenes/model.gltf', import.meta.url);")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	decl := firstVarDecl(t, prog)
	ne, ok := decl.Init.(*ast.NewExpr)
	if !ok {
		t.Fatalf("expected NewExpr, got %T", decl.Init)
	}
	me, ok := ne.Args[1].(*ast.MemberExpr)
	if !ok {
		t.Fatalf("expected MemberExpr for import.meta.url, got %T", ne.Args[1])
	}
	if _, ok := me.Object.(*ast.ImportMetaExpr); !ok {
		t.Fatalf("expected ImportMetaExpr, got %T", me.Object)
	}
}

func TestAwaitExpression(t *testing.T) {
	prog, errs := parse(t, "const x = await fetch('/api');")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	decl := firstVarDecl(t, prog)
	_, ok := decl.Init.(*ast.AwaitExpr)
	if !ok {
		t.Fatalf("expected AwaitExpr, got %T", decl.Init)
	}
}

func TestOptionalChaining(t *testing.T) {
	prog, errs := parse(t, "const x = a?.b;")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	decl := firstVarDecl(t, prog)
	member, ok := decl.Init.(*ast.MemberExpr)
	if !ok {
		t.Fatalf("expected MemberExpr, got %T", decl.Init)
	}
	if !member.Optional {
		t.Error("expected optional chaining")
	}
}

func TestNullishCoalescing(t *testing.T) {
	prog, errs := parse(t, "const x = a ?? b;")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	decl := firstVarDecl(t, prog)
	bin, ok := decl.Init.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", decl.Init)
	}
	if bin.Op != "??" {
		t.Errorf("expected op '??', got %q", bin.Op)
	}
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

func TestJSXTextWithApostrophes(t *testing.T) {
	// Apostrophes in JSX text must not be lexed as string delimiters (which
	// would swallow the rest of the file).
	src := `const el = <p>That page doesn't exist. Try the docs home page instead.</p>;`
	prog, errs := parse(t, src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	decl := firstVarDecl(t, prog)
	jsx, ok := decl.Init.(*ast.JSXElement)
	if !ok {
		t.Fatalf("expected JSXElement, got %T", decl.Init)
	}
	var text string
	for _, child := range jsx.Children {
		if t, ok := child.(*ast.JSXText); ok {
			text += t.Value
		}
	}
	if text != "That page doesn't exist. Try the docs home page instead." {
		t.Errorf("unexpected JSX text: %q", text)
	}
}

func TestJSXTextQuotesAfterValues(t *testing.T) {
	// Quotes directly after a value (e.g. a dimension like 5") are literal text.
	src := `<div>5" screen and it's fine</div>`
	prog, errs := parse(t, "const el = "+src+";")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	decl := firstVarDecl(t, prog)
	jsx, ok := decl.Init.(*ast.JSXElement)
	if !ok {
		t.Fatalf("expected JSXElement, got %T", decl.Init)
	}
	var text string
	for _, child := range jsx.Children {
		if t, ok := child.(*ast.JSXText); ok {
			text += t.Value
		}
	}
	if text != `5" screen and it's fine` {
		t.Errorf("unexpected JSX text: %q", text)
	}
}

func TestArrowFunction(t *testing.T) {
	prog, errs := parse(t, "const fn = () => 42;")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	decl := firstVarDecl(t, prog)
	fn, ok := decl.Init.(*ast.ArrowFn)
	if !ok {
		t.Fatalf("expected ArrowFn, got %T", decl.Init)
	}
	if len(fn.Params) != 0 {
		t.Errorf("expected 0 params, got %d", len(fn.Params))
	}
}

func TestArrowWithObjectReturn(t *testing.T) {
	prog, errs := parse(t, "const fn = () => ({ key: 42 });")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	decl := firstVarDecl(t, prog)
	_, ok := decl.Init.(*ast.ArrowFn)
	if !ok {
		t.Fatalf("expected ArrowFn, got %T", decl.Init)
	}
}

func TestCompoundAssignment(t *testing.T) {
	prog, errs := parse(t, "x += 1;")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	es, ok := prog.Body[0].(*ast.ExprStmt)
	if !ok {
		t.Fatalf("expected ExprStmt, got %T", prog.Body[0])
	}
	bin, ok := es.Expression.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("expected BinaryExpr, got %T", es.Expression)
	}
	if bin.Op != "+=" {
		t.Errorf("expected op '+=', got %q", bin.Op)
	}
}

func TestThisExpression(t *testing.T) {
	prog, errs := parse(t, "const x = this;")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	decl := firstVarDecl(t, prog)
	if _, ok := decl.Init.(*ast.ThisExpr); !ok {
		t.Fatalf("expected ThisExpr, got %T", decl.Init)
	}
}

func TestErrorFileName(t *testing.T) {
	l := lexer.New("const x = (")
	tokens := l.Tokenize()
	p := New(tokens)
	p.Filename = "myfile.tsx"
	_ = p.ParseProgram()
	errs := p.Errors()
	if len(errs) == 0 {
		t.Fatal("expected errors")
	}
	if !containsStr(errs[0].Error(), "myfile.tsx") {
		t.Errorf("expected filename in error, got: %v", errs[0])
	}
}

func containsStr(s, substr string) bool {
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
