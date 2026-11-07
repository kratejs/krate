package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// CleanResult reports what a Clean run removed.
type CleanResult struct {
	// Removed are the directories that existed and were deleted.
	Removed []string
	// Missing are the target directories that did not exist (nothing to do).
	Missing []string
}

// Clean removes the project's generated output: the build output directory
// (dist) and the compiler cache (.krate/cache). It is deliberately scoped to
// those two paths - the rest of .krate (types, generated pages, sidecar
// manifests) is regenerated on demand by build/dev and is left in place so a
// clean does not invalidate editor types unnecessarily.
// A path is only removed when it resolves to within the project root, so a
// misconfigured outDir can never delete an unrelated directory.
func Clean(root string, cfg *config.Config) (*CleanResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving project root: %w", err)
	}

	targets := []string{
		cfg.OutDir,
		filepath.Join(absRoot, ".krate", "cache"),
	}

	result := &CleanResult{}
	for _, target := range targets {
		if target == "" {
			continue
		}
		abs := target
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(absRoot, abs)
		}
		abs = filepath.Clean(abs)

		if !withinRoot(absRoot, abs) {
			return nil, fmt.Errorf("refusing to clean %s: outside project root %s", abs, absRoot)
		}
		if keep := outDirHoldsSources(absRoot, abs, cfg); keep != "" {
			return nil, fmt.Errorf("refusing to clean %s: it is or contains %s, which holds your sources (check outDir and --out-dir)", abs, keep)
		}

		info, err := os.Stat(abs)
		if err != nil {
			if os.IsNotExist(err) {
				result.Missing = append(result.Missing, abs)
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", abs, err)
		}
		if !info.IsDir() {
			result.Missing = append(result.Missing, abs)
			continue
		}

		if err := os.RemoveAll(abs); err != nil {
			return nil, fmt.Errorf("removing %s: %w", abs, err)
		}
		result.Removed = append(result.Removed, abs)
	}
	return result, nil
}

// withinRoot reports whether path is root or a descendant of it. It rejects any
// path that escapes the root via `..`, so a misconfigured outDir cannot delete
// an unrelated directory.
func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// outDirHoldsSources returns the first directory holding the user's work that
// dir (an absolute, clean output or cache directory) is or contains: the
// project root, the pages directory or the public directory. An outDir of ".",
// ".." or "src" would otherwise have the build or clean delete the project.
// Empty when dir is safe. Clean and the build's output wipe share this check.
func outDirHoldsSources(absRoot, dir string, cfg *config.Config) string {
	for _, keep := range []string{absRoot, cfg.PagesDir, cfg.PublicDir} {
		if keep == "" {
			continue
		}
		if !filepath.IsAbs(keep) {
			keep = filepath.Join(absRoot, keep)
		}
		keep = filepath.Clean(keep)
		if withinRoot(dir, keep) {
			return keep
		}
	}
	return ""
}
