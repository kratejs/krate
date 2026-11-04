package renderer

import (
	"strings"
	"testing"
)

// Props whose value reads a caller signal are registered as getters, and
// `props.X` reads in the child (effects and rendered bindings) stay live,
// including in a signal-less presentational child.
func TestSignalDerivedPropsAreLive(t *testing.T) {
	tree, html, js := buildTreeReact(t, `function Child(props) {
  const [open, setOpen] = createSignal(false);
  createEffect(() => { console.log("title", props.title); });
  return <button onClick={() => setOpen(!open())}>{props.title}</button>;
}
function Plain(props) {
  return <span>{props.title}</span>;
}
export default function Page() {
  const [count, setCount] = createSignal(0);
  return <div>
    <Child title={String(count())} />
    <Plain title={count() + 1} />
    <Plain title="fixed" />
    <button onClick={() => setCount(count() + 1)}>inc</button>
  </div>;
}`)
	mustContain(t, "js", js,
		`__krate_props["1.Child.Child_c0"]={get title(){return String(count())}};`,
		`__krate_props["1.Plain.Plain_c0"]={get title(){return (count() + 1)}};`,
		`kbindContent("3",()=>props.title);`, `kbindContent("5",()=>props.title);`,
		`console.log('title', props.title);`,
	)
	mustNotContain(t, "js", js, `Plain_c1`)
	mustContain(t, "html", html, "<span>fixed</span>", "<!--k:3-->0<!--/k:3-->", "<!--k:5-->1<!--/k:5-->")
	if len(tree.Warnings) != 0 {
		t.Errorf("unexpected warnings: %+v", tree.Warnings)
	}
}

// A destructured parameter that cannot be lowered to live props reads (here
// the body reassigns it) still warns instead of silently rendering a snapshot.
func TestSignalDerivedPropThroughDestructuringWarns(t *testing.T) {
	tree, _, _ := buildTreeReact(t, `function Child({ title }) {
  const [open, setOpen] = createSignal(false);
  title = title + "!";
  return <button onClick={() => setOpen(!open())}>{title}</button>;
}
export default function Page() {
  const [count, setCount] = createSignal(0);
  return <div>
    <Child title={String(count())} />
    <Child title={String(count() + 1)} />
    <button onClick={() => setCount(count() + 1)}>inc</button>
  </div>;
}`)
	if len(tree.Warnings) != 1 {
		t.Fatalf("want 1 warning, got %+v", tree.Warnings)
	}
	w := tree.Warnings[0]
	if !w.IsWarning() || !strings.Contains(w.Message, "title") || !strings.Contains(w.Hint, "props.title") {
		t.Errorf("warning = %+v", w)
	}
}

// A plain destructured parameter is lowered to live props reads: no warning,
// and the registration is a getter the child binding re-reads.
func TestSignalDerivedPropThroughDestructuringIsLive(t *testing.T) {
	_, js := fullPipelineWithReact(t, `import { useState } from 'react';
function Child({ title }) {
  const [open, setOpen] = useState(false);
  return <button onClick={() => setOpen(!open)}>{title}</button>;
}
export default function Page() {
  const [count, setCount] = useState(0);
  return <div><Child title={String(count)} /><button onClick={() => setCount(count + 1)}>inc</button></div>;
}`)
	if !strings.Contains(js, "get title()") || !strings.Contains(js, "props.title") {
		t.Errorf("destructured prop must stay a live getter read:\n%s", js)
	}
}
