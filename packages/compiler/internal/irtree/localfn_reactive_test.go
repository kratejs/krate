package irtree_test

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

func rootExprSlots(n *irtree.ComponentNode) []*irtree.ExprSlot {
	var out []*irtree.ExprSlot
	for _, c := range n.Children {
		if s, ok := c.(*irtree.ExprSlot); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestLocalFnCallReadingSignalBuildsExprSlot(t *testing.T) {
	tree := annotateAndBuild(t, `const list = ["a", "b"];
export default function App() {
	const [n, setN] = createSignal(0);
	const cur = () => list[n()];
	return <div><button onClick={() => setN(1)}>go</button>{cur()}</div>;
}`)
	slots := rootExprSlots(tree.Root)
	if len(slots) != 1 {
		t.Fatalf("expected one live expression slot, got %+v", slots)
	}
	if slots[0].ExprSource != "cur()" {
		t.Errorf("ExprSource = %q, want cur()", slots[0].ExprSource)
	}
}

func TestLocalFnCallWithoutSignalFolds(t *testing.T) {
	tree := annotateAndBuild(t, `const list = ["a", "b"];
export default function App() {
	const [n, setN] = createSignal(0);
	const cur = () => list[1];
	return <div><button onClick={() => setN(n() + 1)}>go</button>{cur()}</div>;
}`)
	if slots := rootExprSlots(tree.Root); len(slots) != 0 {
		t.Fatalf("static local function call should fold, got slots %+v", slots)
	}
}
