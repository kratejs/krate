package renderer

import (
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// scopeOf returns the hydration scope of the component whose id ends with
// suffix.
func scopeOf(t *testing.T, js, suffix string) string {
	t.Helper()
	for _, part := range strings.Split(js, "// component ")[1:] {
		id, _, _ := strings.Cut(part, "\n")
		if strings.HasSuffix(id, suffix) {
			return part
		}
	}
	t.Fatalf("no scope %s in:\n%s", suffix, js)
	return ""
}

func TestLocalHandlerBoundByName(t *testing.T) {
	src := `import { createSignal } from "@krate/runtime";
function Code(props) {
  const [copied, setCopied] = createSignal(false);
  var wrapRef = null;
  function handleCopy(e) {
    if (!wrapRef) return;
    setCopied(true);
    console.log(this, arguments.length, e.type);
  }
  const toggle = (e) => setCopied(!copied());
  return <div ref={wrapRef}>
    <button onClick={handleCopy}>{copied() ? "Copied" : "Copy"}</button>
    <b onClick={handleCopy}>again</b>
    <i onClick={toggle}>t</i>
    <u onClick={(e) => setCopied(e.type === "x")}>z</u>
  </div>;
}
export default function App() {
  return <section><Code lang="ts">a</Code><Code lang="go">b</Code></section>;
}`
	_, _, js := buildTreeReact(t, src)
	for _, inst := range []string{"Code_c0", "Code_c1"} {
		scope := scopeOf(t, js, inst)
		// The body is emitted once, as the declaration the handlers name.
		if n := strings.Count(scope, "console.log(this, arguments.length, e.type)"); n != 1 {
			t.Errorf("%s: handleCopy body emitted %d times:\n%s", inst, n, scope)
		}
		if n := len(regexp.MustCompile(`kbindHandler\("[^"]+","onClick",handleCopy\)`).FindAllString(scope, -1)); n != 2 {
			t.Errorf("%s: want 2 handlers bound to handleCopy, got %d:\n%s", inst, n, scope)
		}
		mustContain(t, inst, scope,
			"function handleCopy(e)",
			"var toggle=(e)=>setCopied(!copied());",
			`"onClick",toggle);`,
			`"onClick",(e)=>setCopied((e.type === 'x')));`)
	}
}

func TestListHandlerKeepsLoopBinding(t *testing.T) {
	src := `import { createSignal } from "@krate/runtime";
export default function App() {
  const [picked, setPicked] = createSignal("none");
  function pick(id) { setPicked(id); }
  function clear() { setPicked("none"); }
  return <div><p>{picked()}</p>{["a", "b"].map((id) => <button onClick={() => pick(id)}>{id}</button>)}{["c"].map((pick) => <i onClick={clear}>{pick}</i>)}</div>;
}`
	_, _, js := buildTreeReact(t, src)
	// An inline arrow still sees its row's value; a callback parameter that
	// shadows a local function does not change what onClick={clear} names.
	mustContain(t, "js", js, `"onClick",()=>pick('a'));`, `"onClick",()=>pick('b'));`, `"onClick",clear);`)
}

func TestUnreadPropsNotRegistered(t *testing.T) {
	src := `import { createSignal } from "@krate/runtime";
function shout(s) { return s.toUpperCase(); }
function Child(props) {
  const tone = props.tone;
  const label = props.label;
  const [n, setN] = createSignal(0);
  return <button class={tone} onClick={() => setN(n() + 1)}>{label}{n()}</button>;
}
export default function App() {
  const [m, setM] = createSignal(0);
  return <div><Child tone="red" label="hi" extra={shout("x" + m())} /><b onClick={() => setM(m() + 1)}>{m()}</b></div>;
}`
	_, html, js := buildTreeReact(t, src)
	mustContain(t, "html", html, "red", "hi")
	// Nothing on the client reads a prop: no registration, no props binding,
	// no locals that only fed the server render, and no helper that only the
	// dropped registration called.
	child := scopeOf(t, js, "Child_c0")
	mustNotContain(t, "child scope", child, "props", "var tone", "var label")
	mustNotContain(t, "js", js, "__krate_props", "shout")
}

func TestEmptyScopes(t *testing.T) {
	c := irtree.TierClient
	sigs := []irtree.ComponentSignature{
		{ComponentID: "root", Tier: c}, // 0 empty, top level, plain
		{ComponentID: "branch", Tier: c, ParentID: "root", Conds: []irtree.CondBranch{{SlotID: "4", Branch: "c"}}}, // 1 empty, in a branch
		{ComponentID: "kid", Tier: c, ParentID: "branch"},                                                          // 2 has code
		{ComponentID: "leaf", Tier: c, ParentID: "root"},                                                           // 3 empty leaf
		{ComponentID: "list", Tier: c, ListSlot: "9"},                                                              // 4 empty, list slot
		{ComponentID: "row", Tier: c, ParentID: "list"},                                                            // 5 has code
		{ComponentID: "lazy", Tier: c, Lazy: true},                                                                 // 6 lazy
		{ComponentID: "other", Tier: c, RouteScope: "layout"},                                                      // 7 empty, other route scope
		{ComponentID: "inner", Tier: c, ParentID: "other"},                                                         // 8 has code
	}
	bodies := []string{"", "", "x();\n", "", "", "y();\n", "", "", "z();\n"}
	got := emptyScopes(sigs, bodies, false, true)
	want := map[int]bool{0: true, 3: true}
	if len(got) != len(want) || !got[0] || !got[3] {
		t.Errorf("owners: skip = %v, want %v", got, want)
	}
	// Without owners nothing links scopes: every empty one goes.
	got = emptyScopes(sigs, bodies, false, false)
	for _, i := range []int{0, 1, 3, 4, 7} {
		if !got[i] {
			t.Errorf("no owners: scope %d kept", i)
		}
	}
	if got[6] {
		t.Error("lazy scope skipped")
	}
	if emptyScopes(sigs, bodies, true, true) != nil {
		t.Error("class owner tree page skipped scopes")
	}
}
