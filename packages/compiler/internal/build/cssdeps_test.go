package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func cssTestConfig(root string) *config.Config {
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	return cfg
}

// linkedCSS returns the stylesheet text linked from dist/index.html.
func linkedCSS(t *testing.T, cfg *config.Config) string {
	t.Helper()
	html, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`href="?(/styles\.[a-z0-9]+\.css)`).FindStringSubmatch(string(html))
	if len(m) != 2 {
		t.Fatalf("no stylesheet link in HTML:\n%s", html)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, strings.TrimPrefix(m[1], "/")))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestImportedCSSAndAssetEditsInvalidateDiskCache(t *testing.T) {
	t.Setenv("KRATE_NO_BUILD_CACHE", "")
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", `import './styles/app.css';
export default function Page() { return <div class="deep">hi</div>; }
`)
	writeFileRel(t, root, "src/pages/styles/app.css", `@import "./deep/deep.css";`)
	writeFileRel(t, root, "src/pages/styles/deep/deep.css", `.deep{color:red;background:url(./d.png)}`)
	writeFileRel(t, root, "src/pages/styles/deep/d.png", "ONE")
	cfg := cssTestConfig(root)

	build := func() (string, *Builder) {
		t.Helper()
		b := New(root, cfg)
		if err := b.BuildAll(); err != nil {
			t.Fatalf("BuildAll: %v", err)
		}
		return linkedCSS(t, cfg), b
	}
	first, _ := build()
	if !strings.Contains(first, "color:red") {
		t.Fatalf("first build: %s", first)
	}
	assetRe := regexp.MustCompile(`/assets/d-[a-z0-9]+\.png`)
	oldAsset := assetRe.FindString(first)
	if oldAsset == "" {
		t.Fatalf("asset url not rewritten: %s", first)
	}

	writeFileRel(t, root, "src/pages/styles/deep/deep.css", `.deep{color:blue;background:url(./d.png)}`)
	second, b2 := build()
	if !strings.Contains(second, "color:blue") {
		t.Fatalf("stale CSS after editing an imported file: %s", second)
	}
	if _, misses := b2.diskCache.stats(); misses != 1 {
		t.Fatalf("want one cache miss, got %d", misses)
	}

	writeFileRel(t, root, "src/pages/styles/deep/d.png", "TWO-DIFFERENT")
	third, _ := build()
	newAsset := assetRe.FindString(third)
	if newAsset == "" || newAsset == oldAsset {
		t.Fatalf("asset hash did not follow content: %q -> %q", oldAsset, newAsset)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, strings.TrimPrefix(newAsset, "/")))
	if err != nil || string(data) != "TWO-DIFFERENT" {
		t.Fatalf("asset copy wrong: %v %q", err, data)
	}
}

func TestDevIncrementalImportedCSSRebuild(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "src", "pages", "index.tsx")
	deep := filepath.Join(root, "src", "pages", "deep.css")
	writeFileRel(t, root, "src/pages/index.tsx", `import './app.css';
export default function Page() { return <div class="p">hi</div>; }
`)
	writeFileRel(t, root, "src/pages/app.css", `@import "./deep.css";`)
	writeFileRel(t, root, "src/pages/deep.css", ".p{color:red}")
	cfg := cssTestConfig(root)
	b := New(root, cfg)
	b.DevMode = true
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	pages := b.affectedPages([]string{deep})
	if len(pages) != 1 || pages[0] != page {
		t.Fatalf("affectedPages(deep.css) = %v, want [%s]", pages, page)
	}
}

func TestNewLayoutInvalidatesDiskCache(t *testing.T) {
	t.Setenv("KRATE_NO_BUILD_CACHE", "")
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", `export default function Page() { return <main>PAGE</main>; }
`)
	cfg := cssTestConfig(root)
	build := func() string {
		t.Helper()
		if err := New(root, cfg).BuildAll(); err != nil {
			t.Fatal(err)
		}
		html, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
		if err != nil {
			t.Fatal(err)
		}
		return string(html)
	}
	if strings.Contains(build(), "LAYOUT-MARK") {
		t.Fatal("layout present before it was created")
	}
	writeFileRel(t, root, "src/pages/_layout.tsx", `export default function Layout({ children }) { return <div class="LAYOUT-MARK">{children}</div>; }
`)
	if !strings.Contains(build(), "LAYOUT-MARK") {
		t.Fatal("new _layout.tsx did not invalidate the cached page")
	}
	if err := os.Remove(filepath.Join(root, "src/pages/_layout.tsx")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(build(), "LAYOUT-MARK") {
		t.Fatal("deleted _layout.tsx still wraps the cached page")
	}
}

func TestExternalImportHoistedInPageStylesheet(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", `import './reset.css';
import './fonts.css';
export default function Page() { return <div class="reset">hi</div>; }
`)
	writeFileRel(t, root, "src/pages/reset.css", `@import "./layered.css" layer(base);
.reset{margin:0}`)
	writeFileRel(t, root, "src/pages/layered.css", `.lay{color:red}`)
	writeFileRel(t, root, "src/pages/fonts.css", `@import url("https://fonts.googleapis.com/css2?family=Inter");
.f{font-family:Inter}`)
	cfg := cssTestConfig(root)
	cfg.Minify = true
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatal(err)
	}
	out := linkedCSS(t, cfg)
	imp := strings.Index(out, "@import")
	if imp < 0 {
		t.Fatalf("external @import dropped:\n%s", out)
	}
	if first := strings.Index(out, "{"); first >= 0 && imp > first {
		t.Fatalf("@import follows a rule block, so browsers ignore it:\n%s", out)
	}
	if !strings.Contains(out, "@layer base{") {
		t.Fatalf("layer(base) not honoured:\n%s", out)
	}
}
