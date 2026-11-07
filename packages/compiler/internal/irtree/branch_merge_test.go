package irtree_test

import (
	"strings"
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

func TestSameTypeBranchesEqualPropsAreKept(t *testing.T) {
	tree := annotateAndBuild(t, formComponent+`export default function App() {
	const [on, setOn] = createSignal(false);
	return <div onClick={() => setOn(!on())}>{on() ? <Form mode="same" a="1" /> : <Form mode="same" a="2" />}</div>;
}`)
	_, forms := branchShape(tree, "Form")
	if len(forms) != 1 {
		t.Fatalf("want one Form, got %d", len(forms))
	}
	if _, ok := forms[0].Component.Props["mode"].(*ast.Literal); !ok {
		t.Errorf("an equal prop stays a plain value, got %T", forms[0].Component.Props["mode"])
	}
	if _, ok := forms[0].Component.Props["a"].(*ast.ConditionalExpr); !ok {
		t.Errorf("a differing prop switches, got %T", forms[0].Component.Props["a"])
	}
}

func TestSameTypeBranchesChain(t *testing.T) {
	tree := annotateAndBuild(t, formComponent+`export default function App() {
	const [n, setN] = createSignal(0);
	return <div onClick={() => setN(n() + 1)}>{n() === 0 ? <Form mode="a" /> : n() === 1 ? <Form mode="b" /> : <Form mode="c" />}</div>;
}`)
	conds, forms := branchShape(tree, "Form")
	if conds != 0 || len(forms) != 1 {
		t.Fatalf("a chain of one type is one instance, got %d conditional slots and %d Form slots", conds, len(forms))
	}
}

func TestSameTypeBranchesDifferentChildren(t *testing.T) {
	tree := annotateAndBuild(t, `function Card(props) {
	const [open, setOpen] = createSignal(false);
	return <div onClick={() => setOpen(!open())}>{props.children}</div>;
}
export default function App() {
	const [alt, setAlt] = createSignal(false);
	return <main onClick={() => setAlt(!alt())}>{alt() ? <Card><p>second</p></Card> : <Card><i>first</i></Card>}</main>;
}`)
	conds, cards := branchShape(tree, "Card")
	if len(cards) != 1 {
		t.Fatalf("want one Card, got %d (conditional slots: %d)", len(cards), conds)
	}
	if n := warningCodes(tree)[diag.CodeBranchStateReset]; n != 0 {
		t.Errorf("children that differ merge without a warning: %+v", tree.Warnings)
	}
}

func TestSameTypeBranchesKeepsRemountCases(t *testing.T) {
	cases := map[string]string{
		"different component types": `{on() ? <Form mode="a" /> : <Other mode="b" />}`,
		"different keys":            `{on() ? <Form key="a" mode="a" /> : <Form key="b" mode="b" />}`,
		"key on one side":           `{on() ? <Form key="a" mode="a" /> : <Form mode="b" />}`,
		"an element on one side":    `{on() ? <Form mode="a" /> : null}`,
		"intrinsic elements":        `{on() ? <p>a</p> : <p>b</p>}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			tree := annotateAndBuild(t, formComponent+`function Other(props) {
	const [x, setX] = createSignal(0);
	return <i onClick={() => setX(x() + 1)}>{props.mode}</i>;
}
export default function App() {
	const [on, setOn] = createSignal(false);
	return <div onClick={() => setOn(!on())}>`+body+`</div>;
}`)
			conds, _ := branchShape(tree, "Form")
			if conds == 0 {
				t.Fatalf("this pair must stay a conditional (React remounts it too)")
			}
			if n := warningCodes(tree)[diag.CodeBranchStateReset]; n != 0 {
				t.Errorf("an intentional remount must not warn: %+v", tree.Warnings)
			}
		})
	}
}

func TestSameTypeBranchesStatelessComponentUnchanged(t *testing.T) {
	tree := annotateAndBuild(t, `function Badge(props) {
	return <b>{props.label}</b>;
}
export default function App() {
	const [on, setOn] = createSignal(false);
	return <div onClick={() => setOn(!on())}>{on() ? <Badge label="a" /> : <Badge label="b" />}</div>;
}`)
	conds, _ := branchShape(tree, "Badge")
	if conds != 1 {
		t.Fatalf("a component with no state keeps its two branches, got %d conditional slots", conds)
	}
	if n := warningCodes(tree)[diag.CodeBranchStateReset]; n != 0 {
		t.Errorf("a stateless component loses nothing and must not warn: %+v", tree.Warnings)
	}
}

func TestSameTypeBranchesStaticTestPicksOneBranch(t *testing.T) {
	tree := annotateAndBuild(t, formComponent+`export default function App() {
	return <div>{true ? <Form mode="a" /> : <Form mode="b" />}</div>;
}`)
	conds, forms := branchShape(tree, "Form")
	if conds != 0 || len(forms) != 1 {
		t.Fatalf("a known test builds the winning branch, got %d conditional slots and %d forms", conds, len(forms))
	}
	if _, ok := forms[0].Component.Props["mode"].(*ast.Literal); !ok {
		t.Errorf("the winning branch keeps its own props, got %T", forms[0].Component.Props["mode"])
	}
}

func TestSameTypeBranchesWarnWhenNotMergeable(t *testing.T) {
	cases := map[string]string{
		"different spreads":     `{on() ? <Form {...a} /> : <Form {...b} />}`,
		"spread in other place": `{on() ? <Form mode="a" {...a} /> : <Form {...a} mode="b" />}`,
		"different refs":        `{on() ? <Form mode="a" ref={r1} /> : <Form mode="b" ref={r2} />}`,
		"render prop children":  `{on() ? <Form>{(x) => x}</Form> : <Form>{(y) => y}</Form>}`,
		"impure test":           `{(n = n + 1, on()) ? <Form mode="a" /> : <Form mode="b" />}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			tree := annotateAndBuild(t, formComponent+`const a = {}; const b = {}; const r1 = {current: null}; const r2 = {current: null};
let n = 0;
export default function App() {
	const [on, setOn] = createSignal(false);
	return <div onClick={() => setOn(!on())}>`+body+`</div>;
}`)
			if got := warningCodes(tree)[diag.CodeBranchStateReset]; got != 1 {
				t.Fatalf("want one KR8360 warning, got %d: %+v", got, tree.Warnings)
			}
			for _, w := range tree.Warnings {
				if w.Code == diag.CodeBranchStateReset && !strings.Contains(w.Hint, "key") {
					t.Errorf("hint should say how to fix it: %q", w.Hint)
				}
			}
		})
	}
}

func TestSameTypeBranchesWarnWhenWrapperIsRebuilt(t *testing.T) {
	cases := map[string]string{
		"element wrapper":    `{on() ? <section><Form mode="a" /></section> : <section><Form mode="b" /></section>}`,
		"deep wrapper":       `{on() ? <section><p>x</p><div><Form mode="a" /></div></section> : <section><p>y</p><div><Form mode="b" /></div></section>}`,
		"stateless wrapper":  `{on() ? <Shell><Form mode="a" /></Shell> : <Shell><Form mode="b" /></Shell>}`,
		"chain with wrapper": `{on() ? <section><Form mode="a" /></section> : off() ? <section><Form mode="b" /></section> : <section><Form mode="c" /></section>}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			tree := annotateAndBuild(t, formComponent+`function Shell(props) { return <div>{props.children}</div>; }
export default function App() {
	const [on, setOn] = createSignal(false);
	const [off, setOff] = createSignal(false);
	return <div onClick={() => { setOn(!on()); setOff(!off()); }}>`+body+`</div>;
}`)
			if got := warningCodes(tree)[diag.CodeBranchStateReset]; got == 0 {
				t.Fatalf("want a KR8360 warning for state inside a rebuilt wrapper, got %+v", tree.Warnings)
			}
		})
	}
}

func TestSameTypeBranchesNoWarningForDifferentWrappers(t *testing.T) {
	cases := map[string]string{
		"different tags":          `{on() ? <section><Form mode="a" /></section> : <article><Form mode="b" /></article>}`,
		"different positions":     `{on() ? <section><Form mode="a" /><i /></section> : <section><i /><Form mode="b" /></section>}`,
		"different wrapper keys":  `{on() ? <section key="a"><Form mode="a" /></section> : <section key="b"><Form mode="b" /></section>}`,
		"different inner keys":    `{on() ? <section><Form key="a" mode="a" /></section> : <section><Form key="b" mode="b" /></section>}`,
		"different inner types":   `{on() ? <section><Form mode="a" /></section> : <section><Other /></section>}`,
		"nothing stateful inside": `{on() ? <section><p>a</p></section> : <section><p>b</p></section>}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			tree := annotateAndBuild(t, formComponent+`function Other() { const [x] = createSignal(0); return <i>{x()}</i>; }
export default function App() {
	const [on, setOn] = createSignal(false);
	return <div onClick={() => setOn(!on())}>`+body+`</div>;
}`)
			if got := warningCodes(tree)[diag.CodeBranchStateReset]; got != 0 {
				t.Fatalf("React remounts this too, no warning expected: %+v", tree.Warnings)
			}
		})
	}
}
