package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestMarkdownCodeDoesNotShipLibraryCSS checks that the Code/Aside auto-import
// resolves through the package subpaths, so a page with a code block carries
// the CSS of Code and Aside only, not of every component in the barrel.
func TestMarkdownCodeDoesNotShipLibraryCSS(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "# Notes\n\n```js\nconst a = 1;\n```\n\n:::note\nhello\n:::\n"
	if err := os.WriteFile(filepath.Join(pagesDir, "notes.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, err := repoRootPath()
	if err != nil {
		t.Skip(err)
	}
	nm := filepath.Join(root, "node_modules", "@krate")
	if err := linkRuntimePackage(t, nm); err != nil {
		t.Skip(err)
	}
	if err := os.Symlink(filepath.Join(repo, "packages", "components"), filepath.Join(nm, "components")); err != nil {
		t.Skip(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Minify = false
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("build: %v", err)
	}
	var css strings.Builder
	_ = filepath.Walk(cfg.OutDir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() && strings.HasSuffix(p, ".css") {
			data, _ := os.ReadFile(p)
			css.Write(data)
		}
		return nil
	})
	all := css.String()
	if !strings.Contains(all, ".krate-code") {
		t.Errorf("Code CSS missing from the build output (%d bytes of CSS)", len(all))
	}
	for _, unrelated := range []string{".krate-toast", ".krate-sheet", ".krate-select"} {
		if strings.Contains(all, unrelated) {
			t.Errorf("unrelated component CSS %s shipped with a markdown page", unrelated)
		}
	}
}
