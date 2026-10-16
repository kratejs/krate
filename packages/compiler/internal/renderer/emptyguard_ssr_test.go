package renderer

import (
	"strings"
	"testing"
)

// TestClientEmptyGuardDoesNotAbortIIFE guards the hydration emission ordering
// for a client component with a top-level empty guard before its locals:
//
//	var items = props.items;
//	if (!items || items.length === 0) return <span />;
//	onMount(...);
//	return <nav>{items.map(...)}</nav>;
//
// The guard must NOT be re-emitted before `var items` in the hydration IIFE
// (it would read the hoisted-but-unassigned `items` === undefined, return early,
// and abort onMount/kbindContent registration). Locals must be declared before
// the effects so TOC scroll-spy etc. actually hydrate.
func TestClientEmptyGuardDoesNotAbortIIFE(t *testing.T) {
	src := `
import { onMount } from "@krate/runtime";
interface Toc { title: string; id: string; depth: number }
function TocNav(props: { items: Toc[] }) {
  var items = props.items;
  if (!items || items.length === 0) return <span />;
  onMount(function () {
    var nav = document.querySelector(".toc");
    if (nav) {
      var links = nav.querySelectorAll("a");
      for (var i = 0; i < links.length; i++) {}
    }
  });
  return (
    <nav class="toc">
      {items.map((item) => (
        <a class={"toc-link" + (item.depth <= 2 ? " toc-h2" : " toc-h3")} href={"#" + item.id} data-depth={item.depth}>{item.title}</a>
      ))}
    </nav>
  );
}
function DocsLayout(props: any) {
  const [x, setX] = createSignal(0);
  onMount(function () {});
  return (
    <div>
      <TocNav items={props.tocItems} />
    </div>
  );
}
export default function Page() {
  return <DocsLayout tocItems={[{ title: "Prereq", id: "prereq", depth: 2 }, { title: "Deep", id: "deep", depth: 3 }]} />;
}
`
	result, hjs := fullPipeline(t, src)
	for _, want := range []string{`href="#prereq"`, ">Prereq</a>", `href="#deep"`, ">Deep</a>"} {
		if !strings.Contains(result.HTML, want) {
			t.Errorf("SSR missing %q:\n%s", want, result.HTML)
		}
	}
	// The hydration bundle must declare the items local before onMount and must
	// NOT contain the bare early guard returning h('span') before the local.
	if i := strings.Index(hjs, "var items="); i < 0 {
		t.Errorf("hydration missing local `var items=`:\n%s", hjs)
	} else if !strings.Contains(hjs[i:], "onMount") {
		t.Errorf("onMount must follow the local declaration:\n%s", hjs)
	}
	if strings.Contains(hjs, "return h('span',null);\nvar items=") ||
		strings.Contains(hjs, "return h(\"span\",null);\nvar items=") {
		t.Errorf("hydration still emits the empty-guard return before the local declaration:\n%s", hjs)
	}
	if !strings.Contains(hjs, "kbindContent") {
		t.Errorf("hydration must retain the list content binding:\n%s", hjs)
	}
}
