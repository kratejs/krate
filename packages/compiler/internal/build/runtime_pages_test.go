package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestPageNeedsRuntime(t *testing.T) {
	tests := []struct {
		name string
		r    *PageResult
		want bool
	}{
		{"nil", nil, false},
		{"plain static page", &PageResult{HTML: "<h1>About</h1>"}, false},
		{"hydration script", &PageResult{HTML: "<p>x</p>", HasJS: true, JSFile: "index.abc.js", HydrationJS: "x"}, true},
		{"client island markup", &PageResult{HTML: "<krate-island data-x></krate-island>"}, true},
		{"server action form", &PageResult{HTML: `<form data-krate-action="a"></form>`}, true},
		{"action url", &PageResult{HTML: `<form action="/__krate/action/x"></form>`}, true},
		{"per-request page", &PageResult{HTML: "<p>x</p>", Mode: RenderSSR}, true},
		{"async server page", &PageResult{HTML: "<p>x</p>", ServerPage: true}, true},
		{"page with regions", &PageResult{HTML: "<p>x</p>", Regions: []Region{{}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pageNeedsRuntime(tt.r); got != tt.want {
				t.Fatalf("pageNeedsRuntime = %v, want %v", got, tt.want)
			}
		})
	}
}

// A static page of a site that has islands elsewhere must not load the shared
// runtime chunk (no script, no preload); the island page still does.
func TestStaticPagesSkipRuntimeChunk(t *testing.T) {
	for _, spa := range []bool{true, false} {
		name := "spa"
		if !spa {
			name = "no-spa"
		}
		t.Run(name, func(t *testing.T) {
			repoRoot, err := repoRootPath()
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			// Point the project at the built runtime package.
			if err := os.MkdirAll(filepath.Join(root, "packages"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(repoRoot, "packages", "runtime"), filepath.Join(root, "packages", "runtime")); err != nil {
				t.Skipf("cannot link the runtime package: %v", err)
			}
			pagesDir := filepath.Join(root, "src", "pages")
			if err := os.MkdirAll(pagesDir, 0755); err != nil {
				t.Fatal(err)
			}
			write := func(file, src string) {
				if err := os.WriteFile(filepath.Join(pagesDir, file), []byte(src), 0644); err != nil {
					t.Fatal(err)
				}
			}
			write("index.tsx", `export default function Home() { return <main><h1>Home</h1><Link href="/about">about</Link></main>; }`)
			write("about.tsx", `export default function About() { return <main><h1>About</h1><Link href="/counter">counter</Link></main>; }`)
			write("counter.tsx", `
import { useState } from "react";
export default function Counter() {
  const [n, setN] = useState(0);
  return <main><h1>Counter</h1><button onClick={() => setN(n + 1)}>n={n}</button></main>;
}`)

			cfg := config.Default()
			cfg.PagesDir = pagesDir
			cfg.OutDir = filepath.Join(root, "dist")
			cfg.SPA = &spa
			if err := New(root, cfg).BuildAll(); err != nil {
				t.Fatalf("BuildAll: %v", err)
			}

			chunks, _ := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "runtime.*.js"))
			if len(chunks) != 1 {
				requireE2E(t, "runtime dist not built (chunks: %v)", chunks)
				return
			}
			runtimeRef := "chunks/" + filepath.Base(chunks[0])
			read := func(rel string) string {
				data, err := os.ReadFile(filepath.Join(cfg.OutDir, rel))
				if err != nil {
					t.Fatal(err)
				}
				return string(data)
			}
			for _, rel := range []string{"index.html", "about/index.html"} {
				html := read(rel)
				if strings.Contains(html, runtimeRef) {
					t.Errorf("static page %s still references the runtime chunk", rel)
				}
				if regexp.MustCompile(`<script[^>]*\ssrc=`).MatchString(html) {
					t.Errorf("static page %s ships a script", rel)
				}
			}
			counter := read("counter/index.html")
			if !strings.Contains(counter, `src=/`+runtimeRef) && !strings.Contains(counter, `src="/`+runtimeRef) {
				t.Errorf("island page lost its runtime script:\n%s", counter)
			}
		})
	}
}
