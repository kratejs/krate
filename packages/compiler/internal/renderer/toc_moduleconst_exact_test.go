package renderer

import (
	"strings"
	"testing"
)

// TestClientTOCModuleConstExact mirrors the real docs TOCNav closely: module
// const array, client onMount closure reading props.items, local var items,
// template/ternary map body.
func TestClientTOCModuleConstExact(t *testing.T) {
	src := `
import { onMount } from "@krate/runtime";
interface Toc { title: string; id: string; depth: number }
const tocItems = [
  { title: "Prereq", id: "prereq", depth: 2 },
  { title: "Install", id: "install", depth: 2 },
  { title: "Deep", id: "deep", depth: 3 }
];
function TocNav(props: { items: Toc[] }) {
  var items = props.items;
  onMount(function () {
    var nav = document.querySelector(".toc");
    if (nav && items.length > 0) {
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
export default function Page() {
  return <TocNav items={tocItems} />;
}
`
	result, _ := fullPipeline(t, src)
	for _, want := range []string{"#prereq", ">Prereq</a>", "#install", "#deep", `toc-link toc-h2`, `toc-link toc-h3`} {
		if !strings.Contains(result.HTML, want) {
			t.Errorf("missing %q in SSR:\n%s", want, result.HTML)
		}
	}
}
