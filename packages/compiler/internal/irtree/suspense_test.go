package irtree_test

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

func warningCodes(tree *irtree.ComponentTree) map[diag.Code]int {
	out := map[diag.Code]int{}
	for _, w := range tree.Warnings {
		out[w.Code]++
	}
	return out
}

// The sources below are already lowered by the React pass (use(p) became
// __krateUse(() => __krateRead(p))).

func TestUseOutsideSuspenseWarns(t *testing.T) {
	tree := annotateAndBuild(t, `const p = Promise.resolve({ name: 'x' });
function User() {
	const user = __krateUse(() => __krateRead(p));
	return <p>{user().name}</p>;
}
export default function App() {
	return <main><User /></main>;
}`)
	if warningCodes(tree)[diag.CodeSuspendNoBoundary] != 1 {
		t.Fatalf("expected one KR4008 warning, got %+v", tree.Warnings)
	}
}

func TestUseInsideSuspenseDoesNotWarn(t *testing.T) {
	tree := annotateAndBuild(t, `const p = Promise.resolve({ name: 'x' });
function User() {
	const user = __krateUse(() => __krateRead(p));
	return <p>{user().name}</p>;
}
export default function App() {
	return <Suspense fallback={<i>wait</i>}><User /></Suspense>;
}`)
	if n := warningCodes(tree)[diag.CodeSuspendNoBoundary]; n != 0 {
		t.Fatalf("unexpected KR4008 warning: %+v", tree.Warnings)
	}
}

func TestUseComponentHydratesWithoutSignals(t *testing.T) {
	tree := annotateAndBuild(t, `const p = Promise.resolve({ name: 'x' });
function User() {
	const user = __krateUse(() => __krateRead(p));
	return <p>{user().name}</p>;
}
export default function App() {
	return <Suspense fallback={<i>wait</i>}><User /></Suspense>;
}`)
	var found *irtree.ComponentNode
	var walk func(nodes []irtree.SlotNode)
	walk = func(nodes []irtree.SlotNode) {
		for _, n := range nodes {
			switch s := n.(type) {
			case *irtree.ComponentSlot:
				if s.Component.Name == "User" {
					found = s.Component
				}
				walk(s.Component.Children)
			case *irtree.SuspenseSlot:
				walk(s.Resolved)
			}
		}
	}
	walk(tree.Root.Children)
	if found == nil {
		t.Fatal("User was not built")
	}
	if found.Tier != irtree.TierClient || found.IsSSREval {
		t.Fatalf("User must hydrate (client, not SSREval), got tier %v ssreval %v", found.Tier, found.IsSSREval)
	}
	declared := false
	for _, ev := range append(found.PreSignalVars, found.ExtraVars...) {
		if ev == "var user=__krateUse(()=>__krateRead(p))" {
			declared = true
		}
	}
	if !declared {
		t.Fatalf("expected the use() value declared once in the hydration scope, got %q", append(found.PreSignalVars, found.ExtraVars...))
	}
}

// A signal initialized from a use() value (useState(use(p))) is declared
// after that value: the use() declaration reads no signal, so it is emitted
// with the pre-signal fragments.
func TestUseValueDeclaredBeforeSignalsInitializedFromIt(t *testing.T) {
	tree := annotateAndBuild(t, `const p = Promise.resolve([1]);
function List() {
	const loaded = __krateUse(() => __krateRead(p));
	const [items, setItems] = createSignal(loaded());
	return <ul onClick={() => setItems([])}>{items().length}</ul>;
}
export default function App() {
	return <Suspense fallback={<i>wait</i>}><List /></Suspense>;
}`)
	var found *irtree.ComponentNode
	for _, n := range tree.Root.Children {
		if s, ok := n.(*irtree.SuspenseSlot); ok {
			for _, r := range s.Resolved {
				if cs, ok := r.(*irtree.ComponentSlot); ok {
					found = cs.Component
				}
			}
		}
	}
	if found == nil {
		t.Fatal("List was not built")
	}
	pre := false
	for _, v := range found.PreSignalVars {
		if v == "var loaded=__krateUse(()=>__krateRead(p))" {
			pre = true
		}
	}
	if !pre {
		t.Fatalf("the use() value must precede the signal declarations, pre=%q extra=%q", found.PreSignalVars, found.ExtraVars)
	}
}
