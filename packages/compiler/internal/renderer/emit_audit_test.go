package renderer

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

func TestCallWithArgsOverSignalKeepsBinding(t *testing.T) {
	for name, expr := range map[string]string{
		"fn":      `label(x())`,
		"twoargs": `label(x(), 1)`,
		"plus":    `label(x() + 0)`,
	} {
		src := `
import { createSignal } from "@krate/runtime";
function label(n, m) { return "n=" + n; }
export default function App() {
  const [x, setX] = createSignal(1);
  return <p onClick={() => setX(x() + 1)}>{` + expr + `}</p>;
}
`
		res, hjs := fullPipeline(t, src)
		if !strings.Contains(hjs, "label(") || !strings.Contains(hjs, "kbind") {
			t.Errorf("%s: no binding emitted:\nHTML %s\nJS %s", name, res.HTML, hjs)
		}
	}
}

func TestNonFoldableValuesOnComponentWithoutSignals(t *testing.T) {
	src := `
export default function App() {
  return <p>{new Date(0).toISOString()} {"abc".localeCompare("abd")} {(255).toString(16)}</p>;
}
`
	res, hjs := fullPipeline(t, src)
	if !strings.Contains(res.HTML, "ff") {
		t.Errorf("(255).toString(16) not folded at build time:\n%s", res.HTML)
	}
	for _, want := range []string{"new Date(0).toISOString()", "localeCompare"} {
		if !strings.Contains(hjs, want) {
			t.Errorf("non-foldable expression %q has no hydration binding:\n%s", want, hjs)
		}
	}
}

func TestChainedMemosKeepDeclarationOrder(t *testing.T) {
	src := `
import { createSignal, createMemo } from "@krate/runtime";
export default function App() {
  const [n, setN] = createSignal(1);
  const d = createMemo(() => n() * 2);
  const q = createMemo(() => d() * 2);
  return <p onClick={() => setN(n() + 1)}>{q()}</p>;
}
`
	_, hjs := fullPipeline(t, src)
	in := strings.Index(hjs, "createSignal(")
	id := strings.Index(hjs, "d=createMemo")
	iq := strings.Index(hjs, "q=createMemo")
	if in < 0 || id < 0 || iq < 0 || !(in < id && id < iq) {
		t.Errorf("bad order (n=%d d=%d q=%d):\n%s", in, id, iq, hjs)
	}
}

func TestChainedMixedFourLevel(t *testing.T) {
	src := `
import { createSignal, createMemo } from "@krate/runtime";
import { useMemo } from "react";
export default function App() {
  const [n, setN] = createSignal(1);
  const a = createMemo(() => n() + 1);
  const b = () => a() * 3;
  const c = createMemo(() => b() + 1);
  const d = createMemo(() => c() + a());
  const e = useMemo(() => d() + 1, [d]);
  return <p onClick={() => setN(n() + 1)}>{e}</p>;
}
`
	_, hjs := fullPipeline(t, src)
	t.Log(hjs)
	last := -1
	for _, k := range []string{"createSignal(", "a=createMemo", "b=", "c=createMemo", "d=createMemo", "e="} {
		i := strings.Index(hjs, k)
		if i < 0 || i < last {
			t.Fatalf("order broken at %q:\n%s", k, hjs)
		}
		last = i
	}
}

func TestForLoopClosureBindsLoopVar(t *testing.T) {
	src := `
import { createSignal } from "@krate/runtime";
export default function App() {
  const [v, setV] = createSignal(0);
  const arr = [];
  for (let i = 0; i < 3; i++) arr.push(<button onClick={() => setV(i)}>b</button>);
  return <div>{arr}<span>{v()}</span></div>;
}
`
	_, hjs := fullPipeline(t, src)
	t.Log(hjs)
	for _, want := range []string{"((i)=>(()=>setV(i)))(0)", "((i)=>(()=>setV(i)))(1)", "((i)=>(()=>setV(i)))(2)"} {
		if !strings.Contains(hjs, want) {
			t.Errorf("handler does not bind its loop value %q:\n%s", want, hjs)
		}
	}
}

func TestSpreadChildrenEmitsValidJS(t *testing.T) {
	src := `
import { createSignal } from "@krate/runtime";
export default function App() {
  const [v, setV] = createSignal(0);
  const kids = [<i>a</i>, <b>b</b>];
  return <div onClick={() => setV(v() + 1)}>{...kids}<span>{v()}</span></div>;
}
`
	res, hjs := fullPipeline(t, src)
	t.Log(res.HTML)
	if strings.Contains(hjs, "()=>...") {
		t.Errorf("spread emitted as arrow body:\n%s", hjs)
	}
}

func TestHydrationErrorsAreLoggedAndRecorded(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	start := strings.Index(HydrationBootstrapJS, "window.__krateErrs=[];")
	end := strings.Index(HydrationBootstrapJS, "window.__krateFx=")
	end += strings.Index(HydrationBootstrapJS[end:], "\n")
	js := "var window=globalThis;var logged=[];console.error=function(e){logged.push(String(e&&e.message||e));};\n" +
		HydrationBootstrapJS[start:end] + "\n" +
		"__safe(function(){throw new Error('boom')});\n" +
		"__krateFx({isConnected:false},function(){throw new Error('gone')})();\n" +
		"console.log(JSON.stringify([logged,window.__krateErrs.length]));"
	out, err := exec.Command("node", "-e", js).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != `[["boom","gone"],2]` {
		t.Errorf("got %s", got)
	}
}

func TestRuntimeFragmentRendersLikeReactFragment(t *testing.T) {
	for _, imp := range []string{`import { Fragment } from "@krate/runtime";`, `import { Fragment } from "react";`} {
		src := imp + `
const rows = [{id: 1, n: "a"}, {id: 2, n: "b"}];
export default function App() {
  return <ul>{rows.map((r) => <Fragment key={r.id}><li>{r.n}</li><li>x</li></Fragment>)}</ul>;
}
`
		res, _ := fullPipelineWithReact(t, src)
		if strings.Contains(res.HTML, "<span") || !strings.Contains(res.HTML, "<li>a</li><li>x</li><li>b</li><li>x</li>") {
			t.Errorf("%s: bad HTML %s", imp, res.HTML)
		}
	}
}

func TestDecoratedClassComponentWarns(t *testing.T) {
	raw := `import React from "react";
function track(c: any) { return c; }
@track
class Box extends React.Component<{ n: number }> {
  render() { return <p>box {this.props.n}</p>; }
}
export default function App() {
  return <div><Box n={2} /></div>;
}
`
	stripped, err := tsstrip.Strip("test.tsx", raw, tsstrip.Options{})
	if err != nil {
		t.Fatal(err)
	}
	prog := parseProg(t, stripped.Code)
	bundler.RewriteReact(prog)
	ann := annotator.Annotate(prog, &config.Config{}, "test.tsx", raw)
	tree := irtree.Build(prog, ann)
	found := false
	for _, w := range tree.Warnings {
		if strings.Contains(w.Message, "decorated class <Box>") && strings.Contains(w.Hint, "decorator") {
			found = true
		}
		if strings.Contains(w.Message, "could not be resolved") {
			t.Errorf("generic message instead of the decorator diagnostic: %s", w.Message)
		}
	}
	if !found {
		t.Errorf("no decorator warning: %+v", tree.Warnings)
	}
}
