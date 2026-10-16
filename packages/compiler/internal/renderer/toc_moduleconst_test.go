package renderer

import (
	"strings"
	"testing"
)

// TestClientModuleConstArrayPassedDirect verifies a module-level const array
// passed as a direct prop to a client map component resolves SSR items.
func TestClientModuleConstArrayPassedDirect(t *testing.T) {
	src := `
import { onMount } from "@krate/runtime";
const tocItems = [
  { title: "Prereq", id: "prereq" },
  { title: "Install", id: "install" }
];
function TocNav(props: { items: any[] }) {
  var items = props.items;
  onMount(function () {});
  return (
    <nav class="toc">
      {items.map((item) => (
        <a href={"#" + item.id}>{item.title}</a>
      ))}
    </nav>
  );
}
export default function Page() {
  return <TocNav items={tocItems} />;
}
`
	result, _ := fullPipeline(t, src)
	for _, want := range []string{"#prereq", ">Prereq</a>", "#install", ">Install</a>"} {
		if !strings.Contains(result.HTML, want) {
			t.Errorf("missing %q in SSR:\n%s", want, result.HTML)
		}
	}
}
