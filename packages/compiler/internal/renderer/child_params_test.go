package renderer

import (
	"strings"
	"testing"
)

// A client child with destructured props reads them in an effect and a
// handler; the hydration scope must bind them from the props registry or the
// code throws a ReferenceError at runtime.
func TestDestructuredChildPropsBoundInHydration(t *testing.T) {
	res, js := fullPipeline(t, `function Child({ label, step = 2 }) {
  const [n, setN] = createSignal(step);
  createEffect(() => { console.log(label, n()); });
  return <button onClick={() => setN(n() + step)}>{label}: {n()}</button>;
}
export default function Page() {
  return <div><Child label="A" /><Child label="B" step={3} /></div>;
}`)
	for _, id := range []string{"Child_c0", "Child_c1"} {
		want := `var props=__krate_props["1.Child.` + id + `"];var {label,step=2}=props;`
		if !strings.Contains(js, want) {
			t.Errorf("missing destructured param binding %q:\n%s", want, js)
		}
		if !strings.Contains(js, `__krate_props["1.Child.`+id+`"]={`) {
			t.Errorf("missing props registration for %s:\n%s", id, js)
		}
	}
	// The omitted prop takes its pattern default in SSR and the signal init.
	if !strings.Contains(res.HTML, "A: <!--k:3-->2<!--/k:3-->") || !strings.Contains(js, "createSignal(2)") {
		t.Errorf("omitted destructured prop should take its default:\nHTML:\n%s\nJS:\n%s", res.HTML, js)
	}
	if strings.Contains(js, "'step'") {
		t.Errorf("param name leaked as a string value:\n%s", js)
	}
}

// A whole-props parameter with another name binds that name.
func TestRenamedPropsParamBoundInHydration(t *testing.T) {
	_, js := fullPipeline(t, `function Other(p) {
  const [n, setN] = createSignal(0);
  return <button onClick={() => setN(n() + p.step)}>{n()}</button>;
}
export default function Page() {
  return <div><Other step={4} /></div>;
}`)
	if !strings.Contains(js, `var props=__krate_props["1.Other.Other_c0"];var p=props;`) {
		t.Errorf("renamed props parameter not bound:\n%s", js)
	}
}

// Destructured props that only feed build-time output need no registry.
func TestDestructuredChildPropsUnusedAtHydration(t *testing.T) {
	_, js := fullPipeline(t, `function Child({ label }) {
  const [n, setN] = createSignal(0);
  return <button onClick={() => setN(n() + 1)}>{label}: {n()}</button>;
}
export default function Page() {
  return <div><Child label="A" /></div>;
}`)
	if strings.Contains(js, "__krate_props[\"1.Child") {
		t.Errorf("unexpected props registration:\n%s", js)
	}
}

// Custom hooks called as bare statements run in the hydration scope, in
// source order, and their definitions are hoisted into it.
func TestCustomHookStatementsReplayed(t *testing.T) {
	_, js := fullPipeline(t, `function useLog(msg) {
  createEffect(() => { console.log(msg); });
}
function useTitle(t) {
  createEffect(() => { document.title = t; });
}
export default function Page() {
  const [count, setCount] = createSignal(0);
  useLog("first");
  useTitle("hello " + count());
  return <button onClick={() => setCount(count() + 1)}>{count()}</button>;
}`)
	first := strings.Index(js, "useLog('first');")
	second := strings.Index(js, "useTitle(('hello ' + count()));")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("custom hook calls missing or out of order:\n%s", js)
	}
	if sig := strings.Index(js, "createSignal(0)"); sig < 0 || sig > first {
		t.Errorf("custom hooks should run after the signal declarations:\n%s", js)
	}
	for _, def := range []string{"function useLog(msg)", "function useTitle(t)"} {
		if !strings.Contains(js, def) {
			t.Errorf("hook definition %q not hoisted:\n%s", def, js)
		}
	}
}

// Runtime hooks keep their dedicated passes: a bare useEffect is not
// replayed verbatim.
func TestRuntimeHookStatementNotReplayedVerbatim(t *testing.T) {
	_, js := fullPipeline(t, `export default function Page() {
  const [count, setCount] = createSignal(0);
  useDebugValue(count());
  return <button onClick={() => setCount(count() + 1)}>{count()}</button>;
}`)
	if strings.Contains(js, "useDebugValue(") {
		t.Errorf("runtime hook replayed verbatim:\n%s", js)
	}
}

// Function-prop aliases are emitted in declaration order on every build.
func TestFuncPropAliasesDeterministic(t *testing.T) {
	src := `function Child(props) {
  var onA = props.onA;
  var onB = props.onB;
  var onC = props.onC;
  var onD = props.onD;
  onMount(function () { onA(); onB(); onC(); onD(); });
  return <div>child</div>;
}
export default function Page() {
  const [x, setX] = createSignal(0);
  function a() { setX(1); }
  function b() { setX(2); }
  function c() { setX(3); }
  function d() { setX(4); }
  return <div><Child onA={a} onB={b} onC={c} onD={d} /><span>{x()}</span></div>;
}`
	_, first := fullPipeline(t, src)
	want := "var onA=props.onA;\nvar onB=props.onB;\nvar onC=props.onC;\nvar onD=props.onD;\n"
	if !strings.Contains(first, want) {
		t.Fatalf("aliases not in declaration order:\n%s", first)
	}
	for i := 0; i < 20; i++ {
		if _, js := fullPipeline(t, src); js != first {
			t.Fatalf("nondeterministic output on run %d:\n%s\nvs\n%s", i, js, first)
		}
	}
}
