package fsutil

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestWithinDir(t *testing.T) {
	base := t.TempDir()
	cases := []struct {
		name string
		p    string
		want bool
	}{
		{"base", base, true},
		{"child", filepath.Join(base, "a", "b"), true},
		{"parent", filepath.Dir(base), false},
		{"escape", filepath.Join(base, "..", "escape"), false},
		{"sibling", base + "-sibling", false},
	}
	for _, c := range cases {
		if got := WithinDir(base, c.p); got != c.want {
			t.Errorf("%s: WithinDir(%q, %q) = %v, want %v", c.name, base, c.p, got, c.want)
		}
	}
}

// Parallel page builds write the same content-hashed chunk at once; every
// writer must succeed (on Windows a rename onto a file being replaced fails).
func TestWriteFileIfChangedConcurrentSameContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chunks", "island-loader.abc.js")
	data := []byte("(function(){})();")
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				if err := WriteFileIfChanged(path, data, 0o644); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(data) {
		t.Fatalf("content %q, %v", got, err)
	}
}
