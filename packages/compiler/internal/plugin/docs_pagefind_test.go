package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPagefindUpToDateNeedsTheIndexInOutDir(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "dist")
	hashFile := filepath.Join(root, ".krate", "cache", "pagefind.hash")
	if err := os.MkdirAll(filepath.Dir(hashFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hashFile, []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}
	opts := PagefindOptions{}

	// Same content hash but a fresh dist: the index is gone, so rebuild it.
	if pagefindUpToDate(hashFile, "abc", out, opts) {
		t.Fatal("up to date although dist has no pagefind index")
	}

	idx := filepath.Join(out, "pagefind")
	if err := os.MkdirAll(idx, 0755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"pagefind.js", "pagefind-entry.json"} {
		if err := os.WriteFile(filepath.Join(idx, n), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if !pagefindUpToDate(hashFile, "abc", out, opts) {
		t.Error("unchanged content with its index in dist should skip the indexer")
	}
	if pagefindUpToDate(hashFile, "changed", out, opts) {
		t.Error("changed content must re-run the indexer")
	}

	// A custom output subdirectory is checked, not the default one.
	if pagefindUpToDate(hashFile, "abc", out, PagefindOptions{OutputSubdir: "search-index"}) {
		t.Error("index in the default subdir must not satisfy a custom subdir")
	}
}

func TestPagefindInputHashTracksOptions(t *testing.T) {
	root := t.TempDir()
	content := filepath.Join(root, "docs")
	if err := os.MkdirAll(content, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(content, "a.md"), []byte("# a"), 0644); err != nil {
		t.Fatal(err)
	}
	a := pagefindInputHash(root, "docs", PagefindOptions{})
	b := pagefindInputHash(root, "docs", PagefindOptions{ExcludeSelectors: []string{".nav"}})
	if a == b {
		t.Error("changing indexer options must change the hash")
	}
	if a != pagefindInputHash(root, "docs", PagefindOptions{}) {
		t.Error("hash must be stable for unchanged input")
	}
}
