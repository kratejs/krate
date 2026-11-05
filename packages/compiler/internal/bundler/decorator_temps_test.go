package bundler

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

func decoratedProgram(t *testing.T, src string) *ast.Program {
	t.Helper()
	prog, errs := tsstrip.Parse("a.ts", src, tsstrip.Options{})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	isolateDecoratorTemps(prog, src)
	return prog
}

func topVarNames(prog *ast.Program) []string {
	var names []string
	for _, stmt := range prog.Body {
		if vs, ok := stmt.(*ast.VarStmt); ok {
			for _, d := range vs.Decls {
				if d.Init == nil {
					names = append(names, d.Name)
				}
			}
		}
	}
	return names
}

func TestDecoratorTempsArePrivatePerFile(t *testing.T) {
	a := topVarNames(decoratedProgram(t, "function log(v: any, c: any) { return v; }\nexport class A { @log m() {} }\n"))
	b := topVarNames(decoratedProgram(t, "function log(v: any, c: any) { return v; }\nexport class B { @log m() {} }\n"))
	if len(a) == 0 || len(b) == 0 {
		t.Fatalf("no temporaries: %v %v", a, b)
	}
	seen := map[string]bool{}
	for _, n := range a {
		seen[n] = true
	}
	for _, n := range b {
		if seen[n] {
			t.Errorf("%s is shared between files", n)
		}
		if !strings.Contains(n, "$d") {
			t.Errorf("%s not renamed", n)
		}
	}
}

func TestUndecoratedTempsKeepTheirNames(t *testing.T) {
	names := topVarNames(decoratedProgram(t, "var _cache;\nexport function f() { return _cache; }\n"))
	if len(names) != 1 || names[0] != "_cache" {
		t.Errorf("names = %v", names)
	}
}
