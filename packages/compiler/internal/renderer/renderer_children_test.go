package renderer

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// buildTreeDecorated is buildTreeReact for source with Krate decorators.
func buildTreeDecorated(t *testing.T, src string) (*irtree.ComponentTree, string, string) {
	t.Helper()
	prog, errs := tsstrip.Parse("page.tsx", src, tsstrip.Options{})
	if len(errs) > 0 {
		t.Fatalf("parse: %v", errs)
	}
	bundler.RewriteReact(prog)
	ann := annotator.Annotate(prog, &config.Config{}, "page.tsx", src)
	tree := irtree.Build(prog, ann)
	result := NewEmitter().Emit(tree)
	EmitMeta(tree, result)
	return tree, result.HTML, GenerateNewHydrationJS(result)
}

const rendererScene = `import { createSignal, onMount } from "@krate/runtime";
@renderer
function Canvas(props) {
  let el;
  onMount(() => console.log(props.children, el));
  return <div class="canvas"><canvas ref={el} /></div>;
}
function Box(props) { return <mesh position-x={props.x} onUpdate={(s) => s}><Leaf /></mesh>; }
function Leaf() { return <sphereGeometry attach={(p) => p} />; }
export default function Scene() {
  const [n, setN] = createSignal(1);
  const [items, setItems] = createSignal([1, 2]);
  return <section>
    <Canvas frameloop="demand">
      <ambientLight intensity={0.5} />
      <mesh position={[n(), 0, 0]} onClick={() => setN(n() + 1)} onPointerOver={() => 1} onLostPointerCapture={() => 2} attach={(parent, self) => () => {}} ref={(m) => m}>
        <boxGeometry args={[1, 1, 1]} />
        <meshStandardMaterial color={n() > 2 ? "hotpink" : "orange"} />
      </mesh>
      {n() > 3 && <pointLight position={[2, 2, 2]} />}
      {items().map((i) => <Box key={i} x={i} />)}
      <Box x={9} />
    </Canvas>
    <button onClick={() => setItems([...items(), 3])}>add</button>
  </section>;
}`

func TestRendererChildrenSkipSSR(t *testing.T) {
	tree, html, _ := buildTreeDecorated(t, rendererScene)
	mustContain(t, "html", html, `<div class="canvas">`, "<canvas")
	mustNotContain(t, "html", html, "<mesh", "ambientLight", "boxGeometry", "pointLight", "sphereGeometry")
	if len(tree.Warnings) != 0 {
		t.Errorf("renderer children must produce no diagnostics, got %v", tree.Warnings)
	}
	if len(tree.Errors) != 0 {
		t.Errorf("unexpected errors: %v", tree.Errors)
	}
}

func TestRendererChildrenThunk(t *testing.T) {
	_, _, js := buildTreeDecorated(t, rendererScene)
	mustContain(t, "js", js,
		`frameloop:'demand',children:()=>[h('ambientLight',{intensity:0.5}),h('mesh',{position:()=>[n(),0,0],onClick:()=>setN((n() + 1)),onPointerOver:()=>1,onLostPointerCapture:()=>2,attach:()=>((parent, self)=>()=>{}),ref:(m)=>m},h('boxGeometry',{args:()=>[1,1,1]}),h('meshStandardMaterial',{color:()=>((n() > 2)?'hotpink':'orange')}))`,
		`()=>((n() > 3) && h('pointLight',{position:()=>[2,2,2]}))`,
		`krateList(()=>(items()),(i)=>i,(i)=>h(Box, {get x() {`,
		`h(Box, {x:9})]}`,
		`var props=__krate_props[`,
	)
	// Box and Leaf are declared in the caller's scope in renderer form:
	// events stay, other function props become thunks.
	mustContain(t, "js", js, "function Box(", "onUpdate:(s)=>s", "function Leaf(", "attach:()=>((p)=>p)")
	// Nothing in the children is bound as DOM.
	mustNotContain(t, "js", js, "kbindAttr(", "kbindCond(", "kbindList(")
	if got := strings.Count(js, "setN((n() + 1))"); got != 1 {
		t.Errorf("the mesh click handler must only live in the children thunk, found %d copies", got)
	}
}

func TestRendererDirectiveForms(t *testing.T) {
	for _, directive := range []string{`"use renderer";`, `"@renderer";`} {
		t.Run(directive, func(t *testing.T) {
			src := `import { createSignal } from "@krate/runtime";
function Canvas(props) { ` + directive + ` return <canvas />; }
export default function Scene() {
  const [n, setN] = createSignal(1);
  return <main><Canvas><mesh scale={n()} /></Canvas><button onClick={() => setN(2)}>x</button></main>;
}`
			_, html, js := buildTreeReact(t, src)
			mustNotContain(t, "html", html, "<mesh")
			mustContain(t, "js", js, `children:()=>[h('mesh',{scale:()=>n()})]`)
		})
	}
}

// Without call-site children the registration has no children key, and it is
// made even when the component reads no props in hydration code.
func TestRendererNoChildren(t *testing.T) {
	src := `@renderer
const Canvas = (props) => <canvas />;
export default function Scene() { return <main><Canvas mode="x" /></main>; }`
	_, _, js := buildTreeDecorated(t, src)
	mustContain(t, "js", js, `={mode:'x'}`)
	mustNotContain(t, "js", js, "children:")
}

// A renderer component rendered by an h() render function (a list row) takes
// its children as a thunk, never as h() arguments.
func TestRendererInsideHForm(t *testing.T) {
	src := `import { createSignal } from "@krate/runtime";
@renderer
export default function Canvas(props) { return <canvas />; }
function Row(props) {
  return <li>{props.show && <Canvas frameloop="always"><mesh position-x={props.id} attach={(a) => a} /></Canvas>}</li>;
}
export function Page() {
  const [items, setItems] = createSignal([1, 2]);
  return <ul onClick={() => setItems([3])}>{items().map((i) => <Row key={i} id={i} show={i > 1} />)}</ul>;
}`
	prog, errs := tsstrip.Parse("page.tsx", src, tsstrip.Options{})
	if len(errs) > 0 {
		t.Fatalf("parse: %v", errs)
	}
	ann := annotator.Annotate(prog, &config.Config{}, "page.tsx", src)
	ann.EntryPoint = "Page"
	tree := irtree.Build(prog, ann)
	result := NewEmitter().Emit(tree)
	EmitMeta(tree, result)
	js := GenerateNewHydrationJS(result)
	want := `h(Canvas, {frameloop:'always',children:()=>[h('mesh',{'position-x':()=>props.id,attach:()=>((a)=>a)})]})`
	if !strings.Contains(js, want) {
		t.Fatalf("missing %q in:\n%s", want, js)
	}
}

// <Suspense> in renderer children, and in components compiled for them, is
// the renderer's own boundary: an intrinsic 'krate-suspense' descriptor, not
// the DOM Suspense runtime.
func TestRendererSuspense(t *testing.T) {
	src := `import { createSignal, Suspense } from "@krate/runtime";
@renderer
function Canvas(props) { return <canvas />; }
function Model() { return <Suspense fallback={null}><mesh name="model" /></Suspense>; }
export default function Scene() {
  const [n, setN] = createSignal(1);
  return <main>
    <Canvas>
      <Suspense fallback={<mesh name="loading" scale={n()} />}><Model /></Suspense>
    </Canvas>
    <button onClick={() => setN(2)}>x</button>
  </main>;
}`
	_, html, js := buildTreeDecorated(t, src)
	mustContain(t, "js", js,
		`children:()=>[h('krate-suspense',{fallback:h('mesh',{name:'loading',scale:()=>n()})},h(Model, null))]`,
		`h('krate-suspense',{fallback:null},h('mesh',{name:'model'}))`,
	)
	mustNotContain(t, "js", js, "h(Suspense", "__krateBoundary")
	mustNotContain(t, "html", html, "krate-suspense", "<mesh", "<!--$")
}

// Providers around a renderer component reach its children through the live
// provider getters (krateContextRows), and providers written inside renderer
// children are component calls with a live value getter.
func TestRendererContextProviders(t *testing.T) {
	src := `import { createContext, useContext, useState } from "react";
@renderer
function Canvas(props) { return <canvas />; }
const Theme = createContext("default");
function Inner() { const c = useContext(Theme); return <group name={"ctx:" + c} />; }
function Tinted(props) { return <Theme.Provider value={props.tone}><Inner /></Theme.Provider>; }
export default function Page() {
  const [c, setC] = useState("red");
  const [d, setD] = useState("green");
  return <Theme.Provider value={c}>
    <button onClick={() => { setC("blue"); setD("x"); }}>x</button>
    <Canvas>
      <Inner />
      <Theme.Provider value={d}><Inner /></Theme.Provider>
      <Theme value={d + "!"}><Inner /></Theme>
      <Tinted tone={d} />
    </Canvas>
  </Theme.Provider>;
}`
	_, _, js := buildTreeDecorated(t, src)
	mustContain(t, "js", js,
		`children:krateContextRows([[Theme,()=>(c())]],()=>[h(Inner, null),h(Theme.Provider, {get value() {`,
		`return d();`,
		`h(Theme, {get value() {`,
		`return (d() + '!');`,
		`function Tinted(`,
	)
	if !strings.Contains(js, "h(Theme.Provider, {get value() {\n  return props.tone;") {
		t.Errorf("a provider in a renderer-mode component must keep a live value getter:\n%s", js)
	}
}

func TestRendererVariableRef(t *testing.T) {
	_, _, js := buildTreeDecorated(t, `import { createSignal } from "@krate/runtime";
@renderer
function Canvas(props) { return <canvas />; }
function Spinner() {
  let mesh;
  const box = { current: null };
  return <group ref={box}><mesh ref={mesh} name="m" /><mesh ref={(m) => m} /></group>;
}
export default function Scene() {
  return <Canvas><Spinner /></Canvas>;
}`)
	// A variable ref becomes a callback that assigns it unless it already holds
	// a callback or a {current} object; an arrow ref is passed as written.
	mustContain(t, "js", js, "(m)=>m", "__r", "'current' in __r", "mesh = __v", ")(mesh)", ")(box)")
	if strings.Contains(js, "ref:mesh") {
		t.Errorf("variable ref passed through as a value:\n%s", js)
	}
}

// A provider whose value is a build-time constant reaches the renderer
// children too: their components read the context in the browser, so it is
// passed like a dynamic provider.
func TestRendererStaticContextProvider(t *testing.T) {
	src := `import { createContext, useContext } from "react";
@renderer
function Stage(props) { return <div />; }
const Theme = createContext("plain");
function Themed() { const c = useContext(Theme); return <Text text={"theme:" + c} />; }
const Text = "Text";
export default function Page() {
  return <Theme.Provider value="dark"><Stage><Themed /></Stage></Theme.Provider>;
}`
	_, _, js := buildTreeDecorated(t, src)
	mustContain(t, "js", js, `children:krateContextRows([[Theme,()=>('dark')]],()=>[h(Themed, null)]`)
}

// Capitalized renderer elements (react-konva's <Rect>, constants naming the
// node type) and the renderer component itself take Krate variable refs.
func TestRendererVariableRefOnComponents(t *testing.T) {
	_, _, js := buildTreeDecorated(t, `import { onMount } from "@krate/runtime";
@renderer
function Stage(props) { return <div />; }
const Rect = "Rect";
export default function Page() {
  let stage;
  let rect;
  const obj = { current: null };
  onMount(() => console.log(stage, rect, obj));
  return <Stage ref={stage}><Rect ref={rect} x={1} /><Rect ref={obj} /><Rect ref={(r) => r} /></Stage>;
}`)
	mustContain(t, "js", js, "stage = __v", ")(stage)", "rect = __v", ")(rect)", ")(obj)", "(r)=>r")
	for _, raw := range []string{"ref:stage", "return rect;"} {
		if strings.Contains(js, raw) {
			t.Errorf("variable ref passed through as a value (%s):\n%s", raw, js)
		}
	}
}

// A scene component's pure locals that later statements still read stay where
// they were declared: moving `const get = store.getState` above
// `const store = useStore()` threw "Cannot access before initialization".
func TestRendererComponentLocalsKeepOrder(t *testing.T) {
	src := `import { onMount } from "@krate/runtime";
import { useStore } from "@krate/three/engine";
@renderer
function Canvas(props) {
  let el;
  onMount(() => console.log(props.children, el));
  return <div><canvas ref={el} /></div>;
}
function Rig(props) {
  const store = useStore();
  const get = store.getState;
  const size = get().size;
  onMount(() => get().invalidate());
  return <group name={"rig" + props.n} scale={size.width} />;
}
export default function Scene() {
  return <Canvas><Rig n={1} /></Canvas>;
}`
	_, _, js := buildTreeDecorated(t, src)
	store := strings.Index(js, "store=useStore()")
	get := strings.Index(js, "get=store.getState")
	if store < 0 || get < 0 {
		t.Fatalf("Rig locals missing from the reactive form:\n%s", js)
	}
	if get < store {
		t.Fatalf("const get was moved above const store:\n%s", js)
	}
}
