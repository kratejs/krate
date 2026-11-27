package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Module constants only build-time rendered markup reads stay out of the page
// script, whether the component holding them has a CSS scope, an island, or
// receives an island as children. A constant client code reads still ships.
func TestHydrationScriptModuleData(t *testing.T) {
	t.Parallel()
	const rows = `const ROWS = Array.from({ length: 4 }, (_, i) => ({ name: "row" + i, text: "MARK%s" + i + " filler".repeat(40) }));
`
	counter := `export default function Counter(props) {
  const [n, setN] = createSignal(0);
  return <div><button onClick={() => setN(n() + 1)}>{n()}</button>{props.children}</div>;
}
`
	list := `{ROWS.map((r) => <li><b>{r.name}</b><pre>{r.text}</pre></li>)}`
	files := map[string]string{
		"src/components/counter.tsx": counter,
		// a: a CSS scope around a static list
		"src/pages/a.tsx": strings.Replace(rows, "%s", "A", 1) + `export default function A() {
  const t = createCSSChoice("x", ["x", "y"]);
  return <main><button {...t.trigger("x")}>X</button><div {...t.panel("x")}><ul>` + list + `</ul></div></main>;
}
`,
		// b: an island next to a static list, with the data in the same module
		"src/pages/b.tsx": `import Counter from "../components/counter";
` + strings.Replace(rows, "%s", "B", 1) + `function Rows(props) { return <ul>` + list + `</ul>; }
export default function B() { return <main><Counter /><Rows slug="b" /></main>; }
`,
		// c: an island passed as children of a wrapper that renders the data
		"src/pages/c.tsx": `import Counter from "../components/counter";
` + strings.Replace(rows, "%s", "C", 1) + `function Wrap(props) { return <section><ul>` + list + `</ul>{props.children}</section>; }
export default function C() { return <main><Wrap><Counter /></Wrap></main>; }
`,
		// d: an island that reads the constant at run time keeps it
		"src/pages/d.tsx": strings.Replace(rows, "%s", "D", 1) + `export default function D() {
  const [i, setI] = createSignal(0);
  return <main><button onClick={() => setI((i() + 1) % 4)}>next</button><p>{ROWS[i()].text}</p></main>;
}
`,
	}
	out, _ := buildLoopSite(t, files)
	scripts := func(page string) string {
		var all strings.Builder
		_ = filepath.Walk(filepath.Join(out, page), func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(p, ".js") {
				b, _ := os.ReadFile(p)
				all.Write(b)
			}
			return nil
		})
		return all.String()
	}
	for _, c := range []struct {
		page, mark string
		want       bool
	}{{"a", "MARKA", false}, {"b", "MARKB", false}, {"c", "MARKC", false}, {"d", "MARKD", true}} {
		js := scripts(c.page)
		if got := strings.Contains(js, c.mark); got != c.want {
			t.Errorf("page %s: %s in script = %v, want %v (script %d bytes)", c.page, c.mark, got, c.want, len(js))
		}
		html := readLoopOut(t, out, c.page+"/index.html")
		if c.page != "d" && !strings.Contains(html, c.mark) {
			t.Errorf("page %s: %s missing from the rendered HTML", c.page, c.mark)
		}
	}
}
