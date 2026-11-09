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

func TestCleanWithOptionsAndFreedSpace(t *testing.T) {
	setup := func(t *testing.T) (string, *config.Config) {
		root := t.TempDir()
		touchDir(t, filepath.Join(root, "dist"))
		touchDir(t, filepath.Join(root, ".krate", "cache"))
		touchDir(t, filepath.Join(root, ".krate", "types"))
		cfg := config.Default()
		cfg.OutDir = filepath.Join(root, "dist")
		return root, cfg
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	t.Run("dist only", func(t *testing.T) {
		root, cfg := setup(t)
		res, err := CleanWith(root, cfg, CleanOptions{Dist: true})
		if err != nil {
			t.Fatal(err)
		}
		if exists(filepath.Join(root, "dist")) || !exists(filepath.Join(root, ".krate", "cache")) {
			t.Error("--dist must remove dist and keep the cache")
		}
		if len(res.Removed) != 1 || res.Freed != 1 {
			t.Errorf("removed %v, freed %d; want one dir and 1 byte", res.Removed, res.Freed)
		}
	})
	t.Run("cache only", func(t *testing.T) {
		root, cfg := setup(t)
		if _, err := CleanWith(root, cfg, CleanOptions{Cache: true}); err != nil {
			t.Fatal(err)
		}
		if !exists(filepath.Join(root, "dist")) || exists(filepath.Join(root, ".krate", "cache")) {
			t.Error("--cache must remove the cache and keep dist")
		}
	})
	t.Run("all", func(t *testing.T) {
		root, cfg := setup(t)
		res, err := CleanWith(root, cfg, CleanOptions{All: true})
		if err != nil {
			t.Fatal(err)
		}
		if exists(filepath.Join(root, "dist")) || exists(filepath.Join(root, ".krate")) {
			t.Error("--all must remove dist and all of .krate")
		}
		if res.Freed != 3 {
			t.Errorf("freed %d bytes, want 3", res.Freed)
		}
	})
}

func TestFormatBytes(t *testing.T) {
	for in, want := range map[int64]string{0: "0 B", 999: "999 B", 1500: "1.5 kB", 2_500_000: "2.50 MB", 3_100_000_000: "3.10 GB", -1500: "-1.5 kB"} {
		if got := FormatBytes(in); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
