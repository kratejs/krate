package build

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// Parallel pages share one generated boundary. Each replace of that file can
// fail another page's open on Windows, so concurrent callers must write it once.
func TestSegmentGenWrittenOnceAcrossParallelPages(t *testing.T) {
	root := t.TempDir()
	b := New(root, config.Default())
	path := filepath.Join(root, ".krate", "segments", "abc", "boundary.tsx")

	const n = 32
	stats := make([]os.FileInfo, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if err := b.writeSegmentGen(path, "export {};\n"); err != nil {
				t.Error(err)
				return
			}
			fi, err := os.Stat(path)
			if err != nil {
				t.Error(err)
				return
			}
			stats[i] = fi
		}(i)
	}
	close(start)
	wg.Wait()
	for _, fi := range stats[1:] {
		if fi == nil || stats[0] == nil || !os.SameFile(stats[0], fi) {
			t.Fatal("the generated file was replaced while pages were reading it")
		}
	}

	// A changed source and a deleted file are both written again.
	if err := b.writeSegmentGen(path, "export const a = 1;\n"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "export const a = 1;\n" {
		t.Errorf("changed source not written: %q", data)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := b.writeSegmentGen(path, "export const a = 1;\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("deleted file not restored: %v", err)
	}
}
