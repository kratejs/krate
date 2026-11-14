package bundler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
