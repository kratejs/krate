package renderer

import (
	"strings"
	"testing"
)

// TestClientTemplateTOCMapSSR verifies a client component whose list items use
// template literals, numeric comparisons in ternaries, and attribute
// expressions (the docs TOCNav shape) SSR their real values.
func TestClientTemplateTOCMapSSR(t *testing.T) {
	src := `
import { onMount } from "@krate/runtime";
interface Toc { title: string; id: string; depth: number }
function TocNav(props: { items: Toc[] }) {
  onMount(function () {});
  return (
    <nav class="toc">
      {props.items.map((item) => (
        <a class={"toc-link" + (item.depth <= 2 ? " toc-h2" : " toc-h3")} href={"#" + item.id} data-depth={item.depth}>{item.title}</a>
      ))}
    </nav>
  );
}
export default function Page() {
  return <TocNav items={[{ title: "Getting Started", id: "getting-started", depth: 2 }, { title: "Prereq", id: "prereq", depth: 3 }]} />;
}
`
	result, _ := fullPipeline(t, src)
	for _, want := range []string{"#getting-started", "#prereq", ">Getting Started</a>", ">Prereq</a>", `toc-link toc-h2`, `toc-link toc-h3`} {
		if !strings.Contains(result.HTML, want) {
			t.Errorf("missing %q in SSR:\n%s", want, result.HTML)
		}
	}
}
