package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func touchDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "f.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCleanRemovesDistAndCache(t *testing.T) {
	root := t.TempDir()
	dist := filepath.Join(root, "dist")
	cache := filepath.Join(root, ".krate", "cache")
	// A sibling under .krate that must survive the clean.
	typesDir := filepath.Join(root, ".krate", "types")
	touchDir(t, dist)
	touchDir(t, cache)
	touchDir(t, typesDir)

	cfg := config.Default()
	cfg.OutDir = dist

	result, err := Clean(root, cfg)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if len(result.Removed) != 2 {
		t.Fatalf("expected 2 removed dirs, got %v", result.Removed)
	}
	if _, err := os.Stat(dist); !os.IsNotExist(err) {
		t.Errorf("dist should be removed")
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Errorf(".krate/cache should be removed")
	}
	if _, err := os.Stat(typesDir); err != nil {
		t.Errorf(".krate/types should be preserved: %v", err)
	}
}

func TestCleanMissingTargets(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.OutDir = filepath.Join(root, "dist")

	result, err := Clean(root, cfg)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if len(result.Removed) != 0 {
		t.Errorf("expected nothing removed, got %v", result.Removed)
	}
	if len(result.Missing) != 2 {
		t.Errorf("expected 2 missing targets, got %v", result.Missing)
	}
}

func TestCleanRefusesOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	touchDir(t, outside)

	cfg := config.Default()
	cfg.OutDir = outside

	if _, err := Clean(root, cfg); err == nil {
		t.Fatal("expected an error cleaning a directory outside the project root")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("outside directory must not be deleted: %v", err)
	}
}

// An outDir that is or contains the project's sources must never be removed:
// `outDir: "."` used to make `krate clean` delete the whole project.
func TestCleanRefusesOutDirHoldingSources(t *testing.T) {
	for _, outDir := range []string{".", "src", "src/pages", "public"} {
		t.Run(outDir, func(t *testing.T) {
			root := t.TempDir()
			touchDir(t, filepath.Join(root, "src", "pages"))
			touchDir(t, filepath.Join(root, "public"))
			if err := os.WriteFile(filepath.Join(root, "krate.config.ts"), []byte("export default {}"), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg := config.Default()
			cfg.PagesDir = filepath.Join(root, "src", "pages")
			cfg.PublicDir = filepath.Join(root, "public")
			cfg.OutDir = filepath.Join(root, outDir)

			if _, err := Clean(root, cfg); err == nil {
				t.Fatalf("outDir %q: expected a refusal", outDir)
			}
			for _, keep := range []string{"krate.config.ts", "src/pages/f.txt", "public/f.txt"} {
				if _, err := os.Stat(filepath.Join(root, keep)); err != nil {
					t.Errorf("outDir %q: %s was deleted: %v", outDir, keep, err)
				}
			}
		})
	}
}

func TestCleanStillRemovesOrdinaryOutDirs(t *testing.T) {
	root := t.TempDir()
	touchDir(t, filepath.Join(root, "build", "out"))
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.PublicDir = filepath.Join(root, "public")
	cfg.OutDir = filepath.Join(root, "build", "out")
	if _, err := Clean(root, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg.OutDir); !os.IsNotExist(err) {
		t.Errorf("outDir should be removed: %v", err)
	}
}
