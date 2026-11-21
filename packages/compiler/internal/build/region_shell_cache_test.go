package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixedRegionShellCacheNeverRereads(t *testing.T) {
	t.Parallel()
	absOut := t.TempDir()
	writeRoute(t, absOut, "/p", cacheTestShell)
	path := filepath.Join(absOut, "p", "index.html")

	c := newFixedRegionShellCache()
	e1, err := c.load(path)
	if err != nil || !e1.ok {
		t.Fatalf("load: %v ok=%v", err, e1 != nil && e1.ok)
	}
	// Rewriting and even removing the file must not matter in production.
	if err := os.WriteFile(path, []byte("<html>changed</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	e2, _ := c.load(path)
	if e2 != e1 {
		t.Error("fixed cache re-parsed a shell that was already loaded")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if e3, err := c.load(path); err != nil || e3 != e1 {
		t.Errorf("fixed cache touched the disk: err=%v", err)
	}
	if _, err := newFixedRegionShellCache().load(path); err == nil {
		t.Error("a missing shell should still error on first load")
	}
}

func TestDevRegionShellCacheDetectsRewrite(t *testing.T) {
	t.Parallel()
	absOut := t.TempDir()
	writeRoute(t, absOut, "/p", cacheTestShell)
	path := filepath.Join(absOut, "p", "index.html")

	c := newRegionShellCache()
	e1, _ := c.load(path)
	// Same size, same mtime: the content hash still catches the rewrite.
	fi, _ := os.Stat(path)
	changed := strings.Replace(cacheTestShell, "<i>a</i>", "<i>z</i>", 1)
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	e2, _ := c.load(path)
	if e2 == e1 || !strings.Contains(e2.shell, "<i>z</i>") {
		t.Error("dev cache served a stale shell after a same-size rewrite")
	}
}

func writeRoute(t *testing.T, absOut, route, shell string) {
	t.Helper()
	p := filepath.Join(absOut, strings.TrimPrefix(route, "/"), "index.html")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(shell), 0o644); err != nil {
		t.Fatal(err)
	}
}

const cacheTestShell = `<!DOCTYPE html><html><body><div id=root>` +
	`<!--suspense:1-1--><i>a</i><!--/suspense:1-1-->` +
	`</div></body></html>`
