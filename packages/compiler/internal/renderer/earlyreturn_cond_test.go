package renderer

import (
	"strings"
	"testing"
)

// Early returns are lowered to ConditionalSlots, including at a component's
// root; they must use the same comment-marker emission.
func TestEarlyReturnConditionalAtRoot(t *testing.T) {
	res, js := fullPipeline(t, `function Panel() {
  const [open, setOpen] = createSignal(false);
  if (!open()) return <li onClick={() => setOpen(true)}>closed</li>;
  return <li onClick={() => setOpen(false)}>open</li>;
}
export default function Page() {
  return <ul><Panel /></ul>;
}`)
	html := res.HTML
	if strings.Contains(html, "data-krate-cond-w") {
		t.Fatalf("wrapper divs emitted:\n%s", html)
	}
	if !strings.Contains(html, "<ul><!--k:") || !strings.Contains(html, ">closed</li><!--/k:") {
		t.Errorf("root conditional should render the active branch between markers inside <ul>:\n%s", html)
	}
	if !strings.Contains(html, ">open</li></template>") {
		t.Errorf("inactive early-return branch should be in a hoisted template:\n%s", html)
	}
	if !strings.Contains(js, "kbindCond(") {
		t.Errorf("expected kbindCond for the early-return conditional:\n%s", js)
	}
}

func TestEarlyReturnStaticGuardFolds(t *testing.T) {
	res, _ := fullPipeline(t, `function Toc(props) {
  if (!props.items || props.items.length === 0) return <span></span>;
  return <nav>{props.items.map((i) => <a>{i}</a>)}</nav>;
}
export default function Page() {
  return <div><Toc items={[]} /></div>;
}`)
	if strings.Contains(res.HTML, "<template") || !strings.Contains(res.HTML, "<span></span>") {
		t.Errorf("statically known early return should fold without a template:\n%s", res.HTML)
	}
}
