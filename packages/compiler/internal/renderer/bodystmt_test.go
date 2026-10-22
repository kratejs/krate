package renderer

import (
	"strings"
	"testing"
)

// Component-body statements that are not declarations must run in the
// hydration scope in source order, and build-time folding must never keep a
// value such a statement changes before the read.

// assertInOrder fails unless every fragment occurs in js, in the given order.
func assertInOrder(t *testing.T, js string, frags ...string) {
	t.Helper()
	at := 0
	for _, f := range frags {
		i := strings.Index(js[at:], f)
		if i < 0 {
			t.Fatalf("missing %q (in order) in hydration JS:\n%s", f, js)
		}
		at += i + len(f)
	}
}

func TestBodyStmtMapMutationReplayed(t *testing.T) {
	src := `export default function Index() {
  const cache = new Map();
  cache.set("k", 1);
  return <main><p id="kw">{cache.has("k") ? "yes" : "no"}</p></main>;
}`
	res, js := fullPipeline(t, src)
	if strings.Contains(res.HTML, ">no<") {
		t.Fatalf("read folded without the preceding mutation:\n%s", res.HTML)
	}
	assertInOrder(t, js,
		"var cache=new Map();\n",
		"cache.set('k', 1);\n",
		`kbindContent("3",()=>(cache.has('k')?'yes':'no'));`)
}

func TestBodyStmtArrayPushFolds(t *testing.T) {
	src := `export default function Index() {
  const arr = [1, 2, 3];
  arr.push(4);
  return <main><p>{arr.length}</p></main>;
}`
	res, js := fullPipeline(t, src)
	if !strings.Contains(res.HTML, "<p>4</p>") {
		t.Fatalf("arr.length must fold to the post-push value:\n%s", res.HTML)
	}
	assertInOrder(t, js, "var arr=[1,2,3];\n", "arr.push(4);\n")
}

func TestBodyStmtArrayMutationDefersFold(t *testing.T) {
	src := `export default function Index() {
  const arr = [3, 1, 2];
  arr.sort();
  return <main><p>{arr[0]}</p></main>;
}`
	res, js := fullPipeline(t, src)
	if strings.Contains(res.HTML, "<p>3</p>") {
		t.Fatalf("arr[0] folded to the pre-sort value:\n%s", res.HTML)
	}
	assertInOrder(t, js, "var arr=[3,1,2];\n", "arr.sort();\n", `()=>arr[0]`)
}

func TestBodyStmtMemberWriteFolds(t *testing.T) {
	src := `export default function Index() {
  const o = { n: 1 };
  o.n = 2;
  return <main><p>{o.n}</p></main>;
}`
	res, js := fullPipeline(t, src)
	if !strings.Contains(res.HTML, "<p>2</p>") {
		t.Fatalf("o.n must fold to the written value:\n%s", res.HTML)
	}
	assertInOrder(t, js, "var o={n:1};\n", "(o.n = 2);\n")
}

func TestBodyStmtAliasedWriteDefersFold(t *testing.T) {
	src := `export default function Index() {
  const o = { n: 1 };
  const q = o;
  q.n = 2;
  return <main><p>{o.n}</p></main>;
}`
	res, js := fullPipeline(t, src)
	if strings.Contains(res.HTML, "<p>1</p>") {
		t.Fatalf("o.n folded although an alias changed it:\n%s", res.HTML)
	}
	assertInOrder(t, js, "var o={n:1};\n", "var q=o;\n", "(q.n = 2);\n", `()=>o.n`)
}

func TestBodyStmtUpdateFoldsAndReplays(t *testing.T) {
	src := `export default function Index() {
  let n = 1;
  n++;
  n += 3;
  return <main><p>{n}</p></main>;
}`
	res, js := fullPipeline(t, src)
	if !strings.Contains(res.HTML, "<p>5</p>") {
		t.Fatalf("expected the updated value folded:\n%s", res.HTML)
	}
	assertInOrder(t, js, "var n=1;\n", "n++;\n", "(n += 3);\n")
}

func TestBodyStmtControlFlowMutationDefersFold(t *testing.T) {
	src := `export default function Index() {
  const arr = [];
  try { arr.push(1); } catch (e) {}
  switch (arr.length) { case 1: arr.push(2); }
  let label = "a";
  if (Math.random() > 2) label = "b";
  return <main><p>{arr.length}</p><i>{label}</i></main>;
}`
	res, js := fullPipeline(t, src)
	if strings.Contains(res.HTML, "<p>0</p>") || strings.Contains(res.HTML, "<i>a</i>") {
		t.Fatalf("value folded across a statement that changes it:\n%s", res.HTML)
	}
	assertInOrder(t, js, "var arr=[];\n", "try {", "arr.push(1);", "switch (arr.length)", "arr.push(2);",
		"var label='a';\n", "if (", "label = 'b'")
}

func TestBodyStmtSignalReadingStatementFollowsSignals(t *testing.T) {
	src := `export default function Index() {
  const [c, setC] = createSignal(0);
  const arr = [1];
  arr.push(c());
  createEffect(() => console.log(c()));
  return <main><p>{arr.length}</p><b>{c()}</b></main>;
}`
	_, js := fullPipeline(t, src)
	assertInOrder(t, js,
		"var arr=[1];\n",
		"const [c,setC]=createSignal(0);\n",
		"arr.push(c());\n",
		"createEffect(()=>console.log(c()));\n")
}

func TestBodyStmtHooksNotReplayed(t *testing.T) {
	src := `export default function Index() {
  const [c, setC] = createSignal(0);
  onMount(() => console.log("m"));
  createEffect(() => console.log(c()));
  "use client";
  return <main><b>{c()}</b></main>;
}`
	_, js := fullPipeline(t, src)
	if strings.Count(js, "onMount(") != 1 || strings.Count(js, "createEffect(") != 1 {
		t.Fatalf("hook calls must be emitted exactly once by their own pass:\n%s", js)
	}
	if strings.Contains(js, "'use client'") {
		t.Fatalf("directive replayed:\n%s", js)
	}
}

func TestBodyStmtReactEffectDepsKeepSourceOrder(t *testing.T) {
	src := `import { useState, useEffect } from 'react';
export default function Index() {
  const [c, setC] = useState(0);
  useEffect(() => { console.log("a", c); }, [c]);
  useEffect(() => { console.log("b"); }, []);
  useEffect(() => { console.log("c"); });
  return <main><button onClick={() => setC(c + 1)}>{c}</button></main>;
}`
	_, js := fullPipelineWithReact(t, src)
	assertInOrder(t, js, "createEffectDeps(", "console.log('a', c());", "return [c()];",
		"createEffectDeps(", "console.log('b');", "return [];",
		"createEffectDeps(", "console.log('c');", "return [c()];")
}

func TestBodyStmtChildLocalsMergeInSourceOrder(t *testing.T) {
	src := `function Row(props) {
  const [n, setN] = createSignal(0);
  const list = [props.a];
  list.push("b");
  const size = list.length;
  return <p onClick={() => setN(n() + size)}>{n()}</p>;
}
export default function Index() {
  return <main><Row a="x" /></main>;
}`
	_, js := fullPipeline(t, src)
	assertInOrder(t, js, "var list=[props.a];\n", "list.push('b');\n", "var size=list.length;\n")
}

func TestBodyStmtParamReassignmentFoldedNotReplayed(t *testing.T) {
	src := `function Btn({ className }) {
  const [n, setN] = createSignal(0);
  className = className + " x";
  return <button class={className} onClick={() => setN(n() + 1)}>{n()}</button>;
}
export default function Index() {
  return <main><Btn className="base" /></main>;
}`
	res, js := fullPipeline(t, src)
	if !strings.Contains(res.HTML, `class="base x"`) {
		t.Fatalf("parameter reassignment not folded:\n%s", res.HTML)
	}
	if strings.Contains(js, "className") {
		t.Fatalf("parameter reassignment replayed at hydration:\n%s", js)
	}
}
