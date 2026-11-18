package build

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// sharedNav is a client component imported by several pages. Its items are
// build-time data that differs per page.
const sharedNav = `
import { createSignal } from '@krate/runtime';
export default function Nav(props: { items: { label: string; href: string }[] }) {
	const items = props.items;
	const [open, setOpen] = createSignal(false);
	const [picked, setPicked] = createSignal('');
	return (
		<nav>
			<button onClick={() => setOpen(!open())}>{open() ? 'Close' : 'Menu'}</button>
			<button onClick={() => setPicked(items[items.length - 1].label)}>last: {picked()}</button>
			<ul data-count={items.length}>{items.map((it) => <li><a href={it.href}>{it.label}</a></li>)}</ul>
		</nav>
	);
}
`

func sharedNavPage(labels ...string) string {
	var items []string
	for _, l := range labels {
		items = append(items, `{ label: "`+l+`", href: "/`+strings.ToLower(l)+`/" }`)
	}
	return `
import Nav from '../components/Nav';
export default function Page() {
	return <main><Nav items={[` + strings.Join(items, ", ") + `]} /><p>page</p></main>;
}
`
}

type sharedSite struct {
	cfg *config.Config
}

func buildSharedSite(t *testing.T, root string, files map[string]string, minify bool) *sharedSite {
	t.Helper()
	provideRuntime(t, root)
	for p, c := range files {
		writeFileRel(t, root, p, c)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = minify
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	return &sharedSite{cfg: cfg}
}

func (s *sharedSite) read(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.cfg.OutDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// chunks returns the shared chunk files, relative to outDir.
func (s *sharedSite) chunks(t *testing.T) []string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(s.cfg.OutDir, "chunks", "c", "*.js"))
	var out []string
	for _, f := range files {
		rel, _ := filepath.Rel(s.cfg.OutDir, f)
		out = append(out, filepath.ToSlash(rel))
	}
	sort.Strings(out)
	return out
}

var pageScriptRe = regexp.MustCompile(`<script src="?/([^" >]+)"?>`)

// scripts lists the script srcs of a page in document order.
func (s *sharedSite) scripts(t *testing.T, page string) []string {
	t.Helper()
	var out []string
	for _, m := range pageScriptRe.FindAllStringSubmatch(s.read(t, page), -1) {
		out = append(out, m[1])
	}
	return out
}

func (s *sharedSite) pageJS(t *testing.T, page string) string {
	t.Helper()
	for _, src := range s.scripts(t, page) {
		if !strings.HasPrefix(src, "chunks/") {
			return s.read(t, src)
		}
	}
	t.Fatalf("%s loads no page script", page)
	return ""
}

func TestSharedChunksHoldImportedComponentsOnce(t *testing.T) {
	root := t.TempDir()
	site := buildSharedSite(t, root, map[string]string{
		"src/components/Nav.tsx": sharedNav,
		"src/pages/a.tsx":        sharedNavPage("Alpha", "Beta"),
		"src/pages/b.tsx":        sharedNavPage("Gamma"),
	}, false)

	chunks := site.chunks(t)
	if len(chunks) != 1 {
		t.Fatalf("want one shared chunk for Nav, got %v", chunks)
	}
	chunk := site.read(t, chunks[0])
	for _, page := range []string{"a/index.html", "b/index.html"} {
		scripts := site.scripts(t, page)
		if len(scripts) != 3 || !strings.HasPrefix(scripts[0], "chunks/runtime.") || scripts[1] != chunks[0] {
			t.Errorf("%s scripts = %v, want runtime, %s, page script", page, scripts, chunks[0])
		}
	}
	// The chunk holds the component's code, not the pages' data.
	for _, want := range []string{"createSignal(false)", "kbindHandler(", "'Close'"} {
		if !strings.Contains(chunk, want) {
			t.Errorf("shared chunk lacks %q:\n%s", want, chunk)
		}
	}
	for _, page := range []string{"Alpha", "Beta", "Gamma"} {
		if strings.Contains(chunk, page) {
			t.Errorf("shared chunk contains page data %q:\n%s", page, chunk)
		}
	}
	// Each page script calls the template with its own data and ids.
	a, b := site.pageJS(t, "a/index.html"), site.pageJS(t, "b/index.html")
	if !strings.Contains(a, "Alpha") || !strings.Contains(a, "Beta") || strings.Contains(a, "Gamma") {
		t.Errorf("page a script lacks its items:\n%s", a)
	}
	if !strings.Contains(b, "Gamma") {
		t.Errorf("page b script lacks its items:\n%s", b)
	}
	for name, js := range map[string]string{"a": a, "b": b} {
		if strings.Contains(js, "createSignal") || strings.Count(js, "__ku(") != 1 {
			t.Errorf("page %s script should only call the shared template:\n%s", name, js)
		}
	}
}

// Page data never reaches the shared chunk, so editing it keeps the chunk's
// name; only the page whose data changed gets a new page script.
func TestSharedChunkNameIgnoresPageData(t *testing.T) {
	build := func(labelsB ...string) (*sharedSite, string) {
		root := t.TempDir()
		site := buildSharedSite(t, root, map[string]string{
			"src/components/Nav.tsx": sharedNav,
			"src/pages/a.tsx":        sharedNavPage("Alpha", "Beta"),
			"src/pages/b.tsx":        sharedNavPage(labelsB...),
		}, true)
		return site, strings.Join(site.chunks(t), ",")
	}
	s1, c1 := build("Gamma")
	s2, c2 := build("Gamma", "Delta", "Epsilon")
	if c1 == "" || c1 != c2 {
		t.Fatalf("shared chunks changed with page data: %s vs %s", c1, c2)
	}
	if s1.scripts(t, "a/index.html")[2] != s2.scripts(t, "a/index.html")[2] {
		t.Error("page a's script changed although only page b's data did")
	}
	if s1.scripts(t, "b/index.html")[2] == s2.scripts(t, "b/index.html")[2] {
		t.Error("page b's script should change with its data")
	}
}

// Two instances on one page share the template and pass their own ids; the
// template of a component only one page imports goes into that page's
// script; a component defined in the page module stays inline.
func TestSharedChunkInstancesAndPacking(t *testing.T) {
	root := t.TempDir()
	site := buildSharedSite(t, root, map[string]string{
		"src/components/Nav.tsx": sharedNav,
		"src/components/Solo.tsx": `
import { createSignal } from '@krate/runtime';
export default function Solo() {
	const [n, setN] = createSignal(0);
	return <button onClick={() => setN(n() + 1)}>solo {n()}</button>;
}`,
		"src/pages/a.tsx": `
import Nav from '../components/Nav';
import Solo from '../components/Solo';
import { createSignal } from '@krate/runtime';
function Local() {
	const [on, setOn] = createSignal(false);
	return <button onClick={() => setOn(!on())}>{on() ? 'on' : 'off'}</button>;
}
export default function Page() {
	return <main><Nav items={[{ label: "One", href: "/1/" }]} /><Nav items={[{ label: "Two", href: "/2/" }]} /><Solo /><Local /></main>;
}`,
		"src/pages/b.tsx": sharedNavPage("Gamma"),
	}, false)

	a := site.pageJS(t, "a/index.html")
	calls := regexp.MustCompile(`__ku\("([0-9a-z]+)"`).FindAllStringSubmatch(a, -1)
	if len(calls) != 3 {
		t.Fatalf("want three template calls (two Nav, one Solo), got %d:\n%s", len(calls), a)
	}
	if calls[0][1] != calls[1][1] {
		t.Errorf("the two Nav instances call different templates: %s, %s", calls[0][1], calls[1][1])
	}
	if !strings.Contains(a, "'on'") || !strings.Contains(a, "createSignal(false)") {
		t.Errorf("the page-local component should stay in the page script:\n%s", a)
	}
	shared := site.chunks(t)
	if len(shared) != 1 {
		t.Fatalf("want one package chunk (Solo goes into page a's script), got %v", shared)
	}
	// Solo's template is registered by page a's script itself, ahead of the
	// call.
	if i, j := strings.Index(a, "createSignal(0)"), strings.Index(a, "__ku("); i < 0 || i > j {
		t.Errorf("Solo should be registered at the start of page a's script:\n%s", a)
	}
	for _, page := range []string{"a/index.html", "b/index.html"} {
		got := site.scripts(t, page)
		if len(got) != 3 || got[1] != shared[0] {
			t.Errorf("%s loads %v, want runtime, %s, page script", page, got, shared[0])
		}
	}
}

// sharedChunks: false keeps every component scope in the page script.
func TestSharedChunksOff(t *testing.T) {
	root := t.TempDir()
	provideRuntime(t, root)
	off := false
	for p, c := range map[string]string{
		"src/components/Nav.tsx": sharedNav,
		"src/pages/a.tsx":        sharedNavPage("Alpha"),
		"src/pages/b.tsx":        sharedNavPage("Gamma"),
	} {
		writeFileRel(t, root, p, c)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	cfg.SharedChunks = &off
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	site := &sharedSite{cfg: cfg}
	if c := site.chunks(t); len(c) != 0 {
		t.Errorf("sharedChunks: false wrote %v", c)
	}
	if js := site.pageJS(t, "a/index.html"); strings.Contains(js, "__ku(") || !strings.Contains(js, "createSignal(false)") {
		t.Errorf("page script should hold the scope inline:\n%s", js)
	}
}
