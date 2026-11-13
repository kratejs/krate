package bundler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

func TestCSSImportWorkspaceAndPackageLinks(t *testing.T) {
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{
		"pnpm-workspace.yaml":          "packages:\n  - apps/*\n  - packages/*\n",
		"apps/web/src/page.tsx":        "import './app.css';\nexport default function P(){return <div/>;}",
		"apps/web/src/app.css":         "@import \"../../../packages/tokens/tokens.css\";\n@import \"@acme/tokens/more.css\";\n@import \"../../../secret.txt\";\n.app{a:b}",
		"packages/tokens/tokens.css":   ":root{--tok:1}",
		"packages/tokens/more.css":     ".more{a:b}",
		"packages/tokens/package.json": `{"name":"@acme/tokens"}`,
		"secret.txt":                   "TOPSECRET",
		"other/x.css":                  ".other{a:b}",
		"apps/web/node_modules/.keep":  "",
	})
	// A workspace link, as pnpm creates it.
	if err := os.MkdirAll(filepath.Join(ws, "apps/web/node_modules/@acme"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ws, "apps/web/node_modules/@acme/tokens")
	if err := os.Symlink(filepath.Join(ws, "packages/tokens"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	root := filepath.Join(ws, "apps/web")
	bundle := bundleEntry(t, root, "src/page.tsx", nil)
	for _, want := range []string{"--tok:1", ".more{"} {
		if !strings.Contains(bundle.CSS, want) {
			t.Errorf("missing %q in:\n%s", want, bundle.CSS)
		}
	}
	if strings.Contains(bundle.CSS, "TOPSECRET") {
		t.Errorf("non-css file outside the root was inlined:\n%s", bundle.CSS)
	}
}

func TestCSSImportOutsideRootNeedsWorkspaceOrAllowlist(t *testing.T) {
	ws := t.TempDir()
	writeTree(t, ws, map[string]string{
		"apps/web/src/page.tsx":      "import './app.css';\nexport default function P(){return <div/>;}",
		"apps/web/src/app.css":       "@import \"../../../packages/tokens/tokens.css\";\n@import \"../../../other/x.css\";\n.app{a:b}",
		"packages/tokens/tokens.css": ":root{--tok:1}",
		"other/x.css":                ".other{a:b}",
	})
	root := filepath.Join(ws, "apps/web")

	build := func(roots []string) string {
		resolver.ResetCaches()
		b := New(root)
		b.SetCSSImportRoots(roots)
		bundle, err := b.Bundle(filepath.Join(root, "src/page.tsx"))
		if err != nil {
			t.Fatal(err)
		}
		return bundle.CSS
	}
	if got := build(nil); strings.Contains(got, "--tok:1") || strings.Contains(got, ".other") {
		t.Errorf("import outside the root allowed without workspace or allowlist:\n%s", got)
	}
	if got := build([]string{"../../packages/tokens"}); !strings.Contains(got, "--tok:1") || strings.Contains(got, ".other") {
		t.Errorf("cssImportRoots should allow only the named directory:\n%s", got)
	}
	// A workspace marker at the repo root opens the whole workspace.
	writeTree(t, ws, map[string]string{"package.json": `{"workspaces":["apps/*","packages/*"]}`})
	if got := build(nil); !strings.Contains(got, "--tok:1") || !strings.Contains(got, ".other") {
		t.Errorf("workspace root should allow sibling directories:\n%s", got)
	}
}
