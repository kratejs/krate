package irtree

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// Aggressive DCE may only drop helpers the reference scan cannot prove are
// used. An expression slot emits `kbindContent(..., () => helper(...))` at
// runtime, so its expression is a real reference even though it is not a
// handler, effect, memo or attribute binding.
func TestCollectReferencedFunctionsScansExprSlots(t *testing.T) {
	src := `function Counter(props) {
  function label(value) { return String(value); }
  return null;
}`
	p := parser.New(lexer.New(src).Tokenize())
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	fn, ok := prog.Body[0].(*ast.FnDecl)
	if !ok {
		t.Fatalf("expected FnDecl, got %T", prog.Body[0])
	}
	label, ok := fn.Body[0].(*ast.FnDecl)
	if !ok {
		t.Fatalf("expected local FnDecl, got %T", fn.Body[0])
	}

	b := &builder{
		aggressiveDCE: true,
		functions:     map[string]*ast.FnDecl{label.Name: label},
		ann:           &Annotations{},
	}
	node := &ComponentNode{
		Children: []SlotNode{&ExprSlot{ExprSource: "label(props.value)"}},
	}
	fns := b.collectReferencedFunctions(node, fn.Body)
	for _, got := range fns {
		if got.Name == label.Name {
			return
		}
	}
	t.Fatalf("helper used by expression slot was dropped: %v", fns)
}

func TestIdSafeMatchesTheRuntimeRule(t *testing.T) {
	for in, want := range map[string]string{
		"L:$a-b_c.d": "L:$a-b_c.d",
		"L:$a b/ü":   "L:$a%20b%2F%C3%BC",
		"x\x00y":     "x%00y",
	} {
		if got := idSafe(in); got != want {
			t.Errorf("idSafe(%q) = %q, want %q", in, got, want)
		}
	}
}
