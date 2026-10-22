package renderer

import (
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// buildTreeReact runs parse, React rewrite, annotate and IR build, returning
// the tree (for errors/warnings) plus the emitted HTML and hydration JS.
func buildTreeReact(t *testing.T, src string) (*irtree.ComponentTree, string, string) {
	t.Helper()
	prog := parseProg(t, src)
	bundler.RewriteReact(prog)
	ann := annotator.Annotate(prog, &config.Config{}, "page.tsx", src)
	tree := irtree.Build(prog, ann)
	result := NewEmitter().Emit(tree)
	EmitMeta(tree, result)
	return tree, result.HTML, GenerateNewHydrationJS(result)
}

func mustContain(t *testing.T, label, haystack string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(haystack, w) {
			t.Errorf("%s: missing %q in:\n%s", label, w, haystack)
		}
	}
}

func mustNotContain(t *testing.T, label, haystack string, bad ...string) {
	t.Helper()
	for _, w := range bad {
		if strings.Contains(haystack, w) {
			t.Errorf("%s: unexpected %q in:\n%s", label, w, haystack)
		}
	}
}

// diagsOf returns the diag.Diagnostic values among errs.
func diagsOf(errs []error) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, e := range errs {
		if d, ok := diag.FromError(e); ok {
			out = append(out, d)
		}
	}
	return out
}

// 3.1 Scope analysis.

func TestScopeShadowedSignalNames(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		html     []string
		js       []string
		jsAbsent []string
	}{
		{
			name: "map parameter shadows a signal",
			src: `import { createSignal } from "@krate/runtime";
export default function App() {
  const [count, setCount] = createSignal(0);
  const items = [1, 2, 3];
  return <div><p>{count()}</p><ul>{items.map(count => <li>{count}</li>)}</ul></div>;
}`,
			html: []string{"<li>1</li>", "<li>2</li>", "<li>3</li>"},
			js:   []string{"items.map((count)=>h('li',null,count))"},
		},
		{
			name: "signal of another component is not in scope",
			src: `import { createSignal } from "@krate/runtime";
function Child() {
  const count = 5;
  return <em>{count}</em>;
}
export default function App() {
  const [count, setCount] = createSignal(0);
  return <div><p>{count()}</p><Child /><button onClick={() => setCount(count() + 1)}>+</button></div>;
}`,
			html:     []string{"<em>5</em>"},
			jsAbsent: []string{"component Child"},
		},
		{
			name: "catch and block bindings shadow",
			src: `import { createSignal } from "@krate/runtime";
export default function App() {
  const [err, setErr] = createSignal("");
  const label = "ok";
  return <div>{[1].map(x => { const err = "local"; return <i>{err}</i>; })}<p>{err()}</p><b>{label}</b></div>;
}`,
			html: []string{"<i>local</i>", "<b>ok</b>"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree, html, js := buildTreeReact(t, tc.src)
			if len(tree.Errors) > 0 {
				t.Fatalf("unexpected errors: %v", tree.Errors)
			}
			mustContain(t, "html", html, tc.html...)
			mustContain(t, "js", js, tc.js...)
			mustNotContain(t, "js", js, tc.jsAbsent...)
		})
	}
}

// Helper references are found with a token scan: names inside string
// literals, slot-ID strings and property accesses are not references.
func TestReferencedHelpersTokenScan(t *testing.T) {
	src := `import { createSignal } from "@krate/runtime";
function Toggle() { return 1; }
function format(v) { return "#" + v; }
export default function App() {
  const [n, setN] = createSignal(0);
  return <div>
    <button onClick={() => alert("Toggle theme")}>a</button>
    <button onClick={() => window.Toggle && setN(format(n()))}>b</button>
    <p>{n()}</p>
  </div>;
}`
	_, _, js := buildTreeReact(t, src)
	mustContain(t, "js", js, "function format(v)")
	mustNotContain(t, "js", js, "function Toggle(")
}

// 3.2 Early returns.

func TestEarlyReturnLowering(t *testing.T) {
	t.Run("reactive guard", func(t *testing.T) {
		src := `import { useState } from "react";
function Spinner() { return <i>loading</i>; }
function Main() { return <main>ready</main>; }
export default function App() {
  const [l, setL] = useState(true);
  if (l) return <div><Spinner /><button onClick={() => setL(false)}>stop</button></div>;
  return <Main />;
}`
		tree, html, js := buildTreeReact(t, src)
		if len(tree.Errors) > 0 {
			t.Fatalf("unexpected errors: %v", tree.Errors)
		}
		cond, ok := tree.Root.Children[0].(*irtree.ConditionalSlot)
		if !ok {
			t.Fatalf("root child = %T, want *ConditionalSlot", tree.Root.Children[0])
		}
		if cond.TestJS != "l()" || !cond.InitialActive {
			t.Errorf("TestJS=%q InitialActive=%v, want l() / true", cond.TestJS, cond.InitialActive)
		}
		mustContain(t, "html", html, "<i>loading</i>", "<main>ready</main>")
		mustContain(t, "js", js, `kbindCond("`+string(cond.ID)+`",()=>(l()));`)
	})
	t.Run("static guard folds per instance", func(t *testing.T) {
		src := `function Badge(props) {
  if (!props.label) return null;
  return <b>{props.label}</b>;
}
export default function App() {
  return <div><Badge /><Badge label="x" /></div>;
}`
		tree, html, _ := buildTreeReact(t, src)
		if len(tree.Errors) > 0 {
			t.Fatalf("unexpected errors: %v", tree.Errors)
		}
		if got := strings.Count(html, "<b>"); got != 1 {
			t.Errorf("want exactly one <b>, got %d:\n%s", got, html)
		}
		mustContain(t, "html", html, "<div><b>x</b></div>")
	})
	t.Run("else-if chain and switch", func(t *testing.T) {
		src := `import { createSignal } from "@krate/runtime";
function Status(props) {
  switch (props.kind) {
    case "a": return <i>A</i>;
    case "b":
    case "c": return <i>BC</i>;
    default: return <i>D</i>;
  }
}
export default function App() {
  const [mode, setMode] = createSignal(1);
  if (mode() === 0) {
    return <p>zero</p>;
  } else if (mode() === 1) {
    return <div><p>one</p><Status kind="c" /><Status kind="z" /></div>;
  }
  return <p>many</p>;
}`
		tree, html, js := buildTreeReact(t, src)
		if len(tree.Errors) > 0 {
			t.Fatalf("unexpected errors: %v", tree.Errors)
		}
		mustContain(t, "html", html, "<p>zero</p>", "<p>one</p>", "<i>BC</i>", "<i>D</i>", "<p>many</p>")
		mustContain(t, "js", js, "()=>((mode() === 0)));", "()=>((mode() === 1)));")
	})
	t.Run("unlowerable branch is a positioned error", func(t *testing.T) {
		src := `import { createSignal } from "@krate/runtime";
export default function App() {
  const [l, setL] = createSignal(true);
  if (l()) {
    console.log("loading");
    return <i>loading</i>;
  }
  return <main>ready</main>;
}`
		tree, _, _ := buildTreeReact(t, src)
		ds := diagsOf(tree.Errors)
		if len(ds) != 1 {
			t.Fatalf("want 1 diagnostic, got %v", tree.Errors)
		}
		d := ds[0]
		// The parser gives the expression statement no position, so the
		// diagnostic points at the enclosing guard.
		if d.File != "page.tsx" || d.Line != 4 || d.Col <= 0 || d.Hint == "" || !strings.Contains(d.Source, "if (l())") {
			t.Errorf("diagnostic = %+v", d)
		}
		if !strings.Contains(d.Message, "component <App>") {
			t.Errorf("message should name the component: %q", d.Message)
		}
	})
}

// 3.3 Handlers.

func TestHandlerExpressions(t *testing.T) {
	src := `import { useState } from "react";
function makeHandler(id) { return () => console.log(id); }
export default function App(props) {
  const [flag, setFlag] = useState(false);
  const a = () => setFlag(true);
  const b = () => setFlag(false);
  return <div>
    <button onClick={flag ? a : b}>t</button>
    <button onClick={track}>i</button>
    <button onClick={makeHandler(3)}>m</button>
    <p>{flag}</p>
  </div>;
}`
	tree, html, js := buildTreeReact(t, src)
	if len(tree.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", tree.Errors)
	}
	if got := strings.Count(html, `<button data-k=`); got != 3 {
		t.Errorf("every button needs a handler binding, got %d:\n%s", got, html)
	}
	mustContain(t, "js", js,
		`function(e){var __h=((flag()?a:b));if(typeof __h==="function")return __h.call(this,e);}`,
		`function(e){if(typeof track==="function")return track.call(this,e);}`,
		`(function(){var __h=(makeHandler(3));return function(e){if(typeof __h==="function")return __h.call(this,e);};})()`,
		"function makeHandler(id)",
	)
}

// 3.4 Codegen.

func TestCodegenKeywordOperatorsAndDeclarations(t *testing.T) {
	src := `import { createSignal } from "@krate/runtime";
export default function App() {
  const [n, setN] = createSignal(0);
  let timer;
  const run = () => setN(1);
  return <div>
    <button onClick={() => void run()}>v</button>
    <button onClick={() => { timer = 1; delete window.x; setN(- -n()); }}>d</button>
    <button onClick={async () => { await run(); }}>a</button>
    <p>{n()}</p>
  </div>;
}`
	_, _, js := buildTreeReact(t, src)
	mustContain(t, "js", js, "var timer;", "()=>void run()", "delete window.x;", "setN(- -n())", "await run();")
	mustNotContain(t, "js", js, "voidrun", "deletewindow", "--n()")
}

func TestListItemHandlersSubstituteParam(t *testing.T) {
	src := `import { createSignal } from "@krate/runtime";
export default function App() {
  const [n, setN] = createSignal(0);
  function rm(id) { setN(id); }
  return <div>
    {[{ id: 1 }, { id: 2 }].map(i => <b onClick={() => rm(i.id)}>x{n()}</b>)}
    {[{ id: 7 }].map(i => <s onClick={() => [9].forEach(i => rm(i))}>y</s>)}
  </div>;
}`
	_, _, js := buildTreeReact(t, src)
	mustContain(t, "js", js, `,()=>rm(1));`, `,()=>rm(2));`, `,()=>[9].forEach((i)=>rm(i)));`)
	mustNotContain(t, "js", js, `,()=>rm(i.id));`)
}

func TestRecursiveComponents(t *testing.T) {
	t.Run("self recursion without props is an error", func(t *testing.T) {
		src := `export default function Tree() {
  return <div><Tree /></div>;
}`
		tree, _, _ := buildTreeReact(t, src)
		ds := diagsOf(tree.Errors)
		if len(ds) != 1 {
			t.Fatalf("want 1 diagnostic, got %v", tree.Errors)
		}
		if !strings.Contains(ds[0].Message, "(Tree > Tree)") || ds[0].Line != 2 || ds[0].Col <= 0 || !strings.Contains(ds[0].Source, "<Tree />") || ds[0].Hint == "" {
			t.Errorf("diagnostic = %+v", ds[0])
		}
	})
	t.Run("mutual recursion names the cycle", func(t *testing.T) {
		src := `function A(props) { return <section><B /></section>; }
function B(props) { return <div><A /></div>; }
export default function App() { return <main><A /></main>; }`
		tree, _, _ := buildTreeReact(t, src)
		ds := diagsOf(tree.Errors)
		if len(ds) == 0 || !strings.Contains(ds[0].Message, "A > B > A") {
			t.Fatalf("want cycle diagnostic, got %v", tree.Errors)
		}
	})
	t.Run("recursion over build-time data terminates", func(t *testing.T) {
		src := `function Node(props) {
  return <li>{props.n.name}{props.n.kids.length > 0 && <ul>{props.n.kids.map(k => <Node n={k} />)}</ul>}</li>;
}
export default function App() {
  return <ul><Node n={{ name: "a", kids: [{ name: "b", kids: [] }] }} /></ul>;
}`
		tree, html, _ := buildTreeReact(t, src)
		if len(tree.Errors) > 0 {
			t.Fatalf("unexpected errors: %v", tree.Errors)
		}
		mustContain(t, "html", html, "<ul><li>a<ul><li>b</li></ul></li></ul>")
	})
}

func TestCodegenIssueMessages(t *testing.T) {
	src := `export default function App() {
  class Local {}
  return <div>{String(new Local())}</div>;
}`
	tree, _, _ := buildTreeReact(t, src)
	ds := diagsOf(tree.Errors)
	if len(ds) == 0 {
		t.Fatalf("want a class declaration diagnostic, got %v", tree.Errors)
	}
	d := ds[0]
	if !strings.Contains(d.Message, "class declaration Local") || d.Line != 2 || d.Hint == "" {
		t.Errorf("diagnostic = %+v", d)
	}
	for _, r := range d.Message + d.Hint {
		if r > 0x7e {
			t.Errorf("diagnostic text must be ASCII: %q", d.Message)
			break
		}
	}
	if strings.Contains(d.Message, "Krate bug") {
		t.Errorf("user-facing construct should not be blamed on a compiler bug: %q", d.Message)
	}
}

func TestRenderObjectLiteralIsDiagnosed(t *testing.T) {
	src := `import { createSignal } from "@krate/runtime";
export default function App() {
  const [l] = createSignal(true);
  if (l()) return { a: 1 };
  return <p>x</p>;
}`
	tree, _, _ := buildTreeReact(t, src)
	ds := diagsOf(tree.Errors)
	if len(ds) == 0 || !strings.Contains(ds[0].Message, "object literal") || ds[0].Line <= 0 {
		t.Fatalf("want object-literal diagnostic, got %v", tree.Errors)
	}
}

// Determinism: the same tree built repeatedly (and under different
// GOMAXPROCS) yields byte-identical HTML and hydration JS. Child props
// registrations used to follow map iteration order.
func TestBuildOutputDeterministic(t *testing.T) {
	src := `import { createSignal } from "@krate/runtime";
function Card(props) {
  const [open, setOpen] = createSignal(false);
  return <div class={props.tone} title={props.title} data-a={props.a} data-b={props.b} onClick={() => setOpen(!open())}>{props.label}{open() ? "x" : "y"}</div>;
}
export default function App() {
  const [n, setN] = createSignal(0);
  return <section>
    <Card label="one" tone="red" title="t1" a="1" b="2" />
    <Card label="two" tone="blue" title="t2" a="3" b={String(n())} />
    <button onClick={() => setN(n() + 1)}>+</button>
  </section>;
}`
	prev := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(prev)
	var wantHTML, wantJS string
	for i, procs := range []int{1, 2, 8, 1, 4, 16, 1, 8} {
		runtime.GOMAXPROCS(procs)
		for j := 0; j < 4; j++ {
			_, html, js := buildTreeReact(t, src)
			if i == 0 && j == 0 {
				wantHTML, wantJS = html, js
				if !strings.Contains(js, `__krate_props[`) {
					t.Fatalf("fixture must register child props:\n%s", js)
				}
				continue
			}
			if html != wantHTML || js != wantJS {
				t.Fatalf("build %d/%d (GOMAXPROCS=%d) differs:\n--- first\n%s\n--- now\n%s", i, j, procs, wantJS, js)
			}
		}
	}
}

// 3.6 Typed warnings.

func TestWarningsArePositionedDiagnostics(t *testing.T) {
	t.Run("unresolved component", func(t *testing.T) {
		src := `export default function App() {
  return <div>
    <Missing />
  </div>;
}`
		tree, html, _ := buildTreeReact(t, src)
		if len(tree.Warnings) != 1 {
			t.Fatalf("want 1 warning, got %+v", tree.Warnings)
		}
		w := tree.Warnings[0]
		if !w.IsWarning() || w.File != "page.tsx" || w.Line != 3 || w.Col <= 0 || w.Source != "    <Missing />" || w.Hint == "" {
			t.Errorf("warning = %+v", w)
		}
		mustContain(t, "html", html, "<span></span>")
	})
	t.Run("class component names the unsupported member", func(t *testing.T) {
		src := `import { Component } from "react";
class Gate extends Component {
  shouldComponentUpdate() { return false; }
  render() { return <div>gate</div>; }
}
export default function App() {
  return <main><Gate /></main>;
}`
		tree, _, _ := buildTreeReact(t, src)
		if len(tree.Warnings) != 1 {
			t.Fatalf("want 1 warning, got %+v", tree.Warnings)
		}
		w := tree.Warnings[0]
		if !w.IsWarning() || w.Line != 3 || !strings.Contains(w.Message, "shouldComponentUpdate is not supported") ||
			!strings.Contains(w.Message, "rendered at page.tsx:7:") || !strings.Contains(w.Source, "shouldComponentUpdate()") || w.Hint == "" {
			t.Errorf("warning = %+v", w)
		}
	})
}
