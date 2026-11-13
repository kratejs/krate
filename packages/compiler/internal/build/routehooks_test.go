package build

import (
	"os"
	"path/filepath"
	"testing"
)

// hooksProject is scProject with the runtime package linked, so
// generateStaticParams can be executed.
func hooksProject(t *testing.T, files map[string]string) (*Builder, string) {
	t.Helper()
	ensureRuntimeDist(t)
	b, root := scProject(t, files)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repoRoot, "packages", "runtime"), filepath.Join(root, "packages", "runtime")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	return b, root
}
