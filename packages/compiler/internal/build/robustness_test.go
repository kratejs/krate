package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// outDir ".." is refused before the build deletes anything, and the files of
// the parent directory survive; an outDir outside the project that holds no
// sources still builds.
func TestBuildAllRefusesParentOutDirBeforeDeleting(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "proj")
	writeProjectFile(t, root, "src/pages/index.tsx", "export default function P() { return <main>hi</main>; }\n")
	writeProjectFile(t, parent, "keep.txt", "precious")
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.Tailwind.Enabled = false
	cfg.OutDir = parent
	err := New(root, cfg).BuildAll()
	if err == nil || !strings.Contains(err.Error(), "KR8350") {
		t.Fatalf("want a KR8350 error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(parent, "keep.txt")); statErr != nil {
		t.Fatalf("the parent directory was wiped: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "src", "pages", "index.tsx")); statErr != nil {
		t.Fatalf("project sources were wiped: %v", statErr)
	}

	cfg.OutDir = filepath.Join(parent, "site-dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("an outDir outside the project must keep working: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(cfg.OutDir, "index.html")); statErr != nil {
		t.Fatalf("no output in ../site-dist: %v", statErr)
	}
}
