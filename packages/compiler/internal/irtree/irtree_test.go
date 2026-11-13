package irtree_test

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func parseProg(t *testing.T, src string) *ast.Program {
	t.Helper()
	l := lexer.New(src)
	tokens := l.Tokenize()
	p := parser.New(tokens)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}

func annotateAndBuild(t *testing.T, src string) *irtree.ComponentTree {
	t.Helper()
	prog := parseProg(t, src)
	ann := annotator.Annotate(prog, &config.Config{}, "test.tsx", src)
	return irtree.Build(prog, ann)
}

func TestBuildConditional(t *testing.T) {
	tree := annotateAndBuild(t, `export default function App() {
	const [show, setShow] = createSignal(false);
	return <div>{show() ? <span>yes</span> : <span>no</span>}</div>;
}`)
	if len(tree.Root.Children) == 0 {
		t.Fatal("expected children")
	}
	// Dynamic conditional is now a separate SlotNode
	static, ok := tree.Root.Children[0].(*irtree.StaticHTML)
	if !ok {
		t.Fatalf("expected StaticHTML, got %T", tree.Root.Children[0])
	}
	if !strings.Contains(static.HTML, "<div>") {
		t.Errorf("expected opening div tag, got %q", static.HTML)
	}
	// Conditional with a signal-backed test stays reactive as a
	// ConditionalSlot with both branches built (render-both). The test is
	// not baked statically so toggling show() swaps the visible branch.
	foundCond := false
	for _, child := range tree.Root.Children {
		if cond, ok := child.(*irtree.ConditionalSlot); ok {
			foundCond = true
			if cond.TestJS == "" {
				t.Error("expected TestJS on conditional slot")
			}
			foundYes := false
			for _, c := range cond.Consequent {
				if sh, ok := c.(*irtree.StaticHTML); ok && strings.Contains(sh.HTML, "<span>yes</span>") {
					foundYes = true
					break
				}
			}
			if !foundYes {
				t.Error("expected consequent branch <span>yes</span> in conditional slot")
			}
			foundNo := false
			for _, c := range cond.Alternate {
				if sh, ok := c.(*irtree.StaticHTML); ok && strings.Contains(sh.HTML, "<span>no</span>") {
					foundNo = true
					break
				}
			}
			if !foundNo {
				t.Error("expected alternate branch <span>no</span> in conditional slot")
			}
			break
		}
	}
	if !foundCond {
		t.Error("expected ConditionalSlot for signal-backed ternary")
	}
}

func TestBuildList(t *testing.T) {
	tree := annotateAndBuild(t, `export default function App() {
	const [items, setItems] = createSignal(["a","b","c"]);
	return <ul>{items().map(item => <li>{item}</li>)}</ul>;
}`)
	if len(tree.Root.Children) == 0 {
		t.Fatal("expected children")
	}
	// Dynamic list is now a separate SlotNode
	static, ok := tree.Root.Children[0].(*irtree.StaticHTML)
	if !ok {
		t.Fatalf("expected StaticHTML, got %T", tree.Root.Children[0])
	}
	if !strings.Contains(static.HTML, "<ul>") {
		t.Errorf("expected opening ul tag, got %q", static.HTML)
	}
	// Should have a ListSlot as a separate child
	found := false
	for _, child := range tree.Root.Children {
		if _, ok := child.(*irtree.ListSlot); ok {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected ListSlot among children")
	}
}
