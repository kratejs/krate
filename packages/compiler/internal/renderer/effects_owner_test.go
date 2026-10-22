package renderer

import (
	"strings"
	"testing"
)

const ownerPageSrc = `import { useState, useEffect, useLayoutEffect } from 'react';
function Child({ n }) {
  useEffect(() => { console.log("passive", n); return () => console.log("cleanup"); }, [n]);
  useLayoutEffect(() => { console.log("layout"); }, []);
  return <i>{n}</i>;
}
export default function Index() {
  const [c, setC] = useState(0);
  const [show, setShow] = useState(true);
  useEffect(() => { console.log("parent", c); });
  return <main><button onClick={() => setShow(!show)}>{c}</button>{show && <Child n={c} />}<ul>{[1, 2].map(i => <li key={i}><Child n={i} /></li>)}</ul></main>;
}`

func TestHydrationDeclaresEffectOwners(t *testing.T) {
	_, js := fullPipelineWithReact(t, ownerPageSrc)
	assertInOrder(t, js,
		"var __ko=typeof krateOwner==='function'?krateOwner:function(){};\n",
		"(function(){\n__ko(\"Index_c0\",null);\n",
		// No-deps effect: deps are the component's state.
		"return [c(),show()];",
		// The child in the conditional's consequent, shown on the server.
		"(function(){\n__ko(\"4.c.Child.Child_c0\",\"Index_c0\",[[\"4\",\"c\",1]]);\n",
		"__ko(null);\n")
	// List rows are rendered and adopted by the keyed list runtime, which
	// gives each row its own effect owner.
	if !strings.Contains(js, `kbindList("7"`) || strings.Contains(js, `__ko("8.Child.Child_c1"`) {
		t.Fatalf("list rows must not also hydrate as separate owners:\n%s", js)
	}
	if strings.Contains(js, "onMount(") {
		t.Fatalf("React effects must not lower to onMount:\n%s", js)
	}
	assertInOrder(t, js, "createEffectDeps(", "return [];", "}, 'layout');")
}

func TestHydrationOmitsOwnersWithoutEffects(t *testing.T) {
	_, js := fullPipelineWithReact(t, `import { useState } from 'react';
export default function Index() {
  const [c, setC] = useState(0);
  return <button onClick={() => setC(c + 1)}>{c}</button>;
}`)
	if strings.Contains(js, "__ko") {
		t.Fatalf("pages without effects should not declare owners:\n%s", js)
	}
}

func TestHydrationAlternateBranchOwnerStartsHidden(t *testing.T) {
	_, js := fullPipelineWithReact(t, `import { useState, useEffect } from 'react';
function Panel() {
  useEffect(() => { console.log("mounted"); }, []);
  return <p>panel</p>;
}
export default function Index() {
  const [open, setOpen] = useState(false);
  return <main><button onClick={() => setOpen(!open)}>t</button>{open ? <Panel /> : <span>closed</span>}</main>;
}`)
	if !strings.Contains(js, `[["3","c",0]]`) {
		t.Fatalf("a component in the initially hidden branch must be declared hidden:\n%s", js)
	}
}

func TestBootstrapReportsBranchAndSlotChanges(t *testing.T) {
	for _, frag := range []string{
		// The owner-tree hooks (boundary.ts) bracket the effects.ts activity calls.
		"var ca=typeof krateCondActivity==='function'?krateCondActivity:null;if(window.__krateCondPre)window.__krateCondPre(id,s);if(ca&&!s)ca(id,'a');krateCondSwap(n,s,t);if(ca&&s)ca(id,'c');if(window.__krateCondPost)window.__krateCondPost(id,s);",
		"if(__first){__first=false;if(typeof krateSlotReplaced==='function')krateSlotReplaced(id);}",
	} {
		if !strings.Contains(HydrationBootstrapJS, frag) {
			t.Fatalf("bootstrap missing %q", frag)
		}
	}
	// The dependency helpers live in the runtime (packages/runtime/src/effects.ts).
	if strings.Contains(HydrationBootstrapJS, "window.createEffectDeps") || strings.Contains(HydrationBootstrapJS, "window.createMemoDeps") {
		t.Fatal("deps helpers must not be redefined by the bootstrap")
	}
	if !strings.Contains(CompatBootstrapJS, "createEffectDeps(function(){return ref?krateSetRef(ref,create()):void 0;}") {
		t.Fatal("useImperativeHandle must attach in the layout phase and detach on cleanup")
	}
}

func TestLazyUseStateInitializerRunsOnce(t *testing.T) {
	res, js := fullPipelineWithReact(t, `import { useState } from 'react';
export default function Index() {
  const [c, setC] = useState(() => 2 + 3);
  return <button onClick={() => setC((x) => x + 1)}>{c}</button>;
}`)
	if !strings.Contains(res.HTML, ">5<") || !strings.Contains(js, "createSignal(5)") {
		t.Fatalf("lazy initializer should be called, not stored:\n%s\n%s", res.HTML, js)
	}
}
