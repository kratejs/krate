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
	// Sizes maps each removed directory to the bytes it held.
	Sizes map[string]int64
	// Freed is the total of Sizes.
	Freed int64
}

// CleanOptions picks what Clean removes. The zero value removes the build
// output and the compiler cache.
type CleanOptions struct {
	// Dist removes the build output directory.
	Dist bool
	// Cache removes the compiler cache (.krate/cache).
	Cache bool
	// All removes the build output and the whole .krate directory (types,
	// generated pages, sidecar manifests, logs and the cache).
	All bool
}

// Clean removes the project's generated output: the build output directory
// (dist) and the compiler cache (.krate/cache). See CleanWith to pick.
func Clean(root string, cfg *config.Config) (*CleanResult, error) {
	return CleanWith(root, cfg, CleanOptions{})
}

// CleanWith removes the generated paths chosen by opts. By default (no option
// set) that is dist and .krate/cache; the rest of .krate (types, generated
// pages, sidecar manifests) is regenerated on demand and left in place so a
// clean does not invalidate editor types unnecessarily, unless opts.All is set.
// A path is only removed when it resolves to within the project root, so a
// misconfigured outDir can never delete an unrelated directory.
func CleanWith(root string, cfg *config.Config, opts CleanOptions) (*CleanResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving project root: %w", err)
	}

	if !opts.Dist && !opts.Cache && !opts.All {
		opts.Dist, opts.Cache = true, true
	}
	var targets []string
	if opts.Dist || opts.All {
		targets = append(targets, cfg.OutDir)
	}
	switch {
	case opts.All:
		targets = append(targets, filepath.Join(absRoot, ".krate"))
	case opts.Cache:
		targets = append(targets, filepath.Join(absRoot, ".krate", "cache"))
	}

	result := &CleanResult{Sizes: map[string]int64{}}
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

		size := dirSize(abs)
		if err := os.RemoveAll(abs); err != nil {
			return nil, fmt.Errorf("removing %s: %w", abs, err)
		}
		result.Removed = append(result.Removed, abs)
		result.Sizes[abs] = size
		result.Freed += size
	}
	return result, nil
}

// dirSize adds up the sizes of the regular files under dir. Unreadable entries
// count as zero.
func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
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
