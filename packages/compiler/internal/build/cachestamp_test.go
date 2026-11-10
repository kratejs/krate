package build

import (
	"os"
	"path/filepath"
	"testing"

	krateversion "github.com/kratejs/krate/packages/compiler/internal/version"
)

func TestCacheStale(t *testing.T) {
	setVersion := func(v string) {
		old := krateversion.Value
		krateversion.Value = v
		t.Cleanup(func() { krateversion.Value = old })
	}
	setVersion("0.5.0")
	root := t.TempDir()
	if stale, _ := CacheStale(root); stale {
		t.Fatal("a missing cache is not stale")
	}

	c := newBuildDiskCache(root, "cfg", true)
	c.save("page", &diskPageEntry{Key: "k"})
	if stale, why := CacheStale(root); stale {
		t.Fatalf("fresh cache reported stale: %s", why)
	}
	// The stamp is written once, so it survives the next run.
	setVersion("0.5.1")
	c2 := newBuildDiskCache(root, "cfg", true)
	c2.save("page2", &diskPageEntry{Key: "k2"})
	if stale, why := CacheStale(root); !stale || why == "" {
		t.Fatalf("cache filled by 0.5.0 should be stale under 0.5.1 (stale=%v why=%q)", stale, why)
	}
}

func TestCacheStaleFindsCorruptEntries(t *testing.T) {
	root := t.TempDir()
	pages := filepath.Join(root, ".krate", "cache", "build", "pages")
	if err := os.MkdirAll(pages, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pages, "good.json"), []byte(`{"Key":"k"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if stale, _ := CacheStale(root); stale {
		t.Fatal("valid entries are not stale")
	}
	if err := os.WriteFile(filepath.Join(pages, "bad.json"), []byte(`{"Key"`), 0o644); err != nil {
		t.Fatal(err)
	}
	if stale, why := CacheStale(root); !stale || why != "1 unreadable cache entry" {
		t.Fatalf("stale=%v why=%q", stale, why)
	}
}
