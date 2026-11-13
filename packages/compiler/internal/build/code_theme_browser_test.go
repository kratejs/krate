package build

import (
	"os"
	"path/filepath"
	"testing"
)

// codeThemeProject writes a small project whose pages show highlighted code
// through the Code component and a bare highlighted block, links the workspace
// packages, and returns its root.
func codeThemeProject(t *testing.T, pages map[string]string) string {
	t.Helper()
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for rel, src := range pages {
		writeFileRel(t, root, filepath.Join("src", "pages", rel), src)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@krate"), 0755); err != nil {
		t.Fatal(err)
	}
	// The build finds @krate/runtime's dist through node_modules relative to
	// the working directory.
	t.Chdir(root)
	for _, pkg := range []string{"runtime", "components", "core"} {
		if err := os.Symlink(filepath.Join(repoRoot, "packages", pkg), filepath.Join(root, "node_modules", "@krate", pkg)); err != nil {
			t.Skipf("cannot link workspace packages: %v", err)
		}
	}
	return root
}
