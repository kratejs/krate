package build

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/renderer"
)

// devInspectProject has one component of every tier.
var devInspectProject = map[string]string{
	"src/pages/_layout.tsx": `export default function Layout({ children }: any) {
  return <main class="shell">{children}</main>;
}
`,
	"src/pages/index.tsx": `// @server
import Header from "../components/Header.static";
import Byline from "../components/Byline.server";
import Counter from "../components/Counter";
import { Card } from "../components/Card";
export default function Home() {
  return (
    <section>
      <Header title="Hi" />
      <Byline who="me" />
      <Card label="c"><Counter start={2} /></Card>
      <ul>{["a", "b"].map((x) => <li>{x}</li>)}</ul>
    </section>
  );
}
`,
	"src/components/Header.static.tsx": `export default function Header({ title }: any) { return <header><h1>{title}</h1></header>; }
`,
	"src/components/Byline.server.tsx": `export default function Byline({ who }: any) { return <p class="by">by {who}</p>; }
`,
	"src/components/Card.tsx": `import { Inner } from "./Inner";
export function Card({ label, children }: any) {
  return <div class="card"><Inner text={label} />{children}</div>;
}
`,
	"src/components/Inner.tsx": `export function Inner({ text }: any) { return <span class="inner">{text}</span>; }
`,
	"src/components/Counter.tsx": `import { createSignal } from "@krate/runtime";
export default function Counter({ start }: any) {
  const [count, setCount] = createSignal(start);
  const [open, setOpen] = createSignal(false);
  return (
    <div class="counter">
      <button onClick={() => setCount(count() + 1)}>Count {count()}</button>
      {open() ? <em>open</em> : <b>closed</b>}
      <button onClick={() => setOpen(!open())}>toggle</button>
    </div>
  );
}
`,
	"src/pages/live.tsx": `// @server
import { Live } from "../Live.runtime";
export default function P() { return <Suspense fallback={<span>loading</span>}><Live name="world" /></Suspense>; }
`,
	"src/Live.runtime.tsx": `export const Live = ({ name }: any) => <p>live {name}</p>;
`,
}

func buildInspectProject(t *testing.T, dev bool) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeProject(t, root, devInspectProject)
	linkRuntimeDist(t, root)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	b.DevMode = dev
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	return root, cfg.OutDir
}

var devKDAttrRe = regexp.MustCompile(`data-kd="?([^">]*)"?`)

// readDevTable returns the inspector table of a built page and the n of
// every element marker.
func readDevTable(t *testing.T, html string) ([]DevComponentEntry, map[int]bool) {
	t.Helper()
	m := regexp.MustCompile(`(?s)<script type="?application/json"? id="?` + devComponentsID + `"?>(.*?)</script>`).FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("no component table in:\n%s", html)
	}
	var rows []DevComponentEntry
	if err := json.Unmarshal([]byte(m[1]), &rows); err != nil {
		t.Fatalf("table %s: %v", m[1], err)
	}
	marked := map[int]bool{}
	markup := regexp.MustCompile(`(?s)<script\b.*?</script>`).ReplaceAllString(html, "")
	for _, a := range devKDAttrRe.FindAllStringSubmatch(markup, -1) {
		for _, f := range strings.Fields(a[1]) {
			n, err := strconv.Atoi(f)
			if err != nil {
				t.Errorf("data-kd holds %q, want numbers", a[1])
				continue
			}
			marked[n] = true
		}
	}
	return rows, marked
}

func devRowsByName(rows []DevComponentEntry) map[string]DevComponentEntry {
	out := map[string]DevComponentEntry{}
	for _, r := range rows {
		out[r.Name] = r
	}
	return out
}

func TestDevComponentTable(t *testing.T) {
	t.Parallel()
	_, out := buildInspectProject(t, true)
	html := readBuilt(t, filepath.Join(out, "index.html"))
	if strings.Contains(html, "<!--"+"kd:") {
		t.Errorf("marker comments left in the document")
	}
	rows, marked := readDevTable(t, html)
	by := devRowsByName(rows)
	want := map[string][3]string{ // tier, file, parent name
		"Layout":  {"static", "src/pages/_layout.tsx", ""},
		"Home":    {"server", "src/pages/index.tsx", "Layout"},
		"Header":  {"static", "src/components/Header.static.tsx", "Home"},
		"Byline":  {"server", "src/components/Byline.server.tsx", "Home"},
		"Card":    {"client", "src/components/Card.tsx", "Home"},
		"Inner":   {"static", "src/components/Inner.tsx", "Card"},
		"Counter": {"client", "src/components/Counter.tsx", "Card"},
	}
	if len(rows) != len(want) {
		t.Errorf("rows = %+v", rows)
	}
	for name, w := range want {
		r, ok := by[name]
		if !ok {
			t.Errorf("no row for %s", name)
			continue
		}
		parent := ""
		if r.Parent >= 0 {
			parent = rows[r.Parent].Name
		}
		if r.Tier != w[0] || r.File != w[1] || parent != w[2] || r.Line == 0 {
			t.Errorf("%s = %+v (parent %q), want %v", name, r, parent, w)
		}
		if !marked[r.N] {
			t.Errorf("%s (n=%d) marks no element", name, r.N)
		}
	}
	for i, r := range rows {
		if r.N != i {
			t.Errorf("row %d has n %d", i, r.N)
		}
	}
	// Both component roots of the card's element: the card comes first.
	if !strings.Contains(html, `class="card" data-kd="`+strconv.Itoa(by["Card"].N)+`"`) && !strings.Contains(html, `class=card data-kd=`+strconv.Itoa(by["Card"].N)) {
		t.Errorf("card root not marked:\n%s", html)
	}

	// The page script registers the client component's signals under the
	// key the table gives it.
	js := readBuilt(t, mustGlobOne(t, filepath.Join(out, "index.*.js")))
	call := `__krate_dev_reg(` + strconv.Quote(by["Counter"].Key) + `,__kdt,[["count",count],["open",open]]);`
	if !strings.Contains(js, call) {
		t.Errorf("page script lacks %s:\n%s", call, js)
	}

	// A runtime region has no element: its row names the region.
	live := readBuilt(t, filepath.Join(out, "live", "index.html"))
	rows, _ = readDevTable(t, live)
	r := devRowsByName(rows)["Live"]
	if r.Tier != "runtime" || r.Region != "1-1" || r.File != "src/Live.runtime.tsx" || !strings.Contains(live, "<!--suspense:1-1-->") {
		t.Errorf("runtime row = %+v", r)
	}
}

// Production builds carry none of the inspector's markers or code.
func TestDevComponentsAbsentInProduction(t *testing.T) {
	t.Parallel()
	_, out := buildInspectProject(t, false)
	files, _ := filepath.Glob(filepath.Join(out, "*.html"))
	more, _ := filepath.Glob(filepath.Join(out, "*", "*.html"))
	js, _ := filepath.Glob(filepath.Join(out, "*.js"))
	chunks, _ := filepath.Glob(filepath.Join(out, "chunks", "*.js"))
	all := append(append(append(files, more...), js...), chunks...)
	if len(files) == 0 || len(js) == 0 {
		t.Fatalf("nothing built: %v", all)
	}
	for _, f := range all {
		data := readBuilt(t, f)
		for _, bad := range []string{"data-kd", devComponentsID, "__krate_dev_reg", "__kdt", "__krate_dev_signals", "<!--kd:"} {
			if strings.Contains(data, bad) {
				t.Errorf("%s contains %q", f, bad)
			}
		}
	}
}

// An external island keeps its exact placeholder until its server render
// replaced it, then gets its number.
func TestDevComponentIslands(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	b := New(root, cfg)
	b.DevMode = true
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	html := readBuilt(t, filepath.Join(cfg.OutDir, "index.html"))
	if !strings.Contains(html, "data-krate-island-ssr") || !strings.Contains(html, "badge") {
		t.Fatalf("the island was not server-rendered:\n%s", html)
	}
	rows, marked := readDevTable(t, html)
	badge := devRowsByName(rows)["Badge"]
	if badge.Tier != "island" || badge.Island == "" || !marked[badge.N] || badge.File != "src/pages/index.tsx" {
		t.Fatalf("island row = %+v in %+v", badge, rows)
	}
	el := regexp.MustCompile(`<div[^>]*data-krate-island="?` + regexp.QuoteMeta(badge.Island) + `"?[^>]*>`).FindString(html)
	if !strings.Contains(el, "data-kd") {
		t.Errorf("island element %s has no number", el)
	}
	if page := devRowsByName(rows)["Page"]; badge.Parent != page.N {
		t.Errorf("island parent = %d, want the page %d", badge.Parent, page.N)
	}

	// The route panel lists the island's chunk under the same id.
	info, ok := devRouteInfo(root, cfg, cfg.OutDir, "/")
	if !ok || info.Sizes == nil || info.Sizes.GzipIslands == 0 {
		t.Fatalf("route info = %+v", info)
	}
	found := false
	for _, isl := range info.Islands {
		found = found || (isl.ID == badge.Island && isl.Gzip > 0 && strings.HasPrefix(isl.Chunk, "chunks/island_"))
	}
	if !found {
		t.Errorf("islands = %+v, want %s", info.Islands, badge.Island)
	}
}

func TestFinishDevComponents(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enc := func(key, name, parent string) string {
		return renderer.EncodeDevComponent(renderer.DevComponent{Key: key, Name: name, Tier: "static", File: filepath.Join(root, "src", name+".tsx"), Line: 1, Parent: parent})
	}
	doc := `<html><body><div id=root><main data-kd="` + enc("a", "A", "") + `">` +
		`<p data-kd="` + enc("b", "B", "a") + ` ` + enc("c", "C", "b") + `">x</p>` +
		`<!--kd:` + renderer.EncodeDevComponent(renderer.DevComponent{Key: "r", Name: "R", Tier: "runtime", Region: "region-1"}) + `--><!--region:region-1--><!--/region:region-1-->` +
		`<script>var s = "<p data-kd=x>";</script>` +
		`<div data-krate-island="i1" style="display:contents"></div>` +
		`<!--krate-island:{"m":"src/Widget.tsx","e":"default","s":"/c.js","p":{}}--><b>w</b><!--/krate-island-->` +
		`<span data-kd="` + enc("a", "A", "") + `">second root</span>` +
		`</main></div>` +
		`<!--kd:` + renderer.EncodeDevComponent(renderer.DevComponent{Key: "island:i1", Name: "Chart", Tier: "island", Island: "i1"}) + `-->` +
		`</body></html>`
	got := finishDevComponents(doc, root)
	rows, marked := readDevTable(t, got)
	names := []string{}
	for _, r := range rows {
		names = append(names, r.Name+":"+strconv.Itoa(r.Parent))
	}
	if strings.Join(names, ",") != "A:-1,B:0,C:1,R:0,Chart:0,Widget:0" {
		t.Errorf("rows = %v", names)
	}
	for n := range []int{0, 1, 2, 4} {
		if !marked[[]int{0, 1, 2, 4}[n]] {
			t.Errorf("n %d not marked", n)
		}
	}
	for _, want := range []string{`<p data-kd="1 2">`, `<span data-kd="0">`, `<div data-krate-island="i1" style="display:contents" data-kd="4">`, `var s = "<p data-kd=x>";`, `<!--region:region-1-->`, `<!--krate-island:`} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<!--kd:") {
		t.Errorf("markers left:\n%s", got)
	}
	if rows[0].File != "src/A.tsx" || rows[5].Boundary == nil || *rows[5].Boundary != 0 || rows[3].Region != "region-1" || rows[4].Island != "i1" {
		t.Errorf("rows = %+v", rows)
	}
}

// Hydration of a dev build still works with the markers in place, and the
// page registers its signals and hydration times.
func TestDevInspectorHydratesHeadless(t *testing.T) {
	t.Parallel()
	page := `
		import { createSignal } from '@krate/runtime';
		function Panel({ title }: any) { return <section id="panel"><h2>{title}</h2></section>; }
		function Counter() {
			const [n, setN] = createSignal(0);
			return <div id="counter"><button id="inc" onClick={() => setN(n() + 1)}>inc</button><span id="count">{n()}</span></div>;
		}
		export default function Page() {
			const [open, setOpen] = createSignal(false);
			const [items, setItems] = createSignal(['a', 'b']);
			return (
				<div>
					<Panel title="hello" />
					<Counter />
					<button id="toggle" onClick={() => setOpen(!open())}>toggle</button>
					{open() ? <em id="on">on</em> : <b id="off">off</b>}
					<button id="add" onClick={() => setItems([...items(), 'c'])}>add</button>
					<ul>{items().map((x) => <li class="item">{x}</li>)}</ul>
				</div>
			);
		}
	`
	check := `
const d = win.document;
if (!d.querySelector('#panel[data-kd]')) fail('panel root is not marked: ' + d.body.innerHTML);
const tick = () => new Promise((resolve) => setTimeout(resolve, 50));
d.querySelector('#inc').click();
d.querySelector('#inc').click();
await tick();
if (d.querySelector('#count').textContent !== '2') fail('count = ' + d.querySelector('#count').textContent);
d.querySelector('#toggle').click();
await tick();
if (!d.querySelector('#on') || d.querySelector('#off')) fail('conditional did not swap: ' + d.body.innerHTML);
d.querySelector('#toggle').click();
await tick();
if (d.querySelector('#on') || !d.querySelector('#off')) fail('conditional did not swap back');
d.querySelector('#add').click();
await tick();
const items = [...d.querySelectorAll('li.item')].map((e) => e.textContent).join(',');
if (items !== 'a,b,c') fail('list = ' + items);
const sigs = win.__krate_dev_signals || [];
const count = sigs.find((s) => s.name === 'n');
if (!count || count.get() !== 2) fail('signals = ' + JSON.stringify(sigs.map((s) => s.name)));
const h = win.__krate_dev_hydration || {};
if (!Object.keys(h).includes(count.c) || typeof h[count.c] !== 'number') fail('hydration = ' + JSON.stringify(h));
const hs = win.__krate_dev_hydration_start || {};
if (typeof hs[count.c] !== 'number' || hs[count.c] < 0) fail('hydration start = ' + JSON.stringify(hs));
const table = JSON.parse(d.getElementById('__krate_dev_components').textContent);
if (!table.some((r) => r.key === count.c && r.tier === 'client')) fail('table = ' + JSON.stringify(table));
`
	runHeadlessMode(t, page, check, true)
}

// A server component that renders the children it is given is in the table
// with its root marked, and so is a component its children render.
func TestDevComponentTableServerChildren(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeProject(t, root, map[string]string{
		"src/pages/index.tsx": `// @server
import Panel from "../components/Panel.server";
import Note from "../components/Note.static";
export default function Home() {
  return <div><Panel><p class="kid">static child</p></Panel><Panel><Note text="n" /></Panel></div>;
}
`,
		"src/components/Panel.server.tsx": `export default function Panel(props: any) {
  return <aside class="panel">{props.children}</aside>;
}
`,
		"src/components/Note.static.tsx": `export default function Note({ text }: any) { return <em class="note">{text}</em>; }
`,
	})
	linkRuntimeDist(t, root)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	b.DevMode = true
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	html := readBuilt(t, filepath.Join(cfg.OutDir, "index.html"))
	rows, marked := readDevTable(t, html)
	for _, r := range rows {
		if r.Region == "" && r.Island == "" && !marked[r.N] {
			t.Errorf("row %+v marks no element", r)
		}
	}
	panels := 0
	for _, r := range rows {
		if r.Name == "Panel" {
			panels++
			if r.Tier != "server" {
				t.Errorf("Panel row = %+v", r)
			}
		}
	}
	if panels != 2 || !strings.Contains(html, "static child") {
		t.Errorf("want two Panel rows and the rendered child; rows %+v\n%s", rows, html)
	}
	// A component element among a server component's children is dropped
	// from the output today (also on origin/dev); once it renders it needs
	// its own row.
	if strings.Contains(html, `class="note"`) || strings.Contains(html, `class=note`) {
		if devRowsByName(rows)["Note"].Name == "" {
			t.Errorf("Note renders but has no row: %+v", rows)
		}
	}
}
