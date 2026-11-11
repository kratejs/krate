package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// A native page loads only the code it uses: a page without client code ships
// no script at all, an interactive page links its own bundle, and a lazy
// chunk is not preloaded (it loads when its component first renders).
func TestNativePagesLoadOnlyWhatTheyUse(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, fakeKrateModules)
	writeTree(t, root, map[string]string{
		"package.json":        `{"name":"t","type":"module"}`,
		"src/pages/index.tsx": "export default function P() { return <main>home <a href=\"/about\">x</a></main>; }\n",
		"src/pages/about.tsx": "import { useState } from 'react';\nexport default function P() { const [n,setN]=useState(0); return <main><button onClick={() => setN(n+1)}>{n}</button></main>; }\n",
		"src/pages/lz.tsx":    "import { lazy, Suspense, useState } from 'react';\nconst Chart = lazy(() => import('../Chart'));\nexport default function P() { const [s,setS]=useState(false); return <main><button onClick={() => setS(!s)}>t</button>{s ? <Suspense fallback={<p>..</p>}><Chart/></Suspense> : null}</main>; }\n",
		"src/Chart.tsx":       "import { useState } from 'react';\nexport default function Chart() { const [n,setN]=useState(0); return <i onClick={() => setN(n+1)}>{n}</i>; }\n",
	})
	res, err := newNativeBuilder(t, root).BuildNativeApp(NativeAppOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dist := filepath.Join(res.AppDir, "dist")
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(dist, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	home := read("index.html")
	for _, bad := range []string{"<script", "rel=preload", "modulepreload", "__KRATE_CFG__"} {
		if strings.Contains(home, bad) {
			t.Errorf("a page without client code must ship no %q:\n%s", bad, home)
		}
	}

	about := read("about/index.html")
	if !strings.Contains(about, "<script src=/about/index.") {
		t.Errorf("an interactive page links its own bundle:\n%s", about)
	}
	if strings.Contains(about, "lazy-") {
		t.Errorf("a page links only its own chunks:\n%s", about)
	}

	lz := read("lz/index.html")
	if strings.Contains(lz, "modulepreload") {
		t.Errorf("a lazy chunk must load on demand in a native build, not be preloaded:\n%s", lz)
	}
	if matches, _ := filepath.Glob(filepath.Join(dist, "lz", "lazy-Chart.*.js")); len(matches) != 1 {
		t.Errorf("the lazy component keeps its own chunk, got %v", matches)
	}
	js, _ := filepath.Glob(filepath.Join(dist, "lz", "index.*.js"))
	if len(js) != 1 {
		t.Fatalf("lz page script: %v", js)
	}
	page, _ := os.ReadFile(js[0])
	if !strings.Contains(string(page), "__krateLazy(") {
		t.Errorf("the page script loads the chunk through __krateLazy:\n%s", page)
	}
}

// Precompressed siblings are of no use to the shell, which serves the bundle
// as it is.
func TestNativeBuildWritesNoPrecompressedAssets(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, fakeKrateModules)
	writeTree(t, root, map[string]string{
		"package.json":        `{"name":"t","type":"module"}`,
		"src/pages/index.tsx": "export default function P() { return <main>home</main>; }\n",
	})
	b := newNativeBuilder(t, root)
	b.Cfg.Compress = config.CompressConfig{Brotli: true, Gzip: true}
	res, err := b.BuildNativeApp(NativeAppOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_ = filepath.Walk(filepath.Join(res.AppDir, "dist"), func(p string, fi os.FileInfo, _ error) error {
		if ext := filepath.Ext(p); !fi.IsDir() && (ext == ".br" || ext == ".gz") {
			t.Errorf("native build wrote %s", p)
		}
		return nil
	})
}
