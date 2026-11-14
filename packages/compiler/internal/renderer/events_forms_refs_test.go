package renderer

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

func warningCodes(tree *irtree.ComponentTree) map[diag.Code][]diag.Diagnostic {
	out := map[diag.Code][]diag.Diagnostic{}
	for _, w := range tree.Warnings {
		out[w.Code] = append(out[w.Code], w)
	}
	return out
}

func TestEventHandlersUseReactPropNames(t *testing.T) {
	src := `import { useState } from "react";
export default function App() {
  const [n, setN] = useState(0);
  return <div onClickCapture={() => setN(1)} onClick={() => setN(2)} onDoubleClick={() => setN(3)}
    onMouseEnter={() => setN(4)} onGotPointerCapture={() => setN(5)} onFocus={() => setN(6)}>{n}</div>;
}`
	_, _, js := buildTreeReact(t, src)
	mustContain(t, "js", js,
		`"onClickCapture",`, `"onClick",`, `"onDoubleClick",`, `"onMouseEnter",`, `"onGotPointerCapture",`, `"onFocus",`)
	mustNotContain(t, "js", js, "__krate_del_add", "kbindEvent(", "addEventListener")
}

func TestFormControlBindings(t *testing.T) {
	src := `import { useState } from "react";
export default function App() {
  const [v, setV] = useState("a");
  const [d, setD] = useState("d");
  return <form>
    <input value={v} onChange={(e) => setV(e.target.value)} />
    <input defaultValue={d} onBlur={() => setD("x")} />
    <input type="checkbox" defaultChecked={v === "a"} onBlur={() => setD("y")} />
    <input value="x" defaultValue="y" onChange={() => setV("z")} />
  </form>;
}`
	tree, html, js := buildTreeReact(t, src)
	mustContain(t, "js", js, `,"value",()=>v())`, `,"defaultValue",()=>d())`, `,"defaultChecked",()=>`)
	if strings.Contains(html, `value="x" value="y"`) || strings.Contains(html, `value="y"`) {
		t.Errorf("an ignored defaultValue must not render a second value attribute:\n%s", html)
	}
	if got := warningCodes(tree)[diag.CodeValueAndDefault]; len(got) != 1 || got[0].Hint == "" {
		t.Errorf("want one KR5005 warning with a hint, got %+v", tree.Warnings)
	}
}
