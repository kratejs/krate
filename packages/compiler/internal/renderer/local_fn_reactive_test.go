package renderer

import (
	"strings"
	"testing"
)

func TestLocalFnReadingSignalStaysBound(t *testing.T) {
	cases := map[string]string{
		"arrow": `const cur = () => list[n()];
  const cls = (i: number) => (n() === i ? "on" : "");`,
		"declaration": `function cur() { return list[n()]; }
  function cls(i: number) { return n() === i ? "on" : ""; }`,
		"transitive": `const read = () => n();
  const cur = () => list[read()];
  const cls = (i: number) => (read() === i ? "on" : "");`,
	}
	for name, decls := range cases {
		t.Run(name, func(t *testing.T) {
			src := `import { createSignal } from '@krate/runtime';
const list = [{ label: "alpha" }, { label: "beta" }];
export default function Page() {
  const [n, setN] = createSignal(0);
  ` + decls + `
  return <div><p>{cur().label}</p>{list.map((x, i) => <button class={cls(i)} onClick={() => setN(i)}>{x.label}</button>)}</div>;
}`
			_, html, js := buildTreeReact(t, src)
			mustContain(t, "html", html, "alpha")
			mustContain(t, "js", js, "cur().label", "cls(")
			if !strings.Contains(js, "kbindContent(") && !strings.Contains(js, "kbindText(") {
				t.Errorf("js: expected a text binding for cur().label:\n%s", js)
			}
			mustContain(t, "js", js, "kbindAttr(")
		})
	}
}

func TestLocalFnParamShadowsSignalName(t *testing.T) {
	src := `import { createSignal } from '@krate/runtime';
export default function Page() {
  const [n, setN] = createSignal(0);
  const label = (n: number) => "item " + n;
  return <div><p>{label(3)}</p><button onClick={() => setN(1)}>{n()}</button></div>;
}`
	_, html, js := buildTreeReact(t, src)
	mustContain(t, "html", html, "item 3")
	mustNotContain(t, "js", js, "label(")
}

func TestNonReactiveLocalFnStillFolds(t *testing.T) {
	src := `const list = ["a", "b"];
export default function Page() {
  const cur = () => list[1];
  const cls = (i: number) => (i === 1 ? "on" : "");
  return <div><p>{cur()}</p><span class={cls(1)}>s</span></div>;
}`
	_, html, js := buildTreeReact(t, src)
	mustContain(t, "html", html, "<p>b</p>", `class="on"`)
	mustNotContain(t, "js", js, "kbind", "cur(", "cls(")
}

func TestStatelessChildrenTextBindsCallerSignal(t *testing.T) {
	src := `import { createSignal } from '@krate/runtime';
function Panel(props: { title: string; children?: any }) {
  return <section class="panel"><h2>{props.title}</h2>{props.children}</section>;
}
export default function Page() {
  const [open, setOpen] = createSignal(false);
  return <div><button onClick={() => setOpen(!open())}>t</button><Panel title="t">{open() ? "Open" : "Closed"}<span class={open() ? "a" : "b"}>x</span></Panel><Panel title="u">{open() ? "Open" : "Closed"}</Panel></div>;
}`
	_, html, js := buildTreeReact(t, src)
	mustContain(t, "html", html, "Closed")
	if n := strings.Count(js, `?'Open':'Closed'`); n != 2 {
		t.Errorf("expected 2 content bindings (first and repeated copy), got %d in:\n%s", n, js)
	}
	mustContain(t, "js", js, "kbindAttr(")
}

func TestStatelessChildrenRenderedTwiceBindEveryCopy(t *testing.T) {
	src := `import { createSignal } from '@krate/runtime';
function Twice(props: { children?: any }) {
  return <div><header>{props.children}</header><footer>{props.children}</footer></div>;
}
export default function Page() {
  const [open, setOpen] = createSignal(false);
  return <div><button onClick={() => setOpen(!open())}>t</button><Twice>{open() ? "Open" : "Closed"}</Twice></div>;
}`
	_, _, js := buildTreeReact(t, src)
	if n := strings.Count(js, `?'Open':'Closed'`); n != 2 {
		t.Errorf("expected a binding for each rendered copy, got %d in:\n%s", n, js)
	}
}
