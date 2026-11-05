package irtree

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/astprint"
)

func exprStmt(prog *ast.Program) ast.Expr {
	for _, s := range prog.Body {
		if es, ok := s.(*ast.ExprStmt); ok {
			return es.Expression
		}
	}
	return nil
}

// Copying an object literal keeps its computed keys, and substitution reaches
// the key expression: `{ [K]: v }` must not become `{ K: v }`.
func TestCloneObjectKeepsComputedKeys(t *testing.T) {
	prog := parseModule(t, "({ [K]: 1, [`a${n}`]: 2, plain: 3, get [G]() { return 4; } });")
	obj := exprStmt(prog)
	if obj == nil {
		t.Fatal("no expression")
	}
	cloned := cloneExpr(obj)
	out := astprint.PrintExpr(cloned)
	for _, want := range []string{"[K]: 1", "[`a${n}`]: 2", "plain: 3", "get [G]()"} {
		if !strings.Contains(out, want) {
			t.Errorf("clone lost %q:\n%s", want, out)
		}
	}
	// Substitution maps the key expression too.
	renamed := cloneObjectProp(obj.(*ast.ObjectExpr).Properties[0], func(e ast.Expr) ast.Expr {
		if id, ok := e.(*ast.Identifier); ok && id.Name == "K" {
			return &ast.Identifier{Name: "Q"}
		}
		return e
	})
	if !renamed.Computed || astprint.PrintExpr(renamed.KeyExpr) != "Q" {
		t.Errorf("key expression not substituted: %+v", renamed)
	}
}

// A cva factory whose config names a variant by expression is not folded: its
// variants would be registered under the expression's spelling.
func TestCVAFactoryWithComputedKeyIsNotFolded(t *testing.T) {
	prog := parseModule(t, `
		const v = cva("base", { variants: { [name]: { a: "x" } } });
		const w = cva("base", { variants: { size: { sm: "h-1" } } });
	`)
	specs := CollectCVAFactories(prog)
	if _, ok := specs["v"]; ok {
		t.Error("a factory with a computed variant name was folded")
	}
	if _, ok := specs["w"]; !ok {
		t.Error("an ordinary factory was dropped")
	}
}

// A Go component prop object serializes a computed key by its value; a key
// that cannot be resolved makes the whole value unavailable rather than
// writing the key's spelling.
func TestSerializeJSONComputedKey(t *testing.T) {
	prog := parseModule(t, "({ [K]: 1, b: 2 });")
	obj := exprStmt(prog)
	resolve := func(e ast.Expr) (string, bool) {
		if id, ok := e.(*ast.Identifier); ok && id.Name == "K" {
			return "color", true
		}
		return "", false
	}
	if got := SerializeJSON(obj, resolve); got != `{"color":1,"b":2}` {
		t.Errorf("resolved key: %s", got)
	}
	if got := SerializeJSON(obj, func(ast.Expr) (string, bool) { return "", false }); got != "null" {
		t.Errorf("unresolved key: %s", got)
	}
}
