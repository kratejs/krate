package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestDiskCacheEnabledGates(t *testing.T) {
	t.Setenv("KRATE_NO_BUILD_CACHE", "")
	if !diskCacheEnabled(false, false, false) {
		t.Error("cache should be enabled by default")
	}
	if diskCacheEnabled(true, false, false) {
		t.Error("cache must be disabled when sourcemaps are enabled")
	}
	if diskCacheEnabled(false, true, false) {
		t.Error("cache must be disabled when native plugins have per-page hooks")
	}
	if diskCacheEnabled(false, false, true) {
		t.Error("cache must be disabled when community plugins are configured")
	}
	t.Setenv("KRATE_NO_BUILD_CACHE", "1")
	if diskCacheEnabled(false, false, false) {
		t.Error("cache must be disabled when KRATE_NO_BUILD_CACHE is set")
	}
}

func TestDiskCacheSchemaInvalidates(t *testing.T) {
	c := newBuildDiskCache(t.TempDir(), "cfghash", true)
	before := c.fingerprintFor("content")
	old := diskCacheSchema
	diskCacheSchema = old + ".1"
	after := c.fingerprintFor("content")
	diskCacheSchema = old
	if before == after {
		t.Fatal("fingerprint must change when the cache schema changes")
	}
	if old == "" {
		t.Fatal("diskCacheSchema must be non-empty")
	}
}

func TestDiskCacheReplayAndInvalidation(t *testing.T) {
	t.Setenv("KRATE_NO_BUILD_CACHE", "")

	root := t.TempDir()
	const indexPage = `import { createSignal } from '@krate/runtime';
export default function Page() {
  const [n, setN] = createSignal(0);
  return <div class="p-4"><button onClick={() => setN(n() + 1)}>{n()}</button></div>;
}
`
	writeFileRel(t, root, "src/pages/index.tsx", indexPage)
	writeFileRel(t, root, "src/pages/about.tsx", `export default function About() { return <h1>About</h1>; }`)

	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false

	snapshot := func() (map[string][]byte, *Builder) {
		b := New(root, cfg)
		if err := b.BuildAll(); err != nil {
			t.Fatalf("BuildAll: %v", err)
		}
		files := map[string][]byte{}
		err := filepath.WalkDir(cfg.OutDir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(cfg.OutDir, p)
			data, _ := os.ReadFile(p)
			files[filepath.ToSlash(rel)] = data
			return nil
		})
		if err != nil {
			t.Fatalf("walk dist: %v", err)
		}
		return files, b
	}

	fresh, _ := snapshot()
	cached, b2 := snapshot()

	if b2.diskCache == nil {
		t.Fatal("disk cache not initialised")
	}
	hits, misses := b2.diskCache.stats()
	if hits != 2 || misses != 0 {
		t.Fatalf("second build: got %d hit / %d miss, want 2 / 0", hits, misses)
	}

	if len(fresh) != len(cached) {
		t.Fatalf("file count differs: fresh=%d cached=%d", len(fresh), len(cached))
	}
	for name, want := range fresh {
		if name == "sitemap.xml" {
			continue // lastmod is build-timestamped
		}
		got, ok := cached[name]
		if !ok {
			t.Errorf("replayed build missing %s", name)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("%s: replayed output differs from fresh build", name)
		}
	}

	writeFileRel(t, root, "src/pages/index.tsx", indexPage+"\n")
	_, b3 := snapshot()
	hits, misses = b3.diskCache.stats()
	if hits != 1 || misses != 1 {
		t.Fatalf("after editing index.tsx: got %d hit / %d miss, want 1 / 1", hits, misses)
	}
}
