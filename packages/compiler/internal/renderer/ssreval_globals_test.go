package renderer

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// TestSSREvalGlobalObjectStaysDynamic checks that a call reading the global
// object is not folded through the build-time JS engine, which is a bare VM
// without the run-time globals and would answer for itself.
func TestSSREvalGlobalObjectStaysDynamic(t *testing.T) {
	for _, src := range []string{
		`globalThis.hasOwnProperty("fetch")`,
		`globalThis.Boolean(globalThis.URL)`,
		`Object.keys(globalThis).length`,
		`Reflect.has(globalThis, "fetch")`,
		// Web API globals are unresolved identifiers at build time.
		`String(URL)`,
		`Boolean(globalThis.fetch && fetch)`,
	} {
		var called []string
		eval := NewSSREval(nil)
		eval.SetEvalJS(func(code string) (string, error) {
			called = append(called, code)
			return `["boolean",false]`, nil
		})
		prog := parseProg(t, "const v = "+src+";")
		init := prog.Body[0].(*ast.VarStmt).Decls[0].Init
		v := eval.value(init)
		if !v.IsUnknown() {
			t.Errorf("%s folded to %v", src, v)
		}
		for _, c := range called {
			if strings.Contains(c, "globalThis") || strings.Contains(c, "URL") {
				t.Errorf("%s reached the JS engine as %s", src, c)
			}
		}
	}
	// Calls that do not touch the global object still fold.
	eval := NewSSREval(nil)
	eval.SetEvalJS(func(code string) (string, error) { return `["string","ok"]`, nil })
	prog := parseProg(t, `const v = Symbol.keyFor(Symbol.for("a"));`)
	if v := eval.value(prog.Body[0].(*ast.VarStmt).Decls[0].Init); v.IsUnknown() {
		t.Errorf("Symbol.keyFor stopped folding: %v", v)
	}
}
