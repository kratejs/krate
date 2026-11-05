package renderer

import (
	"strings"
	"testing"
)

func TestHydrationScopesAreIsolated(t *testing.T) {
	src := `import { useState } from "react";
function A() { const [n, setN] = useState(0); return <button onClick={() => setN(n + 1)}>{n}</button>; }
function B() { const [m, setM] = useState(0); return <button onClick={() => setM(m + 1)}>{m}</button>; }
export default function Page() { return <main><A /><B /></main>; }`
	_, js := fullPipeline(t, src)
	scopes := strings.Count(js, "// component ")
	if scopes < 2 {
		t.Fatalf("expected two component scopes, got %d:\n%s", scopes, js)
	}
	if got := strings.Count(js, "try{"); got < scopes {
		t.Errorf("each of %d scopes should run in try{}, found %d:\n%s", scopes, got, js)
	}
	if got := strings.Count(js, "}catch(e){__krateScopeErr(e);}"); got < scopes {
		t.Errorf("each of %d scopes should catch into __krateScopeErr, found %d:\n%s", scopes, got, js)
	}
	if !strings.Contains(HydrationBootstrapJS, "window.__krateScopeErr=") {
		t.Error("bootstrap does not define __krateScopeErr")
	}
}

func TestHydrationSuspenseChildrenAreBound(t *testing.T) {
	src := `import { useState, Suspense } from "react";
function A() { return <b>A</b>; }
function B() { return <b>B</b>; }
export default function Page() {
  const [n, setN] = useState(0);
  const [show, setShow] = useState(true);
  return (<main>
    <button onClick={() => { setN(n + 1); setShow(!show); }}>x</button>
    <Suspense fallback={<p>loading</p>}><p>count {n}</p></Suspense>
    <Suspense fallback={<p>loading</p>}>{show ? <A /> : <B />}</Suspense>
  </main>);
}`
	_, js := fullPipeline(t, src)
	if !strings.Contains(js, "kbindText(") && !strings.Contains(js, "kbindContent(") {
		t.Errorf("text inside <Suspense> is not bound:\n%s", js)
	}
	if !strings.Contains(js, "kbindCond(") {
		t.Errorf("conditional inside <Suspense> is not bound:\n%s", js)
	}
}

func TestDestructuredLocalsWithoutBuildTimeValueAreReplayed(t *testing.T) {
	src := `import { useState } from "react";
const cfg = { label: "L", fmt: (s) => s.toUpperCase() };
function getInfo() { return { title: "T", count: 3 }; }
function Child() {
  const { label, fmt } = cfg;
  const [n, setN] = useState(0);
  return <div><span>{label}</span><span>{fmt("y")}</span><button onClick={() => setN(n + 1)}>{n}</button></div>;
}
export default function Page() {
  const { title, count } = getInfo();
  const { fmt: f } = cfg;
  const [n, setN] = useState(0);
  return <main><Child /><h1>{title}</h1><p>{count + n}</p><i>{f("z")}</i><button onClick={() => setN(n + 1)}>b</button></main>;
}`
	res, js := fullPipeline(t, src)
	for _, want := range []string{"{title", "count", "fmt", "}=getInfo()", "fmt:"} {
		if !strings.Contains(js, want) {
			t.Errorf("hydration JS lacks the replayed destructuring (%q):\n%s", want, js)
		}
	}
	// A name must not be initialized with the whole right-hand side.
	for _, bad := range []string{"var title=", "var count=", "var fmt=cfg", "var f=cfg"} {
		if strings.Contains(js, bad) {
			t.Errorf("destructured name initialized from the whole initializer (%q):\n%s", bad, js)
		}
	}
	if strings.Contains(res.HTML, "[object Object]") {
		t.Errorf("SSR baked an object into the text:\n%s", res.HTML)
	}
}

func TestModuleDestructureIsDeclaredAfterItsSource(t *testing.T) {
	src := `const cfg = { label: "L", fmt: (s) => s.toUpperCase() };
const { label, fmt } = cfg;
export default function Page() { return <main><p>{label}</p><p>{fmt("y")}</p></main>; }`
	_, js := fullPipeline(t, src)
	cfg := strings.Index(js, "var cfg=")
	tmp := strings.Index(js, "var __kmod_")
	member := strings.Index(js, "var fmt=__kmod_")
	if cfg < 0 || tmp < 0 || member < 0 {
		t.Fatalf("missing declarations (cfg %d, temp %d, member %d):\n%s", cfg, tmp, member, js)
	}
	if !(cfg < tmp && tmp < member) {
		t.Errorf("declarations out of order (cfg %d, temp %d, member %d):\n%s", cfg, tmp, member, js)
	}
}

func TestFunctionPropReadInSlotIsHydrated(t *testing.T) {
	src := `function A({ fmt }) { return <p>{fmt("x")}</p>; }
function List({ render, items }) { return <ul>{items.map((i) => <li key={i}>{render(i)}</li>)}</ul>; }
export default function Page() {
  return <main><A fmt={(s) => s.toUpperCase()} /><List items={["a", "b"]} render={(s) => <u>{s}</u>} /></main>;
}`
	_, js := fullPipeline(t, src)
	if !strings.Contains(js, ".fmt(") || !strings.Contains(js, ".render(") {
		t.Errorf("slots that call a function prop have no client binding:\n%s", js)
	}
}
