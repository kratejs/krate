package build

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestBuilderSiteGraph(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	writeFileRel(t, root, "src/pages/about.tsx", `export default function About() { return <h1>About</h1>; }`)

	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false

	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	g, err := b.SiteGraph()
	if err != nil {
		t.Fatalf("SiteGraph: %v", err)
	}

	byRoute := map[string]SiteGraphRoute{}
	for _, r := range g.Routes {
		byRoute[r.Route] = r
	}

	about, ok := byRoute["/about"]
	if !ok {
		t.Fatalf("no /about route in graph: %+v", g.Routes)
	}
	if about.Source != "src/pages/about.tsx" {
		t.Errorf("/about source = %q", about.Source)
	}
	if !about.Built {
		t.Error("/about should be built")
	}
	foundDep := false
	for _, d := range about.Dependencies {
		if d == "src/pages/about.tsx" {
			foundDep = true
		}
	}
	if !foundDep {
		t.Errorf("/about dependencies missing page file: %v", about.Dependencies)
	}
	if len(about.Outputs) == 0 || about.Bytes == 0 {
		t.Errorf("/about outputs/bytes empty: %v %d", about.Outputs, about.Bytes)
	}

	// The root route must not enumerate the whole tree.
	home, ok := byRoute["/"]
	if !ok {
		t.Fatalf("no / route in graph")
	}
	if !slices.Contains(home.Outputs, "index.html") {
		t.Errorf("root outputs missing index.html: %v", home.Outputs)
	}
	for _, o := range home.Outputs {
		if strings.Contains(o, "/") {
			t.Errorf("root output should be top-level, got %q", o)
		}
	}

	if len(g.Files["src/pages/about.tsx"]) == 0 {
		t.Errorf("files map missing dependents for the page file: %v", g.Files)
	}
}
