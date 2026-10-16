package renderer

import (
	"strings"
	"testing"
)

// TestClientComponentListSSR verifies that a client-tier component (one with
// lifecycle hooks) whose return JSX maps over a prop-driven array still renders
// its initial list content into the SSR HTML, rather than deferring everything
// to an empty hydration slot until JS runs.
func TestClientComponentListSSR(t *testing.T) {
	src := `
import { onMount } from "@krate/runtime";
interface Item { title: string }
function SidebarNav(props: { items: Item[] }) {
  onMount(function () {});
  return (
    <div class="sidebar-nav">
      {props.items.map((item) => (
        <a class="sidebar-link" href={item.title}>{item.title}</a>
      ))}
    </div>
  );
}
export default function Page() {
  return <SidebarNav items={[{ title: "Home" }, { title: "Docs" }]} />;
}
`
	result, _ := fullPipeline(t, src)
	if !strings.Contains(result.HTML, `class="sidebar-link"`) {
		t.Errorf("expected SSR list items for client component, got HTML:\n%s", result.HTML)
	}
	if !strings.Contains(result.HTML, ">Home</a>") || !strings.Contains(result.HTML, ">Docs</a>") {
		t.Errorf("expected both list items SSR'd, got:\n%s", result.HTML)
	}
	// The list must still be bound for client re-renders.
	if !strings.Contains(result.HTML, "<!--k:") {
		t.Errorf("expected hydration slot markers to remain, got:\n%s", result.HTML)
	}
}

// TestClientComponentLiteralListSSR guards the pre-existing literal-array list
// SSR path (arrays written inline in the map expression).
func TestClientComponentLiteralListSSR(t *testing.T) {
	src := `
import { onMount } from "@krate/runtime";
function Nav() {
  onMount(function () {});
  return (
    <div>
      {[{ id: "a" }, { id: "b" }].map((x) => (
        <span data-id={x.id}>{x.id}</span>
      ))}
    </div>
  );
}
export default function Page() {
  return <Nav />;
}
`
	result, _ := fullPipeline(t, src)
	if !strings.Contains(result.HTML, `data-id="a"`) || !strings.Contains(result.HTML, `data-id="b"`) {
		t.Errorf("expected literal-array items SSR'd, got:\n%s", result.HTML)
	}
}
