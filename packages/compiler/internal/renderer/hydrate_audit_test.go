package renderer

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
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

func TestPortalInsideConditionalIsLowered(t *testing.T) {
	src := `import { useState } from "react";
import { createPortal } from "react-dom";
export default function Page() {
  const [open, setOpen] = useState(false);
  return <main><button onClick={() => setOpen(!open)}>t</button>
    {open && createPortal(<div id="modal">hello</div>, document.body)}
    {open ? createPortal(<i id="m2">two</i>, document.body) : null}
  </main>;
}`
	res, js := fullPipeline(t, src)
	if strings.Contains(js, "createPortal(") {
		t.Errorf("createPortal reached the hydration script:\n%s", js)
	}
	if got := strings.Count(res.HTML, "data-krate-portal="); got != 2 {
		t.Errorf("expected two portal anchors, got %d:\n%s", got, res.HTML)
	}
	if !strings.Contains(js, "kbindCond(") {
		t.Errorf("portal branches are not toggled by a conditional binding:\n%s", js)
	}
}

func TestInactiveBranchBindingsAreGuarded(t *testing.T) {
	src := `import { useState } from "react";
export default function Page() {
  const [user, setUser] = useState(null);
  return <main><button onClick={() => setUser({ name: "A", tags: ["x"] })}>in</button>
    {user && <p>{user.name} has {user.tags.length} tags</p>}
    <ul>{user && user.tags.map((t) => <li key={t}>{t}</li>)}</ul>
  </main>;
}`
	_, js := fullPipeline(t, src)
	for _, bare := range []string{"()=>user.name", "()=>user.tags.length"} {
		if strings.Contains(js, bare) {
			t.Errorf("binding %q runs while its branch is inactive:\n%s", bare, js)
		}
	}
	for _, guarded := range []string{"?(user.name):''", "?(user.tags.length):''"} {
		if !strings.Contains(js, guarded) {
			t.Errorf("no guarded read %q:\n%s", guarded, js)
		}
	}
}

// JSX attribute strings and JSX text do not process JS escapes (React keeps
// the backslash sequences literally); only string expressions do.
func TestJSXStringEscapesAreLiteral(t *testing.T) {
	src := `export default function Page() {
  return <main><p title="caf\u00e9 \u{1F600} \n" data-x='a\\b'>plain \u00e9</p><i>{"caf\u00e9"}</i></main>;
}`
	res, _ := fullPipeline(t, src)
	for _, want := range []string{`title="caf\u00e9 \u{1F600} \n"`, `>plain \u00e9</p>`, `<i>café</i>`} {
		if !strings.Contains(res.HTML, want) {
			t.Errorf("HTML lacks %q:\n%s", want, res.HTML)
		}
	}
}

func TestInlineArrayConstructionRendersEveryRowAtBuildTime(t *testing.T) {
	src := `export default function Page() {
  return <main>
    <div id="a">{[...Array(3)].map((_, k) => <i key={k}>a{k}</i>)}</div>
    <div id="d">{Array.from({ length: 3 }, (_, k) => <i key={k}>d{k}</i>)}</div>
    <div id="e">{Array.from({ length: 2 }).map((_, k) => <i key={k}>e{k}</i>)}</div>
  </main>;
}`
	res, js := fullPipeline(t, src)
	for _, want := range []string{
		"<i>a0</i><i>a1</i><i>a2</i>",
		"<i>d0</i><i>d1</i><i>d2</i>",
		"<i>e0</i><i>e1</i>",
	} {
		if !strings.Contains(res.HTML, want) {
			t.Errorf("HTML lacks %q:\n%s", want, res.HTML)
		}
	}
	if js != "" {
		t.Errorf("a page of build-time rows should need no JS:\n%s", js)
	}
}

func TestUnboundUnresolvedSlotWarns(t *testing.T) {
	slot := &irtree.ExprSlot{ID: "7", ExprSource: "fmt(x)", Unresolved: true, File: "page.tsx", Pos: ast.Pos{Line: 3, Col: 9}}
	e := NewEmitter()
	if out := e.emitExprSlot(slot); out.HTML != "<!--k:7--><!--/k:7-->" {
		t.Fatalf("unexpected markup %q", out.HTML)
	}
	// No client code binds the slot: it is a silent empty value.
	warnings := e.unboundSlotWarnings(nil)
	if len(warnings) != 1 || warnings[0].Code != diag.CodeSlotUnresolved || !warnings[0].IsWarning() {
		t.Fatalf("expected one KR8011 warning, got %#v", warnings)
	}
	if !strings.Contains(warnings[0].Error(), "page.tsx:3:9") {
		t.Errorf("warning does not locate the expression: %s", warnings[0].Error())
	}
	// A client binding fills it in at runtime.
	bound := []irtree.ComponentSignature{{Tier: irtree.TierClient, SlotBindings: []irtree.SlotBinding{{SlotID: "7", ExprJS: "fmt(x)"}}}}
	if got := e.unboundSlotWarnings(bound); len(got) != 0 {
		t.Errorf("bound slot reported: %#v", got)
	}
	// A known empty value is not reported.
	known := &irtree.ExprSlot{ID: "8", ExprSource: "''", Initial: ""}
	e2 := NewEmitter()
	e2.emitExprSlot(known)
	if got := e2.unboundSlotWarnings(nil); len(got) != 0 {
		t.Errorf("known value reported: %#v", got)
	}
}

func TestUnknownStaticTextBecomesClientSlot(t *testing.T) {
	src := `export default function Page() { return <main><p>{Math.nope(2)}</p></main>; }`
	res, js := fullPipeline(t, src)
	if !strings.Contains(js, "Math.nope(2)") {
		t.Errorf("an expression with no build-time value has no client binding:\n%s\n%s", res.HTML, js)
	}
}
