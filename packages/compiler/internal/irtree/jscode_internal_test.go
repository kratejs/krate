package irtree

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func parseForTest(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(src).Tokenize())
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}

func exprOf(t *testing.T, src string) ast.Expr {
	t.Helper()
	prog := parseForTest(t, src)
	es, ok := prog.Body[0].(*ast.ExprStmt)
	if !ok {
		t.Fatalf("expected ExprStmt, got %T", prog.Body[0])
	}
	return es.Expression
}

func stmtOf(t *testing.T, src string) ast.Stmt {
	t.Helper()
	return parseForTest(t, src).Body[0]
}

func flat(s string) string { return strings.Join(strings.Fields(s), "") }

func TestGenerateOptionalCall(t *testing.T) {
	got := generateExprJS(exprOf(t, "a?.()"), nil)
	if !strings.Contains(got, "a?.()") {
		t.Errorf("optional call not preserved: %q", got)
	}
}

func TestGenerateAndPreservesSemantics(t *testing.T) {
	got := generateExprJS(exprOf(t, "a && b"), nil)
	if flat(got) != "(a&&b)" {
		t.Errorf("&& should keep JS semantics, got %q", got)
	}
}

func TestGenerateObjectMethod(t *testing.T) {
	got := generateExprJS(exprOf(t, "({ run() { return 1; } })"), nil)
	if !strings.Contains(got, "run() {") || !strings.Contains(got, "return 1;") {
		t.Errorf("object method shorthand not emitted: %q", got)
	}
}

func TestRenderForInAndForOf(t *testing.T) {
	in := renderStmtJS(stmtOf(t, "for (const k in obj) { use(k); }"), nil)
	if !strings.Contains(in, "for (const k in obj) {") {
		t.Errorf("for-in not emitted correctly: %q", in)
	}
	of := renderStmtJS(stmtOf(t, "for (const v of list) { use(v); }"), nil)
	if !strings.Contains(of, "for (const v of list) {") {
		t.Errorf("for-of not emitted correctly: %q", of)
	}
	bare := renderStmtJS(stmtOf(t, "for (k in obj) { use(k); }"), nil)
	if !strings.Contains(bare, "for (k in obj) {") {
		t.Errorf("bare for-in not emitted correctly: %q", bare)
	}
}

func TestCodegenIssuesCleanForSupportedConstructs(t *testing.T) {
	prog := parseForTest(t, `function App() {
		for (const k in obj) { use(k); }
		const f = (a, b = 1, ...rest) => a + b;
		const o = { run() { return f?.(1); } };
		return o;
	}`)
	fn, ok := prog.Body[0].(*ast.FnDecl)
	if !ok {
		t.Fatalf("expected FnDecl, got %T", prog.Body[0])
	}
	if issues := codegenIssues(fn); len(issues) > 0 {
		t.Errorf("unexpected codegen issues for supported constructs: %v", issues)
	}
}

func TestClientLinkLowering(t *testing.T) {
	prog := parseForTest(t, `function App() {
		return <Link href="/about" className="nav">About</Link>;
	}`)
	fn, ok := prog.Body[0].(*ast.FnDecl)
	if !ok {
		t.Fatalf("expected FnDecl, got %T", prog.Body[0])
	}
	got := flat(RenderComponentFnJS(fn))
	for _, want := range []string{"h('a',{", "'data-krate-link':true", "'data-prefetch':(true)", "class:'nav'", "'About'"} {
		if !strings.Contains(got, want) {
			t.Errorf("client <Link> lowering missing %q in: %s", want, got)
		}
	}
}

func TestClientLinkExternalAndPrefetchOff(t *testing.T) {
	prog := parseForTest(t, `function App() {
		return <Link href="https://x.test" external>X</Link>;
	}`)
	fn := prog.Body[0].(*ast.FnDecl)
	got := flat(RenderComponentFnJS(fn))
	if !strings.Contains(got, "'data-krate-external':true") {
		t.Errorf("external <Link> not lowered: %s", got)
	}
	if strings.Contains(got, "'data-krate-link':true") {
		t.Errorf("external <Link> must not get data-krate-link: %s", got)
	}

	pf := parseForTest(t, `function App() {
		return <Link href="/a" prefetch={false}>A</Link>;
	}`)
	got2 := flat(RenderComponentFnJS(pf.Body[0].(*ast.FnDecl)))
	if !strings.Contains(got2, "'data-prefetch':(false)") {
		t.Errorf("prefetch={false} not preserved: %s", got2)
	}
}
