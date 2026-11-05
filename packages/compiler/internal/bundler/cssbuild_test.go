package bundler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func bundleEntry(t *testing.T, root, entry string, cache *ModuleCache) *Bundle {
	t.Helper()
	resolver.ResetCaches()
	b := New(root)
	b.SetModuleCache(cache)
	bundle, err := b.Bundle(filepath.Join(root, entry))
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}
	return bundle
}

func TestCSSImportSiblingPackageAndURLRebase(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"src/page.tsx":                        "import './styles/sub/app.css';\nexport default function P(){return <div/>;}",
		"src/styles/tokens.css":               ":root{--c:red}",
		"src/styles/sub/app.css":              "@import \"../tokens.css\";\n@import url(./unq.css);\n@import \"fakepkg/dist/style.css\";\n@import \"./deep/deep.css\";\n.app{background:url(\"/root.png\")}",
		"src/styles/sub/unq.css":              ".unq{a:b}",
		"src/styles/sub/deep/deep.css":        ".deep{background:url(./d.png)}",
		"src/styles/sub/deep/d.png":           "DDD",
		"src/styles/sub/root.png":             "NOT-THE-ROOT-FILE",
		"node_modules/fakepkg/dist/style.css": ".pkg{background:url(./p.png)}",
		"node_modules/fakepkg/dist/p.png":     "PPP",
		"node_modules/fakepkg/package.json":   `{"name":"fakepkg"}`,
	})
	bundle := bundleEntry(t, root, "src/page.tsx", nil)
	for _, want := range []string{"--c:red", ".unq{a:b}", ".pkg{", ".deep{", `url("/root.png")`} {
		if !strings.Contains(bundle.CSS, want) {
			t.Errorf("missing %q in:\n%s", want, bundle.CSS)
		}
	}
	if strings.Contains(bundle.CSS, "@import") {
		t.Errorf("import left in the sheet:\n%s", bundle.CSS)
	}
	if strings.Contains(bundle.CSS, "url(./d.png)") || strings.Contains(bundle.CSS, "url(./p.png)") {
		t.Errorf("url() in imported files not rebased:\n%s", bundle.CSS)
	}
	var d, p, rootPNG bool
	for src, u := range bundle.AssetFiles {
		switch {
		case strings.HasSuffix(src, "d.png") && strings.HasPrefix(u, "/assets/d-"):
			d = true
		case strings.HasSuffix(src, "p.png") && strings.HasPrefix(u, "/assets/p-"):
			p = true
		case strings.HasSuffix(src, "root.png"):
			rootPNG = true
		}
	}
	if !d || !p {
		t.Errorf("imported-file assets not registered: %v", bundle.AssetFiles)
	}
	if rootPNG {
		t.Errorf("root-absolute url() must not be joined to the CSS dir: %v", bundle.AssetFiles)
	}
	deps := strings.Join(bundle.CSSDeps, "\n")
	for _, want := range []string{"tokens.css", "unq.css", "deep.css", "d.png", "style.css", "p.png"} {
		if !strings.Contains(deps, want) {
			t.Errorf("CSSDeps missing %s: %v", want, bundle.CSSDeps)
		}
	}
}

func TestCSSMissingURLAndImportWarn(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"p.tsx": "import './a.css';\nexport default function P(){return <div/>;}",
		"a.css": "@import './nope.css';\n.a{background:url(./missing.png)}",
	})
	bundle := bundleEntry(t, root, "p.tsx", nil)
	var msgs []string
	for _, m := range bundle.Modules {
		for _, d := range m.Diagnostics {
			msgs = append(msgs, d.Message)
		}
	}
	all := strings.Join(msgs, "\n")
	if !strings.Contains(all, "nope.css") || !strings.Contains(all, "missing.png") {
		t.Fatalf("expected warnings for unresolved import and url, got: %v", msgs)
	}
}

func TestCSSCacheInvalidatedByImportedFileAndAsset(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"p.tsx":         "import './app.css';\nexport default function P(){return <div/>;}",
		"app.css":       "@import './deep/deep.css';",
		"deep/deep.css": ".d{color:red;background:url(./d.png)}",
		"deep/d.png":    "ONE",
	})
	cache := NewModuleCache()
	first := bundleEntry(t, root, "p.tsx", cache)
	if !strings.Contains(first.CSS, "color:red") {
		t.Fatalf("first: %s", first.CSS)
	}
	writeTree(t, root, map[string]string{"deep/deep.css": ".d{color:blue;background:url(./d.png)}"})
	second := bundleEntry(t, root, "p.tsx", cache)
	if !strings.Contains(second.CSS, "color:blue") {
		t.Fatalf("stale CSS after editing an imported file: %s", second.CSS)
	}
	oldURL := second.AssetFiles[filepath.Join(root, "deep", "d.png")]
	writeTree(t, root, map[string]string{"deep/d.png": "TWO-DIFFERENT"})
	third := bundleEntry(t, root, "p.tsx", cache)
	newURL := third.AssetFiles[filepath.Join(root, "deep", "d.png")]
	if oldURL == "" || newURL == "" || oldURL == newURL {
		t.Fatalf("asset hash did not follow content: %q -> %q", oldURL, newURL)
	}
	if !strings.Contains(third.CSS, newURL) {
		t.Fatalf("css still points at the old asset: %s", third.CSS)
	}
}

func TestCSSModuleBundleFeatures(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"p.tsx": `import a from './A.module.css';
import b from './B.module.css';
export default function P(){
  return <div><span className={a["card-title"]}>x</span><i className={b.cardTitle}>y</i><u className={a.card}>z</u></div>;
}`,
		"base.module.css": ".base{color:red}",
		"A.module.css":    "@keyframes pop{from{opacity:0}}\n.card{composes: base from \"./base.module.css\"; animation: pop 1s}\n.card-title{color:blue}",
		"B.module.css":    "@keyframes pop{from{opacity:1}}\n.card-title{animation: pop 2s}",
	})
	bundle := bundleEntry(t, root, "p.tsx", nil)
	a := bundle.CSSModules[filepath.Join(root, "A.module.css")]
	b := bundle.CSSModules[filepath.Join(root, "B.module.css")]
	base := bundle.CSSModules[filepath.Join(root, "base.module.css")]
	if a == nil || b == nil || base == nil {
		t.Fatalf("modules missing: %v", bundle.CSSModules)
	}
	if strings.Contains(bundle.CSS, "composes") {
		t.Errorf("composes left in CSS:\n%s", bundle.CSS)
	}
	if !strings.Contains(bundle.CSS, ".base_") {
		t.Errorf("composed module CSS not bundled:\n%s", bundle.CSS)
	}
	if !strings.HasSuffix(a.Mappings["card"], base.Mappings["base"]) {
		t.Errorf("card mapping %q lacks base %q", a.Mappings["card"], base.Mappings["base"])
	}
	if a.Mappings["pop"] == b.Mappings["pop"] || a.Mappings["pop"] == "" {
		t.Errorf("keyframes not scoped per module: %q %q", a.Mappings["pop"], b.Mappings["pop"])
	}
	// The member reads were replaced with literals (no leftover `styles` refs).
	var printed string
	for _, m := range bundle.Modules {
		if m.IsEntry {
			printed = astprint.Print(m.Program)
		}
	}
	if !strings.Contains(printed, a.Mappings["card-title"]) || !strings.Contains(printed, b.Mappings["cardTitle"]) ||
		strings.Contains(printed, `a["card-title"]`) || strings.Contains(printed, "b.cardTitle") {
		t.Errorf("computed/camelCase member reads not rewritten:\n%s", printed)
	}
}

func TestUnbalancedCSSIsReportedAndDoesNotLeakIntoTheNextSheet(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"src/page.tsx":  "import './a.css';\nimport './b.css';\nexport default function P(){return <div/>;}",
		"src/a.css":     ".a{color:red}\n.broken {\n  color:green;\n",
		"src/b.css":     ".b{color:blue}",
		"src/page2.tsx": "import './c.css';\nexport default function P(){return <div/>;}",
		"src/c.css":     "@import './d.css';\n.c{color:red}",
		"src/d.css":     ".d{color:red}\n}\n.d2{color:blue}",
	})
	bundle := bundleEntry(t, root, "src/page.tsx", nil)
	if idx := strings.Index(bundle.CSS, ".b{color:blue}"); idx < 0 ||
		strings.Count(bundle.CSS[:idx], "{") != strings.Count(bundle.CSS[:idx], "}") {
		t.Errorf("the unclosed block of a.css swallows b.css:\n%s", bundle.CSS)
	}
	var found bool
	for _, m := range bundle.Modules {
		for _, d := range m.Diagnostics {
			if d.Code == diag.CodeCSSSyntax && d.File == "src/a.css" && d.Line == 2 && d.Col == 9 {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("no KR7141 warning at src/a.css:2:9 in %+v", bundle.Modules)
	}

	// A stray brace in an imported file is reported against that file.
	bundle = bundleEntry(t, root, "src/page2.tsx", nil)
	found = false
	for _, m := range bundle.Modules {
		for _, d := range m.Diagnostics {
			if d.Code == diag.CodeCSSSyntax && d.File == "src/d.css" && d.Line == 2 {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("no KR7141 warning at src/d.css:2 for the imported file")
	}
	if strings.Count(bundle.CSS, "{") != strings.Count(bundle.CSS, "}") {
		t.Errorf("imported sheet left unbalanced:\n%s", bundle.CSS)
	}
}
