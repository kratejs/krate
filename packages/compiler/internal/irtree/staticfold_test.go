package irtree_test

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
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

func TestStaticMapKeepsRuntimeForUnsupportedCallbacks(t *testing.T) {
	cases := map[string]string{
		"block let": `export default function App() {
	const items = [1, 2];
	return <ul>{items.map(item => { let n = item * 2; return <li>{n}</li>; })}</ul>;
}`,
		"early return": `export default function App() {
	const items = [1, 2];
	return <ul>{items.map(item => { if (item > 1) return <li>big</li>; return <li>small</li>; })}</ul>;
}`,
		"async": `export default function App() {
	const items = [1, 2];
	return <ul>{items.map(async item => <li>{item}</li>)}</ul>;
}`,
		"dynamic const": `export default function App() {
	const items = [1, 2];
	return <ul>{items.map(item => { const label = getLabel(item); return <li>{label}</li>; })}</ul>;
}`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			tree := annotateAndBuild(t, src)
			if !containsListSlot(tree.Root.Children) {
				t.Fatal("callback shape must stay on the runtime list path")
			}
		})
	}
}

func TestStaticFlatMapBlockLocals(t *testing.T) {
	tree := annotateAndBuild(t, `export default function App() {
	const items = [1, 2];
	return <ul>{items.flatMap(n => { const label = "n" + n; return [<li key={n}>{label}</li>]; })}</ul>;
}`)
	if containsListSlot(tree.Root.Children) {
		t.Fatal("known flatMap must not emit a ListSlot")
	}
	if got, want := renderStaticHTML(tree.Root.Children), `<ul><li>n1</li><li>n2</li></ul>`; got != want {
		t.Fatalf("HTML = %q, want %q", got, want)
	}
}

func TestStaticMapTransformsLateIconsAndImages(t *testing.T) {
	const src = `const media = [{ icon: "lucide:github", src: "/hero.png" }, { icon: "tabler:menu", src: "/other.png" }];
export default function App() {
	return <div>{media.map((m) => <a><Icon name={m.icon} /><Image src={m.src} alt="x" /></a>)}</div>;
}`
	iconCalls, imageCalls := 0, 0
	prog := parseProg(t, src)
	ann := annotator.Annotate(prog, &config.Config{}, "test.tsx", src)
	tree := irtree.BuildWithOptions(prog, ann, irtree.BuildOptions{
		TransformIcon: func(*ast.JSXElement) *ast.JSXElement {
			iconCalls++
			return &ast.JSXElement{
				Opening: &ast.JSXOpening{Name: "svg", SelfClosing: false},
				Closing: &ast.JSXClosing{Name: "svg"},
			}
		},
		TransformImage: func(*ast.JSXElement) *ast.JSXElement {
			imageCalls++
			return &ast.JSXElement{Opening: &ast.JSXOpening{Name: "img", SelfClosing: true}}
		},
	})
	html := renderStaticHTML(tree.Root.Children)
	if iconCalls != 2 || imageCalls != 2 {
		t.Fatalf("transform calls = icon:%d image:%d, want 2 each", iconCalls, imageCalls)
	}
	if got := strings.Count(html, "<svg></svg>"); got != 2 {
		t.Errorf("rendered %d transformed icons in %q", got, html)
	}
	if got := strings.Count(html, "<img"); got != 2 {
		t.Errorf("rendered %d transformed images in %q", got, html)
	}
}

func TestStaticFoldingLocalValues(t *testing.T) {
	tree := annotateAndBuild(t, `export default function App() {
	const items = [1, 2, 3];
	const doubled = items.map(n => n * 2);
	const label = (n) => "N" + n;
	const { name } = { name: "Ada" };
	const [first] = ["x", "y"];
	const rows = [];
	for (let i = 0; i < 3; i++) rows.push(i);
	return <div>{doubled.join(",")}-{label(3)}-{rows.join(",")}-{name}-{first}</div>;
}`)
	if got, want := renderStaticHTML(tree.Root.Children), `<div>2,4,6-N3-0,1,2-Ada-x</div>`; got != want {
		t.Fatalf("HTML = %q, want %q", got, want)
	}
}

func TestStaticFoldingWhileAndLocalHelpers(t *testing.T) {
	tree := annotateAndBuild(t, `export default function App() {
	const items = [];
	let i = 0;
	while (i < 3) { items.push("w" + i); i++; }
	const label = (n) => "L" + n;
	const { name } = { name: "Ada" };
	return <div data-label={label(3)}>{items.join(",")}:{name}</div>;
}`)
	if len(tree.Root.AttrBindings) != 0 {
		t.Fatalf("known helper attribute emitted a binding: %+v", tree.Root.AttrBindings)
	}
	got := renderStaticHTML(tree.Root.Children)
	if !strings.Contains(got, `data-label="L3"`) || !strings.Contains(got, "w0,w1,w2:Ada") {
		t.Fatalf("while/local fold HTML = %q", got)
	}
}

func TestStaticFoldingObjectSpreadAttributes(t *testing.T) {
	tree := annotateAndBuild(t, `export default function App() {
	const attrs = { title: "hello", "data-count": 3 };
	return <div {...attrs}>x</div>;
}`)
	got := renderStaticHTML(tree.Root.Children)
	if !strings.Contains(got, `title="hello"`) || !strings.Contains(got, `data-count="3"`) {
		t.Fatalf("static object spread was not folded: %q", got)
	}
}

func TestStaticFoldingForOf(t *testing.T) {
	tree := annotateAndBuild(t, `export default function App() {
	const out = [];
	for (const n of [1, 2, 3]) out.push(n * 2);
	return <div>{out.join(",")}</div>;
}`)
	if got, want := renderStaticHTML(tree.Root.Children), `<div>2,4,6</div>`; got != want {
		t.Fatalf("for-of fold HTML = %q, want %q", got, want)
	}
}

func TestStaticFoldingAttributes(t *testing.T) {
	tree := annotateAndBuild(t, `export default function App() {
	const items = [1, 2, 3];
	return <div data-count={items.length} data-total={items.reduce((a, b) => a + b, 0)} data-label={"ok".toUpperCase()} data-max={Math.max(2, 7)}>x</div>;
}`)
	if len(tree.Root.AttrBindings) != 0 {
		t.Fatalf("known attributes emitted hydration bindings: %+v", tree.Root.AttrBindings)
	}
	html := renderStaticHTML(tree.Root.Children)
	for _, want := range []string{`data-count="3"`, `data-total="6"`, `data-label="OK"`, `data-max="7"`} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %s in %s", want, html)
		}
	}
}

func TestReactiveMapStillUsesListSlot(t *testing.T) {
	tree := annotateAndBuild(t, `import { createSignal } from "@krate/runtime";
export default function App() {
	const [items, setItems] = createSignal(["a", "b"]);
	return <ul>{items().map(item => <li>{item}</li>)}</ul>;
}`)
	if !containsListSlot(tree.Root.Children) {
		t.Fatal("signal-backed map must keep a ListSlot")
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

func TestStaticFoldingQuickJSFallback(t *testing.T) {
	tree := buildStaticTreeWithJS(t, `export default function App() {
	const filled = new Array(3).fill(7);
	return <div>{filled.join("-")}-{String(Number.isInteger(4))}-{Array.from({length: 3}, (_, i) => i * 2).join(",")}</div>;
}`)
	got := renderStaticHTML(tree.Root.Children)
	if got != `<div>7-7-7-true-0,2,4</div>` {
		t.Fatalf("QuickJS fallback HTML = %q", got)
	}
}

func TestImpureExpressionsDoNotUseQuickJS(t *testing.T) {
	tree := annotateAndBuild(t, `export default function App() {
	return <div>{Math.random()}</div>;
}`)
	html := renderStaticHTML(tree.Root.Children)
	if strings.Contains(html, "0.") || strings.Contains(html, "Math.random") {
		t.Fatalf("random expression must stay dynamic, got %q", html)
	}
}
