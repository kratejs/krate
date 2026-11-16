package build

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// copyFixture copies a testdata project into dst. Directories named
// `_node_modules` become `node_modules` (git ignores the real name).
func copyFixture(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		for i, p := range parts {
			if p == "_node_modules" {
				parts[i] = "node_modules"
			}
		}
		target := filepath.Join(dst, filepath.FromSlash(strings.Join(parts, "/")))
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestRendererComponentAndVendorPackage builds a page whose @renderer Canvas
// comes from a Krate package that vendors its engine (krate.vendor), linked
// into node_modules like a workspace package or installed as a copy.
func TestRendererComponentAndVendorPackage(t *testing.T) {
	for _, mode := range []string{"linked", "installed"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			copyFixture(t, filepath.Join("testdata", "renderer_vendor"), root)
			pkg := filepath.Join(root, "node_modules", "@krate", "fakethree")
			if err := os.MkdirAll(filepath.Dir(pkg), 0755); err != nil {
				t.Fatal(err)
			}
			if mode == "linked" {
				if err := os.Symlink(filepath.Join(root, "fakethree"), pkg); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			} else {
				copyFixture(t, filepath.Join("testdata", "renderer_vendor", "fakethree"), pkg)
				if err := os.RemoveAll(filepath.Join(root, "fakethree")); err != nil {
					t.Fatal(err)
				}
			}
			cfg := config.Default()
			cfg.PagesDir = filepath.Join(root, "src", "pages")
			cfg.OutDir = filepath.Join(root, "dist")
			cfg.Minify = false
			out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
			if err != nil {
				t.Fatalf("BuildAll: %v\n%s", err, out)
			}
			if strings.Contains(out, "KR") {
				t.Errorf("renderer children must not produce diagnostics:\n%s", out)
			}
			html := readBuilt(t, filepath.Join(cfg.OutDir, "index.html"))
			if !strings.Contains(html, `id="title"`) || !strings.Contains(html, "<canvas") {
				t.Errorf("the page and the Canvas body must render:\n%s", html)
			}
			for _, tag := range []string{"<mesh", "<ambientlight", "<ambientLight", "<boxGeometry", "<pointLight", "<group"} {
				if strings.Contains(html, tag) {
					t.Errorf("renderer children must not render to HTML, found %s:\n%s", tag, html)
				}
			}
			js := readBuilt(t, mustGlobOne(t, filepath.Join(cfg.OutDir, "index.*.js")))
			for _, want := range []string{
				`children:()=>[h('ambientLight',{intensity:0.5}),h('mesh',{position:()=>[n(),0,0]`,
				`attach:()=>((parent, self)=>()=>{})`,
				`onPointerMissed:()=>setN(0)`,
				`h('group',{position:()=>origin,'rotation-y':()=>Math.PI})`,
				`()=>((n() > 3) && h('pointLight',{position:()=>[2,2,2]}))`,
				`krateList(()=>(items()),(i)=>i,(i)=>h(Box,`,
				`h(Box, {x:9}),h('krate-suspense',{fallback:h('mesh',{name:'loading',scale:()=>n()})},h(Box, {x:3}))]}`,
				// Components reached from the children are declared in the
				// caller's scope, transitively and in renderer form.
				"function Box(",
				"function Skin(",
				`attach:()=>((parent)=>parent)`,
				// The engine is vendored and shares the page's `three`.
				"ENGINE_MARK",
				"ROOT_THREE_INSTANCE",
				`createRoot=__krateVendor["createRoot"]`,
				`Vector3=__krateVendor["Vector3"]`,
			} {
				if !strings.Contains(js, want) {
					t.Errorf("hydration JS is missing %q:\n%s", want, js)
				}
			}
			// A Suspense in renderer children belongs to the renderer: the
			// page stays static and ships no Suspense runtime.
			if p, ok := manifestPage(readManifest(t, root), "/"); ok && p.Mode != RenderSSG {
				t.Errorf("a Suspense inside renderer children must not make the page %s", p.Mode)
			}
			for _, bad := range []string{"h(Suspense", "__krateBoundary"} {
				if strings.Contains(js, bad) {
					t.Errorf("renderer children must not use the DOM Suspense runtime, found %q", bad)
				}
			}
			if strings.Contains(js, "NESTED_THREE_COPY") {
				t.Error("the engine must use the page's three, not a second copy")
			}
			if strings.Contains(js, "Vector3$m") {
				t.Error("vendor named imports must ask for the original export name")
			}
		})
	}
}

func TestUsesSuspenseSkipsRendererChildren(t *testing.T) {
	parse := func(src string) *ast.Program {
		prog, errs := tsstrip.Parse("page.tsx", src, tsstrip.Options{})
		if len(errs) > 0 {
			t.Fatalf("parse: %v", errs)
		}
		return prog
	}
	inCanvas := parse(`export default function P() { return <Canvas><Suspense fallback={<mesh />}><Box /></Suspense></Canvas>; }`)
	if usesSuspense(inCanvas, map[string]bool{"Canvas": true}) {
		t.Error("a Suspense inside renderer children must not count")
	}
	if !usesSuspense(inCanvas, nil) {
		t.Error("without renderer knowledge the Suspense counts")
	}
	both := parse(`export default function P() { return <main><Canvas><Suspense><Box /></Suspense></Canvas><Suspense fallback="..."><Data /></Suspense></main>; }`)
	if !usesSuspense(both, map[string]bool{"Canvas": true}) {
		t.Error("a Suspense outside renderer children still makes the page stream")
	}
}
