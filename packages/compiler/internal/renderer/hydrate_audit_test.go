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
