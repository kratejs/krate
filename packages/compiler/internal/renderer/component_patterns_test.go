package renderer

import (
	"strings"
	"testing"
)

// Destructured params with defaults, renames, nesting and a rest element read
// through the live props object.
func TestDestructuredPropsLowerToLiveReads(t *testing.T) {
	res, js := fullPipelineWithReact(t, `import { useState } from 'react';
function Label({ value, tone = "x", user: { name: n }, ...rest }) {
  return <p className={tone} {...rest}>{n}: {value}</p>;
}
export default function Page() {
  const [v, setV] = useState(0);
  return <div><button onClick={() => setV(v + 1)}>inc</button><Label value={v} tone={v % 2 ? "odd" : "even"} user={{ name: "ann" }} id="l" /></div>;
}`)
	mustContain(t, "html", res.HTML, `id="l"`)
	mustContain(t, "js", js, "get value()", "get tone()")
	mustContain(t, "js", js, `kbindAttr("`, `"class"`)
	mustNotContain(t, "js", js, "var {value")
}

// A rest element used for anything but a spread or a member read keeps the
// snapshot binding.
func TestRestUsedAsValueKeepsSnapshotBinding(t *testing.T) {
	_, js := fullPipeline(t, `function C({ a, ...rest }) {
  const [n, setN] = createSignal(0);
  return <button onClick={() => setN(n() + Object.keys(rest).length)}>{a}{n()}</button>;
}
export default function Page() { return <C a="x" b="y" />; }`)
	mustContain(t, "js", js, "var {a,...rest}=props")
}

// {n && <b/>} renders n for a falsy number like React; a boolean test does not.
func TestGuardRendersFalsyOperand(t *testing.T) {
	res, _ := fullPipelineWithReact(t, `import { useState } from 'react';
export default function Page() {
  const [n, setN] = useState(0);
  const [on, setOn] = useState(false);
  return <div><button onClick={() => setN(n + 1)}>inc</button><p id="a">{n && <b>has</b>}</p><p id="b">{on && <b>on</b>}</p><p id="c">{n || <i>none</i>}</p></div>;
}`)
	mustContain(t, "html", res.HTML, `<p id="a"><!--k:`, `0<!--/k:`, `<p id="c"><!--k:`)
	mustContain(t, "templates", res.HTML, `<template data-kc=`, `<i>none</i>`)
	if strings.Count(res.HTML, "<b>on</b>") != 1 {
		t.Errorf("a boolean guard keeps an empty alternate branch:\n%s", res.HTML)
	}
}

// A reactive dangerouslySetInnerHTML is rendered for SSR and rebound.
func TestReactiveInnerHTMLRebinds(t *testing.T) {
	res, js := fullPipelineWithReact(t, `import { useState } from 'react';
export default function Page() {
  const [html, setHtml] = useState("<b>one</b>");
  return <div id="out" dangerouslySetInnerHTML={{ __html: html }} />;
}`)
	mustContain(t, "html", res.HTML, `<b>one</b>`, `data-k=`)
	mustNotContain(t, "html", res.HTML, `innerhtml=`)
	mustContain(t, "js", js, "kbindInnerHTML(")
}

// A conditional inside <svg> keeps its template in the SVG namespace.
func TestSVGConditionalTemplateIsWrapped(t *testing.T) {
	res, _ := fullPipelineWithReact(t, `import { useState } from 'react';
export default function Page() {
  const [show, setShow] = useState(false);
  return <div><button onClick={() => setShow(!show)}>t</button><svg>{show && <g><path d="M0 0" /></g>}</svg><p>{show && <b>x</b>}</p></div>;
}`)
	mustContain(t, "svg template", res.HTML, `data-kns="svg"><svg><g><path d="M0 0"></path></g></svg></template>`)
	if strings.Count(res.HTML, `data-kns="svg"`) != 1 {
		t.Errorf("only the conditional inside <svg> is wrapped:\n%s", res.HTML)
	}
}

// A JSX value held in a derived variable renders its initial branch.
func TestDerivedJSXValueRendersAsConditional(t *testing.T) {
	res, js := fullPipelineWithReact(t, `import { useState } from 'react';
export default function Page() {
  const [on, setOn] = useState(false);
  const [tab, setTab] = useState("a");
  const badge = on ? <b>on</b> : <i>off</i>;
  let content;
  switch (tab) {
    case "a": content = <u>A</u>; break;
    default: content = <s>B</s>;
  }
  return <div><p>{badge}</p><div>{content}</div><button onClick={() => setOn(!on)}>t</button></div>;
}`)
	mustContain(t, "html", res.HTML, `<i>off</i>`, `<u>A</u>`, `<template data-kc=`)
	mustContain(t, "js", js, "kbindCond(")
	mustNotContain(t, "js", js, "kbindText(")
}

// props.children inside a hidden branch is spliced into the template.
func TestChildrenPlaceholderInHiddenBranchTemplate(t *testing.T) {
	res, _ := fullPipelineWithReact(t, `import { useState } from 'react';
function Disclosure(props) {
  const [open, setOpen] = useState(false);
  return <div><button onClick={() => setOpen(!open)}>t</button>{open && <section>{props.children}</section>}</div>;
}
export default function Page() { return <Disclosure><span id="inner">hello</span></Disclosure>; }`)
	mustContain(t, "html", res.HTML, `<section><span id="inner">hello</span></section>`)
	mustNotContain(t, "html", res.HTML, "__children__")
}

// Provider values reach rows rendered later by the list runtime.
func TestContextReachesListRows(t *testing.T) {
	_, js := fullPipelineWithReact(t, `import { createContext, useContext, useState } from 'react';
const Sel = createContext("");
function Row(props) {
  const sel = useContext(Sel);
  return <li className={sel === props.id ? "sel" : ""}>{props.id}</li>;
}
export default function Page() {
  const [sel, setSel] = useState("a");
  const [ids, setIds] = useState(["a", "b"]);
  return <Sel.Provider value={sel}><ul>{ids.map((id) => <Row key={id} id={id} />)}</ul></Sel.Provider>;
}`)
	mustContain(t, "js", js, "krateContextRows([[", "Sel.useContextLive()")
	mustNotContain(t, "js", js, "krateDerived(()=>Sel.useContext())")
}

// Spreads the compiler cannot expand are applied by the runtime.
func TestUnresolvedElementSpreadIsBound(t *testing.T) {
	_, js := fullPipelineWithReact(t, `import { useState } from 'react';
function useField(name) {
  const [v, setV] = useState("");
  return { value: v, name, onChange: (e) => setV(e.target.value) };
}
export default function Page() {
  const [n, setN] = useState(0);
  const field = useField("email");
  return <div><input {...field} /><p {...(n > 0 ? { title: "x" } : {})}>p</p><button onClick={() => setN(n + 1)}>go</button></div>;
}`)
	if strings.Count(js, "kbindSpread(") != 2 {
		t.Errorf("both spreads must be bound:\n%s", js)
	}
}

// A function as the only child is the component's children prop.
func TestFunctionChildIsChildrenProp(t *testing.T) {
	_, js := fullPipelineWithReact(t, `import { useState } from 'react';
function Toggle(props) {
  const [on, setOn] = useState(false);
  return <div>{props.children(on, () => setOn(!on))}</div>;
}
export default function Page() {
  return <Toggle>{(on, flip) => <button onClick={flip}>{on ? "ON" : "OFF"}</button>}</Toggle>;
}`)
	mustContain(t, "js", js, "children:(on, flip)", "kbindContent(")
}

// Children.map over the literal call-site children unrolls.
func TestChildrenMapUnrollsOverCallSiteChildren(t *testing.T) {
	res, _ := fullPipelineWithReact(t, `import { Children, cloneElement, useState } from 'react';
function Tab(props) { return <button className={props.active ? "act" : ""}>{props.name}</button>; }
function Tabs(props) {
  const [active, setActive] = useState(0);
  return <div>{Children.map(props.children, (c, i) => cloneElement(c, { active: i === active, onSelect: () => setActive(i) }))}</div>;
}
export default function Page() { return <Tabs><Tab name="a" /><Tab name="b" /></Tabs>; }`)
	mustContain(t, "html", res.HTML, `>a</button>`, `>b</button>`, `class="act"`)
}

// A simple higher-order component compiles as the component it returns.
func TestHigherOrderComponentInstantiated(t *testing.T) {
	res, js := fullPipelineWithReact(t, `import { useState } from 'react';
function Button(props) { return <button onClick={props.onClick}>{props.label}:{props.count}</button>; }
function withCounter(Wrapped) {
  return function WithCounter(props) {
    const [count, setCount] = useState(0);
    return <Wrapped {...props} count={count} onClick={() => setCount(count + 1)} />;
  };
}
const CounterButton = withCounter(Button);
export default function Page() { return <CounterButton label="clicks" />; }`)
	mustContain(t, "html", res.HTML, `<button`, `clicks:`)
	mustContain(t, "js", js, "onClick")
}

// Custom hook arguments that read state are passed as live getters.
func TestHookArgumentsAreLive(t *testing.T) {
	_, js := fullPipelineWithReact(t, `import { useEffect, useState } from 'react';
function useDebounce(value, ms) {
  const [d, setD] = useState(value);
  useEffect(() => { const t = setTimeout(() => setD(value), ms); return () => clearTimeout(t); }, [value, ms]);
  return d;
}
export default function Page() {
  const [n, setN] = useState(0);
  const d = useDebounce(n, 20);
  return <button onClick={() => setN(n + 1)}>{d}</button>;
}`)
	mustContain(t, "js", js, "krateDerived(()=>", "krateParam(")
}

// setState during render runs as an effect.
func TestRenderPhaseSetStateRunsAsEffect(t *testing.T) {
	_, js := fullPipelineWithReact(t, `import { useState } from 'react';
function Counter(props) {
  const [prev, setPrev] = useState(props.value);
  const [changes, setChanges] = useState(0);
  if (props.value !== prev) { setPrev(props.value); setChanges(changes + 1); }
  return <p>{changes}</p>;
}
export default function Page() {
  const [n, setN] = useState(0);
  return <div><button onClick={() => setN(n + 1)}>inc</button><Counter value={n} /></div>;
}`)
	mustContain(t, "js", js, "createEffect(")
}
