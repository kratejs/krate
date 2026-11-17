package renderer

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// regFor returns the props registration of the child instance whose id ends
// with suffix, e.g. `{onToggle:pick}`.
func regFor(t *testing.T, js, suffix string) string {
	t.Helper()
	re := regexp.MustCompile(`__krate_props\["([^"]*)"\]=(\{[^\n]*\});`)
	for _, m := range re.FindAllStringSubmatch(js, -1) {
		if strings.HasSuffix(m[1], suffix) {
			return m[2]
		}
	}
	t.Fatalf("no registration for %s in:\n%s", suffix, js)
	return ""
}

func TestPropsRegKeepsOnlyClientReads(t *testing.T) {
	src := `import { createSignal } from "@krate/runtime";
function Card(props) {
  const [open, setOpen] = createSignal(false);
  const items = props.items;
  return <div class={props.tone} title={props.title} data-b={props.b} onClick={() => { setOpen(!open()); props.onToggle && props.onToggle(); }}>{props.label}{items.map((i) => <i>{i}</i>)}{open() ? "x" : "y"}</div>;
}
export default function App() {
  const [n, setN] = createSignal(0);
  const pick = () => setN(n() + 1);
  return <section>
    <Card label="one" tone="red" title="t1" b="2" items={["a", "b"]} onToggle={pick} />
    <Card label="two" tone="blue" title="t2" b={String(n())} items={["c"]} />
  </section>;
}`
	_, html, js := buildTreeReact(t, src)
	mustContain(t, "html", html, "one", "t1", ">a<")
	// Static props, and a local that only feeds the server render, stay out.
	if got := regFor(t, js, "Card_c0"); got != "{onToggle:pick}" {
		t.Errorf("Card_c0 registration = %s, want {onToggle:pick}", got)
	}
	// The signal-derived prop is bound on the client and stays a getter.
	if got := regFor(t, js, "Card_c1"); got != "{get b(){return String(n())}}" {
		t.Errorf("Card_c1 registration = %s", got)
	}
}

func TestPropsRegEscapeKeepsAll(t *testing.T) {
	cases := map[string]string{
		"passed whole":  `onClick={() => log(props)}`,
		"dynamic key":   `onClick={() => log(props[key()])}`,
		"aliased":       `onClick={() => { const p = props; log(p.x); }}`,
		"spread object": `onClick={() => log({ ...props })}`,
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			src := `import { createSignal } from "@krate/runtime";
function log(v) { console.log(v); }
function Child(props) {
  const [key, setKey] = createSignal("tone");
  return <button ` + handler + `>{props.label}</button>;
}
export default function App() {
  return <Child label="one" tone="red" x={1} />;
}`
			_, _, js := buildTreeReact(t, src)
			got := regFor(t, js, "Child_c0")
			for _, want := range []string{"label:", "tone:", "x:"} {
				if !strings.Contains(got, want) {
					t.Errorf("registration %s lost %s", got, want)
				}
			}
		})
	}
}

func TestPropsRegForwardedToChild(t *testing.T) {
	// The middle component forwards a live prop to a grandchild that binds it
	// on the client, and a static one the grandchild only renders.
	src := `import { createSignal } from "@krate/runtime";
function Leaf(props) {
  return <span data-n={props.n} title={props.label}>{props.label}</span>;
}
function Middle(props) {
  const [o, setO] = createSignal(false);
  return <div onClick={() => setO(!o())}><Leaf n={props.n} label={props.label} /></div>;
}
export default function App() {
  const [n, setN] = createSignal(0);
  return <section>
    <Middle n={n()} label="static" extra="unused" />
    <button onClick={() => setN(n() + 1)}>+</button>
  </section>;
}`
	_, html, js := buildTreeReact(t, src)
	mustContain(t, "html", html, "static")
	leaf := regFor(t, js, "Leaf_c0")
	if !strings.Contains(leaf, "get n(){return props.n}") || strings.Contains(leaf, "label") {
		t.Errorf("Leaf registration = %s, want only the forwarded live n", leaf)
	}
	// Middle reads n only to forward it; that forward makes it a read.
	middle := regFor(t, js, "Middle_c0")
	if !strings.Contains(middle, "get n(){return n()}") || strings.Contains(middle, "label") || strings.Contains(middle, "extra") {
		t.Errorf("Middle registration = %s, want only n", middle)
	}
}

func TestPropsRegRemountedBranch(t *testing.T) {
	// A component in a conditional branch re-runs its scope on every mount;
	// the props that scope reads must stay registered.
	src := `import { createSignal, createEffect } from "@krate/runtime";
function Panel(props) {
  createEffect(() => console.log(props.name));
  return <p title={props.title}>{props.title}</p>;
}
export default function App() {
  const [open, setOpen] = createSignal(true);
  return <section>
    {open() && <Panel name="panel" title="t" />}
    <button onClick={() => setOpen(!open())}>toggle</button>
  </section>;
}`
	_, _, js := buildTreeReact(t, src)
	got := regFor(t, js, "Panel_c0")
	if !strings.Contains(got, "name:") || strings.Contains(got, "title") {
		t.Errorf("Panel registration = %s, want only name", got)
	}
	mustContain(t, "js", js, "props.name")
}

func TestPropsRegRendererKeepsAll(t *testing.T) {
	// A renderer boundary hands its whole props object to the runtime.
	src := `import { createSignal, onMount } from "@krate/runtime";
@renderer
function Canvas(props) {
  let el;
  onMount(() => console.log(el));
  return <div class="canvas"><canvas ref={el} /></div>;
}
export default function Scene() {
  const [n, setN] = createSignal(1);
  return <section><Canvas frameloop="demand" dpr={2}><mesh /></Canvas><button onClick={() => setN(n() + 1)}>+</button></section>;
}`
	_, _, js := buildTreeDecorated(t, src)
	mustContain(t, "js", js, "frameloop:'demand'", "dpr:2")
}

func TestAnalyzePropsReads(t *testing.T) {
	cases := []struct {
		code   string
		names  string
		all    bool
		unused string
	}{
		{code: `kbindHandler("1","onClick",()=>props.onPick(props.label));`, names: "label,onPick"},
		{code: `x(props?.a, props["b"], props?.["c"]);`, names: "a,b,c"},
		{code: `var items=props.items;`, unused: "items"},
		{code: "var items=props.items;\nfoo(items);", names: "items"},
		{code: `var t=props.tags || [];`, names: "tags"},
		{code: `f(props);`, all: true},
		{code: `var p={...props};`, all: true},
		{code: `props[k]();`, all: true},
		{code: `function Row(props){return props.x}`, names: "x", all: true},
		{code: `eval("props.x");`, all: true},
		{code: `var s="props.x"; o.props.y; // props.z`},
	}
	for _, c := range cases {
		r, foreign := analyzePropsReads(c.code)
		if foreign {
			t.Errorf("%s: unexpected foreign registry use", c.code)
		}
		var names, unused []string
		for n := range r.names {
			names = append(names, n)
		}
		for l := range r.unused {
			unused = append(unused, l)
		}
		sort.Strings(names)
		sort.Strings(unused)
		if strings.Join(names, ",") != c.names || r.all != c.all || strings.Join(unused, ",") != c.unused {
			t.Errorf("%s: names=%v all=%v unused=%v, want %q %v %q", c.code, names, r.all, unused, c.names, c.all, c.unused)
		}
	}
	if _, foreign := analyzePropsReads(`var x=__krate_props["a.B_c0"];`); !foreign {
		t.Error("a direct registry read must disable filtering")
	}
	if _, foreign := analyzePropsReads(`__krate_props["a.B_c0#ctx:Theme"]=function(){return t()};`); foreign {
		t.Error("a context overlay registration is modelled")
	}
}

func TestPropsDeclBinding(t *testing.T) {
	id, binding, ok := parsePropsDecl(`var props=__krate_props["a.B_c0"];var { label, onPick: pick, size = "m,n" }=props`)
	if !ok || id != "a.B_c0" || !strings.HasPrefix(binding, "{") {
		t.Fatalf("parsePropsDecl = %q %q %v", id, binding, ok)
	}
	r := bindingReads(binding)
	var names []string
	for n := range r.names {
		names = append(names, n)
	}
	sort.Strings(names)
	if r.all || strings.Join(names, ",") != "label,onPick,size" {
		t.Errorf("destructured keys = %v all=%v", names, r.all)
	}
	for _, b := range []string{`{ a, ...rest }`, `p`, `[a, b]`, `{ [k]: v }`} {
		if !bindingReads(b).all {
			t.Errorf("%s must read every prop", b)
		}
	}
	if _, _, ok := parsePropsDecl(`var props=__krate_props["x"];foo()`); ok {
		t.Error("an unknown suffix must not parse")
	}
}
