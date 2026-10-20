package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

func TestDiskCacheInvalidatesOnCSSChange(t *testing.T) {
	t.Setenv("KRATE_NO_BUILD_CACHE", "")
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", `import './style.css';
export default function Page() { return <div class="probe">hi</div>; }
`)
	writeFileRel(t, root, "src/pages/style.css", ".probe{color:red}")
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false

	build := func() string {
		t.Helper()
		if err := New(root, cfg).BuildAll(); err != nil {
			t.Fatalf("BuildAll: %v", err)
		}
		html, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
		if err != nil {
			t.Fatal(err)
		}
		m := regexp.MustCompile(`href="?(/styles\.[a-z0-9]+\.css)`).FindStringSubmatch(string(html))
		if len(m) != 2 {
			t.Fatalf("no stylesheet link in HTML:\n%s", html)
		}
		return m[1]
	}

	first := build()
	writeFileRel(t, root, "src/pages/style.css", ".probe{color:blue}")
	second := build()
	if first == second {
		t.Fatalf("CSS hash did not change after editing the stylesheet: %s", first)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, strings.TrimPrefix(second, "/")))
	if err != nil {
		t.Fatalf("new stylesheet missing: %v", err)
	}
	if !strings.Contains(string(data), "blue") {
		t.Fatalf("new stylesheet is stale: %s", data)
	}
}

func TestDevIncrementalCSSRebuild(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "src", "pages", "index.tsx")
	cssPath := filepath.Join(root, "src", "pages", "style.css")
	writeFileRel(t, root, "src/pages/index.tsx", `import './style.css';
export default function Page() { return <div class="probe">hi</div>; }
`)
	writeFileRel(t, root, "src/pages/style.css", ".probe{color:red}")
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	b := New(root, cfg)
	b.DevMode = true
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	writeFileRel(t, root, "src/pages/style.css", ".probe{color:green}")
	pages := b.affectedPages([]string{cssPath})
	if len(pages) != 1 || pages[0] != page {
		t.Fatalf("affectedPages(%s) = %v, want [%s]", cssPath, pages, page)
	}
	if _, err := b.BuildPages(pages); err != nil {
		t.Fatalf("BuildPages: %v", err)
	}
	html, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`href="?(/styles\.[a-z0-9]+\.css)`).FindStringSubmatch(string(html))
	if len(m) != 2 {
		t.Fatalf("no stylesheet link in rebuilt HTML:\n%s", html)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, strings.TrimPrefix(m[1], "/")))
	if err != nil {
		t.Fatalf("linked stylesheet missing: %v", err)
	}
	if !strings.Contains(string(data), "green") {
		t.Fatalf("incremental rebuild wrote stale CSS: %s", data)
	}
}

func TestDevIncrementalLayoutCSSRebuild(t *testing.T) {
	root := t.TempDir()
	layoutCSS := filepath.Join(root, "src", "pages", "layout.css")
	writeFileRel(t, root, "src/pages/_layout.tsx", `import './layout.css';
export default function Layout(props: any) { return <div class="shell">{props.children}</div>; }
`)
	writeFileRel(t, root, "src/pages/index.tsx", `export default function Page() { return <div class="probe">hi</div>; }
`)
	writeFileRel(t, root, "src/pages/layout.css", ".shell{color:red}")
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	b := New(root, cfg)
	b.DevMode = true
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	writeFileRel(t, root, "src/pages/layout.css", ".shell{color:green}")
	pages := b.affectedPages([]string{layoutCSS})
	if len(pages) != 1 {
		t.Fatalf("affectedPages(%s) = %v, want the wrapped page", layoutCSS, pages)
	}
	if _, err := b.BuildPages(pages); err != nil {
		t.Fatalf("BuildPages: %v", err)
	}
	html, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`href="?(/styles\.[a-z0-9]+\.css)`).FindStringSubmatch(string(html))
	if len(m) != 2 {
		t.Fatalf("no stylesheet link in rebuilt HTML:\n%s", html)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, strings.TrimPrefix(m[1], "/")))
	if err != nil {
		t.Fatalf("linked stylesheet missing: %v", err)
	}
	if !strings.Contains(string(data), "green") {
		t.Fatalf("layout CSS rebuild wrote stale CSS: %s", data)
	}
}
