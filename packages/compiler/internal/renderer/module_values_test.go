package renderer

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// buildWithData builds page with the constants of a second module (dataSrc)
// available the way the build merges imported modules.
func buildWithData(t *testing.T, dataSrc, pageSrc string, params map[string]string) (*irtree.ComponentTree, *EmitResult, string) {
	t.Helper()
	opts := irtree.BuildOptions{PageParams: params}
	if dataSrc != "" {
		data := parseProg(t, dataSrc)
		bundler.RewriteReact(data)
		opts.ModuleConsts, opts.ModuleConstInits = irtree.CollectModuleConstants(data)
	}
	prog := parseProg(t, pageSrc)
	bundler.RewriteReact(prog)
	ann := annotator.Annotate(prog, &config.Config{}, "page.tsx", pageSrc)
	tree := irtree.BuildWithOptions(prog, ann, opts)
	result := NewEmitter().Emit(tree)
	EmitMeta(tree, result)
	return tree, result, GenerateNewHydrationJS(result)
}

const iconData = `export const items = [
  { id: "a", name: "Alpha", icon: <svg viewBox="0 0 1 1" /> },
  { id: "b", name: "Beta", icon: <svg viewBox="0 0 2 2" /> },
];`

func TestModuleDataWithJSXKeepsSiblingFields(t *testing.T) {
	page := `import { items } from "./data";
export default function Page({ params }) {
  const item = items.find((i) => i.id === params.id);
  return <main><title>{items.find((i) => i.id === params.id).name}</title><h1>{item.name}</h1>{item.icon}</main>;
}`
	for _, tc := range []struct{ id, name, box string }{{"a", "Alpha", "0 0 1 1"}, {"b", "Beta", "0 0 2 2"}} {
		tree, result, _ := buildWithData(t, iconData, page, map[string]string{"id": tc.id})
		for _, w := range tree.Warnings {
			if strings.Contains(w.Message, "cannot be evaluated at build time") {
				t.Errorf("%s: unexpected warning %s", tc.id, w.Message)
			}
		}
		all := result.HeadHTML + result.HTML
		mustContain(t, tc.id, all, "<title>"+tc.name+"</title>", "<h1>"+tc.name+"</h1>", `<svg viewBox="`+tc.box+`"></svg>`)
	}
}

func TestModuleDataWithJSXRendersInStaticList(t *testing.T) {
	page := `import { items } from "./data";
export default function Page() {
  return <ul>{items.map((it) => <li>{it.name}{it.icon}</li>)}</ul>;
}`
	_, result, _ := buildWithData(t, iconData, page, nil)
	mustContain(t, "list", result.HTML,
		`<li>Alpha<svg viewBox="0 0 1 1"></svg></li>`, `<li>Beta<svg viewBox="0 0 2 2"></svg></li>`)
}

func TestModuleDataWithJSXStatelessComponentProp(t *testing.T) {
	page := `import { items } from "./data";
function Rows(props) { return <ul>{props.rows.map((r) => <li>{r.name}{r.icon}</li>)}</ul>; }
export default function Page() { return <Rows rows={items} />; }`
	_, result, _ := buildWithData(t, iconData, page, nil)
	mustContain(t, "stateless", result.HTML, `<li>Alpha<svg viewBox="0 0 1 1"></svg></li>`)
}

func TestModuleDataWithJSXIslandRendersInitialHTML(t *testing.T) {
	page := `import { useState } from "react";
import { items } from "./data";
function Picker(props) {
  const [n, setN] = useState(0);
  return <div><button onClick={() => setN(props.rows.length + n)}>{n}</button><ul>{props.rows.map((r) => <li>{r.name}</li>)}</ul></div>;
}
export default function Page() { return <Picker rows={items} />; }`
	tree, result, js := buildWithData(t, iconData, page, nil)
	if len(result.Errors) > 0 || len(tree.Errors) > 0 {
		t.Fatalf("errors: %v %v", result.Errors, tree.Errors)
	}
	mustContain(t, "island", result.HTML, `<li>Alpha</li>`, `<li>Beta</li>`)
	// The client keeps the original expression, JSX included.
	mustContain(t, "island js", js, `name:'Alpha'`, `icon:h('svg',{viewBox:'0 0 1 1'})`)
	mustNotContain(t, "island js", js, "rows:undefined", "icon:undefined")
}

const linkData = `export const links = [{ href: "/a", label: "A" }, { href: "/b", label: "B" }, { href: "/c", label: "C" }];`

func TestObjectIdentityInListRows(t *testing.T) {
	cases := []struct {
		name string
		page string
		want []string
	}{
		{
			name: "stateless page, module data",
			page: `import { links } from "./data";
export default function Page() {
  const current = links.find((l) => l.href === "/b");
  return <nav>{links.map((l) => <a class={l === current ? "on" : "off"}>{l.label}</a>)}</nav>;
}`,
			want: []string{`<a class="off">A</a>`, `<a class="on">B</a>`, `<a class="off">C</a>`},
		},
		{
			name: "component with state",
			page: `import { useState } from "react";
import { links } from "./data";
export default function Page() {
  const [n, setN] = useState(0);
  const current = links.find((l) => l.href === "/b");
  return <nav><button onClick={() => setN(n + 1)}>{n}</button>{links.map((l) => <a class={l === current ? "on" : "off"}>{l.label}</a>)}</nav>;
}`,
			want: []string{`class="off"`, `class="on"`},
		},
		{
			name: "local array and find",
			page: `import { useState } from "react";
export default function Page() {
  const [n, setN] = useState(0);
  const mine = [{ id: 1, t: "X" }, { id: 2, t: "Y" }];
  const cur = mine.find((m) => m.id === 2);
  return <p><button onClick={() => setN(n + 1)}>{n}</button>{mine.map((m) => <b class={m === cur ? "on" : "off"}>{m.t}</b>)}</p>;
}`,
			want: []string{`<b class="off">X</b>`, `<b class="on">Y</b>`},
		},
		{
			name: "find inside the loop",
			page: `import { links } from "./data";
export default function Page() {
  return <nav>{links.map((l) => { const last = links.find((x) => x.href === "/c"); return <i class={l === last ? "on" : "off"}>{l.label}</i>; })}</nav>;
}`,
			want: []string{`<i class="off">A</i>`, `<i class="off">B</i>`, `<i class="on">C</i>`},
		},
		{
			name: "array signal read twice",
			page: `import { useState } from "react";
export default function Page() {
  const [list] = useState([{ id: 1, t: "One" }, { id: 2, t: "Two" }]);
  const [n, setN] = useState(0);
  const cur = list.find((i) => i.id === 2);
  return <p><button onClick={() => setN(n + 1)}>{n}</button>{list.map((i) => <b class={i === cur ? "on" : "off"}>{i.t}</b>)}</p>;
}`,
			want: []string{`<b data-k`, `class="off"`, `>One</b>`, `class="on"`, `>Two</b>`},
		},
		{
			name: "passed to a stateless component",
			page: `import { links } from "./data";
function Menu(props) { return <nav>{props.items.map((l) => <a class={l === props.current ? "on" : "off"}>{l.label}</a>)}</nav>; }
export default function Page() {
  const current = links.find((l) => l.href === "/b");
  return <Menu items={links} current={current} />;
}`,
			want: []string{`<a class="off">A</a>`, `<a class="on">B</a>`, `<a class="off">C</a>`},
		},
		{
			name: "passed to a component with state",
			page: `import { useState } from "react";
import { links } from "./data";
function Menu(props) {
  const [n, setN] = useState(0);
  return <nav><button onClick={() => setN(n + 1)}>{n}</button>{props.items.map((l) => <a class={l === props.current ? "on" : "off"}>{l.label}</a>)}</nav>;
}
export default function Page() {
  const current = links.find((l) => l.href === "/b");
  return <Menu items={links} current={current} />;
}`,
			want: []string{`class="off"`, `>A</a>`, `class="on"`, `>B</a>`, `>C</a>`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, result, _ := buildWithData(t, linkData, tc.page, nil)
			if strings.Count(result.HTML, `class="on"`) != 1 {
				t.Errorf("want exactly one row marked on:\n%s", result.HTML)
			}
			// Each fragment appears in order.
			rest := result.HTML
			for _, w := range tc.want {
				i := strings.Index(rest, w)
				if i < 0 {
					t.Fatalf("missing %q (in order) in:\n%s", w, result.HTML)
				}
				rest = rest[i+len(w):]
			}
		})
	}
}
