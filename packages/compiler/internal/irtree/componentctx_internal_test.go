package irtree

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// leakPage interleaves components that install unusual per-component state
// (an early return, a dynamic context provider and its reader, CSS-free
// signals) with plain siblings.
const leakPage = `
const Theme = createContext("light");
function Early(props) {
  const [n, setN] = createSignal(0);
  if (!props.show) return <p class="early">hidden</p>;
  return <p class="early" onClick={() => setN(n() + 1)}>{n()}</p>;
}
function ThemeBox(props) {
  const [t, setT] = createSignal("dark");
  return <Theme.Provider value={t()}><section onClick={() => setT("x")}><Reader /><Plain k="c" /></section></Theme.Provider>;
}
function Reader() {
  const theme = Theme.useContext();
  return <i>{theme}</i>;
}
function Plain(props) {
  const [c, setC] = createSignal(1);
  const label = "L" + props.k;
  return <button title={label} onClick={() => setC(c() + 1)}>{c()}</button>;
}
export default function Page() {
  return <main>
    <Plain k="a" />
    <Early show={false} />
    <Plain k="b" />
    <ThemeBox />
    <Plain k="d" />
  </main>;
}
`

func buildLeakPage(t *testing.T) (*builder, *ComponentNode) {
	t.Helper()
	p := parser.New(lexer.New(leakPage).Tokenize())
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse: %v", errs)
	}
	fns := make(map[string]*ast.FnDecl)
	tiers := make(map[string]ComponentTier)
	for _, stmt := range prog.Body {
		switch s := stmt.(type) {
		case *ast.FnDecl:
			fns[s.Name] = s
		case *ast.ExportStmt:
			if fn, ok := s.Declaration.(*ast.FnDecl); ok {
				fns[fn.Name] = fn
			}
		}
	}
	for name := range fns {
		tiers[name] = TierClient
	}
	ann := &Annotations{
		EntryPoint:     "Page",
		Functions:      fns,
		ComponentTiers: tiers,
		SourceFile:     "leak.tsx",
	}
	b := newBuilder(prog, ann, BuildOptions{})
	return b, b.buildComponentNode(fns["Page"], "")
}

// TestComponentBuildRestoresContext checks that building a component tree
// leaves no per-component state behind: the context stack is balanced and the
// active context is back to its initial (zero) value.
func TestComponentBuildRestoresContext(t *testing.T) {
	b, root := buildLeakPage(t)
	if root == nil || len(root.Children) == 0 {
		t.Fatal("page built no slots")
	}
	if len(b.ctxStack) != 0 {
		t.Fatalf("context stack not balanced: depth %d", len(b.ctxStack))
	}
	if !reflect.DeepEqual(b.componentCtx, componentCtx{}) {
		t.Fatalf("component context leaked out of the build: %+v", b.componentCtx)
	}
}

// TestSiblingComponentsDoNotLeakState checks that a component built after
// siblings with an early return or a context provider is built exactly like
// one built before them.
func TestSiblingComponentsDoNotLeakState(t *testing.T) {
	_, root := buildLeakPage(t)
	var plains, provided []*ComponentNode
	var walk func(slots []SlotNode, inProvider bool)
	walk = func(slots []SlotNode, inProvider bool) {
		for _, s := range slots {
			cs, ok := s.(*ComponentSlot)
			if !ok || cs.Component == nil {
				continue
			}
			n := cs.Component
			if n.Name == "Plain" && !inProvider {
				plains = append(plains, n)
			}
			if n.Name == "Plain" && inProvider {
				provided = append(provided, n)
			}
			walk(n.Children, inProvider || n.Name == "ThemeBox")
			walk(n.CallSiteSlots, inProvider)
		}
	}
	walk(root.Children, false)
	if len(plains) != 3 || len(provided) != 1 {
		t.Fatalf("found %d Plain instances outside the provider and %d inside, want 3 and 1", len(plains), len(provided))
	}
	// The provider's dynamic context reaches its own subtree only.
	if _, ok := provided[0].Props[contextPropPrefix+"Theme"]; !ok {
		t.Fatalf("Plain inside the provider did not receive the context prop: %v", provided[0].Props)
	}
	shape := func(n *ComponentNode) [6]int {
		return [6]int{len(n.Signals), len(n.Handlers), len(n.AttrBindings), len(n.ExtraVars), len(n.PreSignalVars), int(n.Tier)}
	}
	// Instance-specific parts: the slot ID and the folded `k` prop.
	vars := func(n *ComponentNode) []string {
		k := n.Props["k"].(*ast.Literal).Value
		var out []string
		for _, v := range append(append([]string(nil), n.PreSignalVars...), n.ExtraVars...) {
			v = strings.ReplaceAll(v, string(n.ID), "ID")
			out = append(out, strings.ReplaceAll(v, "'L"+k+"'", "'L?'"))
		}
		return out
	}
	first := plains[0]
	for _, n := range plains[1:] {
		if shape(n) != shape(first) {
			t.Errorf("%s shape %v differs from %s %v", n.ID, shape(n), first.ID, shape(first))
		}
		if !reflect.DeepEqual(vars(n), vars(first)) {
			t.Errorf("%s hydration vars %q differ from %s %q", n.ID, vars(n), first.ID, vars(first))
		}
		if n.Handlers[0].Body != first.Handlers[0].Body {
			t.Errorf("%s handler %q differs from %q", n.ID, n.Handlers[0].Body, first.Handlers[0].Body)
		}
		if _, ok := n.Props[contextPropPrefix+"Theme"]; ok || len(n.Props) != 1 {
			t.Errorf("%s received props %v from a sibling's context", n.ID, n.Props)
		}
	}
}
