package build

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// buildFiles writes files (paths relative to root) and runs BuildAll with
// Tailwind enabled, returning the root, the out dir and captured stderr.
func buildFiles(t *testing.T, files map[string]string, tweak func(*config.Config)) (root, outDir, stderr string) {
	t.Helper()
	root = t.TempDir()
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Tailwind.Enabled = true
	cfg.Tailwind.Strict = true
	if tweak != nil {
		tweak(cfg)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	berr := New(root, cfg).BuildAll()
	os.Stderr = orig
	_ = w.Close()
	logged, _ := io.ReadAll(r)
	if berr != nil {
		t.Fatalf("BuildAll: %v\n%s", berr, logged)
	}
	return root, cfg.OutDir, string(logged)
}
