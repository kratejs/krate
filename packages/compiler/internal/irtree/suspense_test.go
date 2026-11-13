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
