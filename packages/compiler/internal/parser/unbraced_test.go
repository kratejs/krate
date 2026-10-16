package parser

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
)

func TestUnbracedLoopBodies(t *testing.T) {
	cases := []struct {
		src   string
		check func(ast.Stmt) bool
	}{
		{"for (let i = 0; i < 3; i++) x++;", func(s ast.Stmt) bool { f, ok := s.(*ast.ForStmt); return ok && len(f.Body) == 1 }},
		{"for (;;) x++;", func(s ast.Stmt) bool { f, ok := s.(*ast.ForStmt); return ok && len(f.Body) == 1 }},
		{"for (const k in obj) use(k);", func(s ast.Stmt) bool { f, ok := s.(*ast.ForInStmt); return ok && !f.IsForOf && len(f.Body) == 1 }},
		{"for (const v of list) use(v);", func(s ast.Stmt) bool { f, ok := s.(*ast.ForInStmt); return ok && f.IsForOf && len(f.Body) == 1 }},
		{"while (x) x--;", func(s ast.Stmt) bool { w, ok := s.(*ast.WhileStmt); return ok && len(w.Body) == 1 }},
		{"do x++; while (x < 3);", func(s ast.Stmt) bool { d, ok := s.(*ast.DoWhileStmt); return ok && len(d.Body) == 1 }},
	}
	for _, c := range cases {
		prog := parseNoErrs(t, c.src)
		if len(prog.Body) == 0 {
			t.Fatalf("%q: produced no statements", c.src)
		}
		if !c.check(prog.Body[0]) {
			t.Fatalf("%q: unexpected AST shape %T", c.src, prog.Body[0])
		}
	}
}

func TestUnbracedNestedControlFlow(t *testing.T) {
	prog := parseNoErrs(t, "while (a) if (b) c(); else d();")
	w, ok := prog.Body[0].(*ast.WhileStmt)
	if !ok || len(w.Body) != 1 {
		t.Fatalf("expected while with a single statement body, got %T", prog.Body[0])
	}
	ifStmt, ok := w.Body[0].(*ast.IfStmt)
	if !ok {
		t.Fatalf("expected the while body to be an if, got %T", w.Body[0])
	}
	if len(ifStmt.Consequent) != 1 || len(ifStmt.Alternate) != 1 {
		t.Fatalf("if branches not parsed: %+v", ifStmt)
	}
}

func TestBracedLoopBodiesStillWork(t *testing.T) {
	prog := parseNoErrs(t, "for (let i = 0; i < 3; i++) { x++; y++; }")
	f, ok := prog.Body[0].(*ast.ForStmt)
	if !ok {
		t.Fatalf("expected ForStmt, got %T", prog.Body[0])
	}
	if len(f.Body) != 2 {
		t.Fatalf("expected 2 body statements, got %d", len(f.Body))
	}
}
