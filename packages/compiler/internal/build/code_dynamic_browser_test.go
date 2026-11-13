package build

import (
	"os"
	"path/filepath"
	"testing"
)

// requireRuntimeDist skips when @krate/runtime is not built (or built before the
// highlighter existed).
func requireRuntimeDist(t *testing.T) string {
	t.Helper()
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"highlight.js", "krate-f-highlight.js", "krate-features.json"} {
		if _, err := os.Stat(filepath.Join(repoRoot, "packages", "runtime", "dist", f)); err != nil {
			requireE2E(t, "the built runtime is missing %s (run pnpm --filter @krate/runtime build)", f)
		}
	}
	return repoRoot
}
