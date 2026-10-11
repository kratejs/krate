package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicCreatesAndOverwrites(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "file.txt")

	if err := WriteFileAtomic(p, []byte("one"), 0o644); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "one" {
		t.Fatalf("content = %q, want one", b)
	}
	if err := WriteFileAtomic(p, []byte("two"), 0o644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "two" {
		t.Fatalf("content = %q, want two", b)
	}
	// No leftover temp files.
	entries, _ := os.ReadDir(filepath.Dir(p))
	for _, e := range entries {
		if e.Name() != "file.txt" {
			t.Errorf("unexpected leftover file: %s", e.Name())
		}
	}
}

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
