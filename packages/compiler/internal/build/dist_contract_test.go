package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// buildContractSite builds a three-page site: a static page, a page with a
// counter, and the 404 page. spa selects the router build.
func buildContractSite(t *testing.T, spa bool) *config.Config {
	t.Helper()
	root := t.TempDir()
	ensureRuntimeDist(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "packages")); err != nil {
		t.Fatal(err)
	}
	writeFileRel(t, root, "src/pages/index.tsx", `export default function Home() { return <main><h1>Home</h1><p>plain</p></main>; }`)
	writeFileRel(t, root, "src/pages/counter.tsx", `import { createSignal } from "@krate/runtime";
export default function Counter() {
  const [n, setN] = createSignal(0);
  return <main><h1>Counter</h1><button onClick={() => setN(n() + 1)}>n={n()}</button></main>;
}`)
	writeFileRel(t, root, "src/pages/404.tsx", `export default function NotFound() { return <main><h1>Missing</h1></main>; }`)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.SPA = &spa
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	return cfg
}

var scriptRefRe = regexp.MustCompile(`<script[^>]*\ssrc=|rel="?modulepreload|rel="?preload"? as="?script`)

// TestDistLayoutContract pins the shape of a build's output. Deploy scripts,
// static hosts and the docs rely on it, so a change here has to come with an
// upgrade note (guides/upgrading.md) and a CHANGELOG line.
func TestDistLayoutContract(t *testing.T) {
	cfg := buildContractSite(t, true)
	out := cfg.OutDir

	for _, rel := range []string{"index.html", "counter/index.html", "404.html", ".krate/manifest.json"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err != nil {
			t.Errorf("dist/%s missing: %v", rel, err)
		}
	}
	// 0.4 wrote dist/manifest.json; the manifest is build metadata and lives
	// in the dot directory now.
	if _, err := os.Stat(filepath.Join(out, "manifest.json")); err == nil {
		t.Error("dist/manifest.json must not exist; the manifest is dist/.krate/manifest.json")
	}
	data, err := os.ReadFile(filepath.Join(out, ".krate", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Pages []struct {
			Route string `json:"route"`
		} `json:"pages"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("manifest.json is not valid JSON: %v", err)
	}
	if len(manifest.Pages) == 0 {
		t.Errorf("manifest.json lists no pages: %.300s", data)
	}
	routes := map[string]bool{}
	for _, p := range manifest.Pages {
		routes[p.Route] = true
	}
	for _, route := range []string{"/", "/counter"} {
		if !routes[route] {
			t.Errorf("manifest.json has no page %q (pages: %v)", route, routes)
		}
	}
	// The 404 page is served directly, never listed as a route.
	if routes["/404"] {
		t.Error("manifest.json should not list /404")
	}

	// Hydration scripts and shared chunks are hashed files at the top level
	// and under chunks/.
	if m, _ := filepath.Glob(filepath.Join(out, "counter", "index.*.js")); len(m) == 0 {
		if m2, _ := filepath.Glob(filepath.Join(out, "counter.*.js")); len(m2) == 0 {
			t.Error("no hydration script for /counter")
		}
	}
	if m, _ := filepath.Glob(filepath.Join(out, "chunks", "runtime.*.js")); len(m) != 1 {
		t.Errorf("expected one shared runtime chunk, got %v", m)
	}
}

// TestJSBudget is the budget for pages without interactivity: no
// script tag, no preload, even when another page of the site is interactive.
// The counter page keeps the runtime chunk within a size budget; the figures
// are today's (26.1 KB gzip with the router, 17.8 KB without) plus about 15%.
func TestJSBudget(t *testing.T) {
	for _, tc := range []struct {
		name      string
		spa       bool
		maxRuntKB float64
	}{
		{"spa", true, 30},
		{"no-spa", false, 21},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := buildContractSite(t, tc.spa)
			out := cfg.OutDir
			html, err := os.ReadFile(filepath.Join(out, "index.html"))
			if err != nil {
				t.Fatal(err)
			}
			if loc := scriptRefRe.FindString(string(html)); loc != "" {
				t.Errorf("static page / loads JavaScript (%q): %s", loc, html)
			}
			counter, _ := os.ReadFile(filepath.Join(out, "counter", "index.html"))
			if !scriptRefRe.Match(counter) {
				t.Fatalf("counter page loads no script:\n%s", counter)
			}

			chunks, _ := filepath.Glob(filepath.Join(out, "chunks", "runtime.*.js"))
			if len(chunks) != 1 {
				t.Fatalf("runtime chunks = %v", chunks)
			}
			rt, _ := os.ReadFile(chunks[0])
			gz := float64(gzipSize(string(rt))) / 1024
			t.Logf("runtime chunk: %d bytes, %.1f KB gzip (budget %.0f KB)", len(rt), gz, tc.maxRuntKB)
			if gz > tc.maxRuntKB {
				t.Errorf("runtime chunk is %.1f KB gzip, over the %.0f KB budget; trim the runtime or raise the budget on purpose and say so in the CHANGELOG", gz, tc.maxRuntKB)
			}

			// The page's own script stays small: a counter is not a bundle.
			var pageJS int
			for _, ref := range regexp.MustCompile(`(?:src|href)="?(/[^" >]*\.js)`).FindAllStringSubmatch(string(counter), -1) {
				if strings.Contains(ref[1], "/chunks/") {
					continue
				}
				b, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(strings.TrimPrefix(ref[1], "/"))))
				if err != nil {
					t.Fatalf("counter references %s: %v", ref[1], err)
				}
				pageJS += gzipSize(string(b))
			}
			if pageJS > 2048 {
				t.Errorf("counter page script is %d bytes gzip, want <= 2048", pageJS)
			}
		})
	}
}
