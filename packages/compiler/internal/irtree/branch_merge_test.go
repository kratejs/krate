package irtree_test

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

func walkSlots(slots []irtree.SlotNode, visit func(irtree.SlotNode)) {
	for _, s := range slots {
		visit(s)
		switch n := s.(type) {
		case *irtree.ComponentSlot:
			if n.Component != nil {
				walkSlots(n.Component.Children, visit)
				walkSlots(n.Component.ReturnSlots, visit)
			}
		case *irtree.ConditionalSlot:
			walkSlots(n.Consequent, visit)
			walkSlots(n.Alternate, visit)
		}
	}
}

// branchShape counts the conditional slots and the component slots named name.
func branchShape(tree *irtree.ComponentTree, name string) (conds int, comps []*irtree.ComponentSlot) {
	walkSlots(tree.Root.Children, func(s irtree.SlotNode) {
		switch n := s.(type) {
		case *irtree.ConditionalSlot:
			conds++
		case *irtree.ComponentSlot:
			if n.Component != nil && n.Component.Name == name {
				comps = append(comps, n)
			}
		}
	})
	return conds, comps
}

const formComponent = `function Form(props) {
	const [text, setText] = createSignal('');
	createEffect(() => { console.log('mount'); });
	return <form><b>{props.mode}</b><input value={text()} onInput={(e) => setText(e.target.value)} /></form>;
}
`

func TestSameTypeBranchesBuildOneInstance(t *testing.T) {
	tree := annotateAndBuild(t, formComponent+`export default function App() {
	const [isEdit, setEdit] = createSignal(false);
	return <div><button onClick={() => setEdit(!isEdit())}>flip</button>{isEdit() ? <Form mode="edit" hint="h" /> : <Form mode="new" />}</div>;
}`)
	conds, forms := branchShape(tree, "Form")
	if conds != 0 || len(forms) != 1 {
		t.Fatalf("want one Form and no conditional slot, got %d conditional slots and %d Form slots", conds, len(forms))
	}
	props := forms[0].Component.Props
	mode, ok := props["mode"].(*ast.ConditionalExpr)
	if !ok {
		t.Fatalf("mode should switch with the condition, got %T", props["mode"])
	}
	if _, ok := mode.Test.(*ast.CallExpr); !ok {
		t.Errorf("test = %T, want the isEdit() read", mode.Test)
	}
	if _, ok := props["hint"].(*ast.ConditionalExpr); !ok {
		t.Errorf("a prop on one branch only should switch to undefined, got %T", props["hint"])
	}
	if n := warningCodes(tree)[diag.CodeBranchStateReset]; n != 0 {
		t.Errorf("a merged pair must not warn: %+v", tree.Warnings)
	}
}
