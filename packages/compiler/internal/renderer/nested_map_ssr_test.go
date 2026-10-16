package renderer

import (
	"strings"
	"testing"
)

// TestClientNestedMapComponentSSR mirrors the docs sidebar structure: a
// client component maps over a prop array of objects, and each item renders a
// nested (recursive, pure-prop) component that branches on member values
// (children presence / url). All of it must SSR as concrete markup.
func TestClientNestedMapComponentSSR(t *testing.T) {
	src := `
import { onMount } from "@krate/runtime";
interface Item { title: string; url?: string; indexURL?: string; children?: Item[] }
function SidebarSection(props: { item: Item }) {
  var item = props.item;
  const hasChildren = item.children && item.children.length > 0;
  if (!hasChildren && item.url) {
    return <a class="sidebar-link" href={item.url}>{item.title}</a>;
  }
  return (
    <div class="section">
      <span>{item.title}</span>
      {item.indexURL && <a class="idx" href={item.indexURL}>idx</a>}
      {item.children && item.children.map((child) => (
        <SidebarSection item={child} />
      ))}
    </div>
  );
}
function Nav(props: { items: Item[] }) {
  onMount(function () {});
  return <div class="nav">{props.items.map((item) => <SidebarSection item={item} />)}</div>;
}
export default function Page() {
  return <Nav items={[{ title: "RootA", url: "/a/" }, { title: "RootB", url: "/b/" }, { title: "Section", indexURL: "/s/", children: [{ title: "ChildC", url: "/s/c/" }, { title: "ChildD", url: "/s/d/" }] }]} />;
}
`
	result, _ := fullPipeline(t, src)
	for _, want := range []string{`class="sidebar-link"`, ">RootA</a>", ">RootB</a>", ">ChildC</a>", ">ChildD</a>", `class="idx"`, "/s/c/", "/s/d/"} {
		if !strings.Contains(result.HTML, want) {
			t.Errorf("missing %q in SSR:\n%s", want, result.HTML)
		}
	}
}
