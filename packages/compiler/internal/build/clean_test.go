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
