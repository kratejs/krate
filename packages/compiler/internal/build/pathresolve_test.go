package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// canonicalPath is called for every module of every page; it must cache, and
// a reset (a new build) must see a symlink that changed in between.
func TestCanonicalPathCachesUntilReset(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	for _, d := range []string{a, b} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(a, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	resetCanonicalCache()
	first := canonicalPath(link)
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(b, link); err != nil {
		t.Fatal(err)
	}
	if got := canonicalPath(link); got != first {
		t.Fatalf("within a build the cached result must be reused: %q then %q", first, got)
	}
	resetCanonicalCache()
	if got := canonicalPath(link); got == first || !strings.HasSuffix(got, "b") {
		t.Fatalf("after a reset the new target must be seen: %q", got)
	}
}
