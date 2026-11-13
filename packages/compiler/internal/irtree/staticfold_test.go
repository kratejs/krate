package irtree_test

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/native/jsruntime"
)

func buildStaticTreeWithJS(t *testing.T, src string) *irtree.ComponentTree {
	t.Helper()
	prog := parseProg(t, src)
	ann := annotator.Annotate(prog, &config.Config{}, "test.tsx", src)
	return irtree.BuildWithOptions(prog, ann, irtree.BuildOptions{EvalJS: jsruntime.EvaluateExpr})
}

func TestStaticFoldingMapCallbackShapes(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "destructured object",
			src: `export default function App() {
	const items = [{id: 1, name: "A"}, {id: 2, name: "B"}];
	return <ul>{items.map(({ id, name }) => <li key={id}>{name}</li>)}</ul>;
}`,
			want: `<ul><li>A</li><li>B</li></ul>`,
		},
		{
			name: "named callback",
			src: `export default function App() {
	const renderRow = (item) => <li>{item}</li>;
	return <ul>{["a","b"].map(renderRow)}</ul>;
}`,
			want: `<ul><li>a</li><li>b</li></ul>`,
		},
		{
			name: "function declaration callback",
			src: `export default function App() {
	function renderRow(item, i) { return <li>{i}:{item}</li>; }
	return <ul>{["a","b"].map(renderRow)}</ul>;
}`,
			want: `<ul><li>0:a</li><li>1:b</li></ul>`,
		},
		{
			name: "object entries destructuring",
			src: `export default function App() {
	const stats = { a: 1, b: 2 };
	return <ul>{Object.entries(stats).map(([k, v]) => <li>{k}:{v}</li>)}</ul>;
}`,
			want: `<ul><li>a:1</li><li>b:2</li></ul>`,
		},
		{
			name: "nested destructuring",
			src: `export default function App() {
	const items = [{ meta: { name: "A" } }, { meta: { name: "B" } }];
	return <ul>{items.map(({ meta: { name } }) => <li>{name}</li>)}</ul>;
}`,
			want: `<ul><li>A</li><li>B</li></ul>`,
		},
		{
			name: "callback local destructuring",
			src: `export default function App() {
	const items = [{ name: "A" }, { name: "B" }];
	return <ul>{items.map(item => { const { name } = item; return <li>{name}</li>; })}</ul>;
}`,
			want: `<ul><li>A</li><li>B</li></ul>`,
		},
		{
			name: "array parameter",
			src: `export default function App() {
	const items = ["a", "b"];
	return <ul>{items.map((item, i, all) => <li>{item}:{i}:{all.length}</li>)}</ul>;
}`,
			want: `<ul><li>a:0:2</li><li>b:1:2</li></ul>`,
		},
		{
			name: "loop built array",
			src: `export default function App() {
	const items = [];
	for (let i = 0; i < 3; i++) items.push("i" + i);
	return <ul>{items.map(x => <li>{x}</li>)}</ul>;
}`,
			want: `<ul><li>i0</li><li>i1</li><li>i2</li></ul>`,
		},
		{
			name: "flatMap arrays",
			src: `export default function App() {
	const items = [1, 2];
	return <ul>{items.flatMap(n => [<li key={n}>a{n}</li>, <li key={n}>b{n}</li>])}</ul>;
}`,
			want: `<ul><li>a1</li><li>b1</li><li>a2</li><li>b2</li></ul>`,
		},
		{
			name: "rest parameter",
			src: `export default function App() {
	return <ul>{["a","b"].map((first, ...rest) => <li>{first}:{rest[0]}</li>)}</ul>;
}`,
			want: `<ul><li>a:0</li><li>b:1</li></ul>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := annotateAndBuild(t, tc.src)
			if containsListSlot(tree.Root.Children) {
				t.Fatal("fully static map must not emit a ListSlot")
			}
			if got := renderStaticHTML(tree.Root.Children); got != tc.want {
				t.Fatalf("HTML = %q, want %q", got, tc.want)
			}
		})
	}
}

func containsListSlot(nodes []irtree.SlotNode) bool {
	for _, n := range nodes {
		switch c := n.(type) {
		case *irtree.ListSlot:
			return true
		case *irtree.ComponentSlot:
			if c.Component != nil && (containsListSlot(c.Component.Children) || containsListSlot(c.Component.ReturnSlots)) {
				return true
			}
		case *irtree.ConditionalSlot:
			if containsListSlot(c.Consequent) || containsListSlot(c.Alternate) {
				return true
			}
		}
	}
	return false
}
