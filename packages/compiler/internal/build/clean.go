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
