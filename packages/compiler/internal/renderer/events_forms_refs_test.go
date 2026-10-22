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

func TestFormDiagnostics(t *testing.T) {
	src := `import { useState } from "react";
export default function App() {
  const [v, setV] = useState("a");
  const [on, setOn] = useState(false);
  return <div>
    <input value={v} />
    <input type="checkbox" checked={on} />
    <input value={v} readOnly />
    <input type="submit" value="Go" />
    <input value={on ? v : undefined} onChange={(e) => setV(e.target.value)} />
    <div contentEditable onChange={() => setOn(true)} />
  </div>;
}`
	tree, _, _ := buildTreeReact(t, src)
	codes := warningCodes(tree)
	if got := codes[diag.CodeReadOnlyControlled]; len(got) != 2 {
		t.Errorf("want 2 KR5004 (value and checked without onChange), got %+v", got)
	}
	if got := codes[diag.CodeControlledSwitch]; len(got) != 1 || !strings.Contains(got[0].Hint, "''") {
		t.Errorf("want one KR5006 suggesting an empty string, got %+v", got)
	}
	if got := codes[diag.CodeContentEditableChange]; len(got) != 1 || !strings.Contains(got[0].Hint, "onInput") {
		t.Errorf("want one KR5007 suggesting onInput, got %+v", got)
	}
	for _, w := range tree.Warnings {
		if !strings.HasPrefix(w.Error(), "page.tsx:") || !strings.Contains(w.Error(), "["+string(w.Code)+"]") {
			t.Errorf("warning should be positioned and coded: %q", w.Error())
		}
	}
}

func TestEventAndRefDiagnostics(t *testing.T) {
	src := `export default function App() {
  return <div>
    <button onClickcapture={() => 1}>a</button>
    <button onTap={() => 1}>b</button>
    <button onclick={() => 1}>c</button>
    <button onclick="go()">d</button>
    <input ref="field" />
    <my-widget onFancyThing={() => 1} />
  </div>;
}`
	tree, _, js := buildTreeReact(t, src)
	codes := warningCodes(tree)
	unknown := codes[diag.CodeUnknownEvent]
	if len(unknown) != 2 {
		t.Fatalf("want 2 KR5001 (onClickcapture, onTap; custom elements exempt), got %+v", tree.Warnings)
	}
	if !strings.Contains(unknown[0].Hint, "onClickCapture") {
		t.Errorf("KR5001 should suggest onClickCapture, got %q", unknown[0].Hint)
	}
	if got := codes[diag.CodeLowercaseEvent]; len(got) != 1 || !strings.Contains(got[0].Hint, "onClick") {
		t.Errorf("want one KR5002 for onclick={fn}, got %+v", got)
	}
	if got := codes[diag.CodeStringRef]; len(got) != 1 {
		t.Errorf("want one KR5003 for a string ref, got %+v", got)
	}
	if strings.Contains(js, "'field'") {
		t.Errorf("a string ref must not be bound:\n%s", js)
	}
}

func TestRefBindingsAndEffectOrder(t *testing.T) {
	src := `import { useRef, useLayoutEffect, useImperativeHandle, forwardRef, useState } from "react";
function Input({ ref, ...rest }) { return <input ref={ref} {...rest} />; }
const Fancy = forwardRef((props, ref) => {
  const inner = useRef(null);
  const [n, setN] = useState(0);
  useImperativeHandle(ref, () => ({ focus: () => inner.current.focus(), n }), [n]);
  return <input ref={inner} onFocus={() => setN(n + 1)} />;
});
export default function App() {
  const a = useRef(null);
  const b = useRef(null);
  const [on, setOn] = useState(true);
  useLayoutEffect(() => { console.log(a.current, b.current); });
  return <div onClick={() => setOn(!on)}>
    <Input ref={a} placeholder="p" />
    <Fancy ref={b} />
    <span ref={(el) => { console.log(el); return () => console.log("cleanup"); }}>x</span>
  </div>;
}`
	_, _, js := buildTreeReact(t, src)
	mustContain(t, "js", js,
		`kbindRef("`, `()=>ref,v=>{ref=v;}`, `()=>inner,v=>{inner=v;}`,
		`useImperativeHandle(props.ref,`, "()=>{\n  return [n()];\n}",
		"__kfx.push(function(){",
		"for(var __kfi=0;__kfi<__kfx.length;__kfi++)__kfx[__kfi]();",
	)
	// The parent's layout effect is queued, the children's refs and handle
	// are bound in their own scopes, and the queue runs after every scope.
	queued := strings.Index(js, "__kfx.push(function(){")
	childRef := strings.Index(js, `()=>ref,v=>{ref=v;}`)
	handle := strings.Index(js, "useImperativeHandle(")
	flush := strings.Index(js, "for(var __kfi=0")
	if !(queued < childRef && childRef < flush && handle < flush) {
		t.Errorf("effects must run after every ref and handle is bound:\n%s", js)
	}
	if strings.Contains(js[handle-30:handle], "__kfx.push") {
		t.Errorf("useImperativeHandle must not be queued with effects:\n%s", js)
	}
}

// Siblings after a child component keep distinct slot IDs, so each handler
// binds its own element.
func TestSiblingSlotIDsSurviveChildComponents(t *testing.T) {
	src := `import { useState } from "react";
function Child() { const [c, setC] = useState(0); return <i onClick={() => setC(c + 1)}>{c}</i>; }
export default function App() {
  const [n, setN] = useState(0);
  return <div>
    <button data-testid="one" onClick={() => setN(1)}>a</button>
    <Child />
    <button data-testid="two" onClick={() => setN(2)}>b</button>
    <Child />
    <button data-testid="three" onClick={() => setN(3)}>c</button>
  </div>;
}`
	_, html, _ := buildTreeReact(t, src)
	seen := map[string]string{}
	for _, id := range []string{"one", "two", "three"} {
		i := strings.Index(html, `data-testid="`+id+`"`)
		if i < 0 {
			t.Fatalf("missing button %s:\n%s", id, html)
		}
		start := strings.LastIndex(html[:i], `data-k="`)
		slot := html[start : start+strings.Index(html[start:], `" `)]
		if other, dup := seen[slot]; dup {
			t.Errorf("buttons %s and %s share %s:\n%s", other, id, slot, html)
		}
		seen[slot] = id
	}
}
