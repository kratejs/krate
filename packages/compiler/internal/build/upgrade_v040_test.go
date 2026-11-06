package build

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/fix"
)

// copyFixtureTree copies a fixture directory into dst.
func copyFixtureTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// installKrateCoreStub gives the project the one package its krate.config.ts
// imports, as `npm install` would.
func installKrateCoreStub(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "node_modules", "@krate", "core")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	pkg := `{"name":"@krate/core","version":"0.5.0","type":"module","main":"./index.js"}`
	idx := "export function defineConfig(c) { return c; }\n"
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(idx), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestUpgradeV040AppTemplate is the upgrade contract. testdata/upgrade-v040-app
// is the create-krate-app template exactly as v0.4.0 shipped it (git archive of
// packages/create-krate-app/templates/app at the tag, minus README, AGENTS.md
// and .gitignore). An existing 0.4 project looks like this, so it has to keep
// building with the current compiler. The one known difference is tsconfig
// baseUrl: the build works and warns once (KR7125), and `krate fix` repairs it.
// A new failure here means a 0.4 project would stop building on upgrade: fix
// the compiler, or document the break in guides/upgrading.md and CHANGELOG.md
// and update this test.
func TestUpgradeV040AppTemplate(t *testing.T) {
	root := t.TempDir()
	copyFixtureTree(t, filepath.Join("testdata", "upgrade-v040-app"), root)
	installKrateCoreStub(t, root)
	// Real projects have the runtime package installed; the hydration bundle
	// is looked up under packages/runtime/dist from the project root.
	ensureRuntimeDist(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "packages")); err != nil {
		t.Fatal(err)
	}

	load := func(wantKR7125 int) *config.Config {
		t.Helper()
		config.Warnings = nil
		cfg, err := config.Load(root)
		if err != nil {
			t.Fatalf("config.Load: %v", err)
		}
		cfg.Resolve(root)
		got := 0
		for _, w := range config.Warnings {
			if strings.Contains(w, "KR7125") && strings.Contains(w, "krate fix --apply") {
				got++
				continue
			}
			t.Errorf("config warning on an unchanged 0.4 config: %s", w)
		}
		if got != wantKR7125 {
			t.Errorf("KR7125 warnings = %d, want %d (%q)", got, wantKR7125, config.Warnings)
		}
		return cfg
	}

	// 1. As shipped, the alias is read from the project root and warns once.
	cfg0 := load(1)
	if err := New(root, cfg0).BuildAll(); err != nil {
		t.Fatalf("0.4 tsconfig (baseUrl ./src with @/* -> ./src/*) must still build: %v", err)
	}
	if home, _ := os.ReadFile(filepath.Join(cfg0.OutDir, "index.html")); !strings.Contains(string(home), "demo-count") {
		t.Errorf("the unfixed 0.4 project built a wrong index.html:\n%s", home)
	}

	// 2. krate fix repairs it, once, and only tsconfig.json changes.
	res, ferr := fix.Run(root, true)
	if ferr != nil {
		t.Fatal(ferr)
	}
	if len(res.Changes) != 1 || res.Changes[0].File != "tsconfig.json" || res.Changes[0].To != `"."` {
		t.Fatalf("fix changes = %+v", res.Changes)
	}

	// 3. The fixed project builds, with the 0.5 output layout.
	cfg := load(0)
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("build after krate fix: %v", err)
	}
	out := cfg.OutDir
	for _, rel := range []string{"index.html", "about/index.html", "404.html", "global.css", "favicon.svg", "robots.txt", ".krate/manifest.json"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err != nil {
			t.Errorf("dist/%s missing: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "manifest.json")); err == nil {
		t.Error("dist/manifest.json exists again; the manifest lives in dist/.krate/ (see the 0.5 upgrade notes)")
	}
	home, _ := os.ReadFile(filepath.Join(out, "index.html"))
	for _, want := range []string{"You just built a Krate app.", "<title>", "demo-count", "counter_"} {
		if !strings.Contains(string(home), want) {
			t.Errorf("index.html lost %q", want)
		}
	}
	notFound, _ := os.ReadFile(filepath.Join(out, "404.html"))
	if !strings.Contains(string(notFound), `noindex`) {
		t.Error("404.html should carry robots noindex (documented in the 0.5 upgrade notes)")
	}

	// 4. A second fix finds nothing: the repair is idempotent.
	res, ferr = fix.Run(root, true)
	if ferr != nil || len(res.Changes) != 0 {
		t.Fatalf("second fix = %+v, %v", res.Changes, ferr)
	}
}
