package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newDocsFixture(t *testing.T) (root, genDir string, p *DocsPlugin) {
	t.Helper()
	root = t.TempDir()
	genDir = filepath.Join(root, ".krate", "gen", "docs")
	if err := os.MkdirAll(genDir, 0755); err != nil {
		t.Fatal(err)
	}
	return root, genDir, &DocsPlugin{}
}

// writeThemePkg creates a node_modules theme package and returns its root dir.
func writeThemePkg(t *testing.T, root, name string, pkgJSON map[string]interface{}) string {
	t.Helper()
	pkgDir := filepath.Join(root, "node_modules", name)
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(pkgJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if entry, ok := pkgJSON["main"].(string); ok {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(pkgDir, entry)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pkgDir, entry), []byte("export default function T() { return null; }"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if entry, ok := pkgJSON["module"].(string); ok {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(pkgDir, entry)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pkgDir, entry), []byte("export default function T() { return null; }"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return pkgDir
}

func themeOption(spec string) json.RawMessage {
	b, err := json.Marshal(spec)
	if err != nil {
		return nil
	}
	return b
}

func TestResolveDocsThemeNoTheme(t *testing.T) {
	root, _, p := newDocsFixture(t)

	// Nothing set -> no layout at all.
	got, err := p.resolveDocsTheme(root, &DocsPluginOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil theme when nothing set, got %+v", got)
	}

	// Legacy layout alone -> root-relative component path.
	got, err = p.resolveDocsTheme(root, &DocsPluginOptions{Layout: "src/components/docs-layout.tsx"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected a resolved theme for layout-only options")
	}
	wantModule := filepath.Join(root, "src", "components", "docs-layout")
	if got.module != wantModule {
		t.Errorf("layout-only module = %q, want %q", got.module, wantModule)
	}
	if got.spec != "" {
		t.Errorf("layout-only spec = %q, want empty", got.spec)
	}
}

func TestResolveDocsThemeStringPath(t *testing.T) {
	root, _, p := newDocsFixture(t)

	// Relative path works as a layout alias.
	got, err := p.resolveDocsTheme(root, &DocsPluginOptions{
		Layout: "src/components/docs-layout.tsx",
		Theme:  themeOption("./src/components/alt-layout.tsx"),
	})
	if err == nil {
		t.Fatalf("expected conflict error when layout and theme point to different components, got %+v", got)
	}
	if !strings.Contains(err.Error(), `both layout ("src/components/docs-layout.tsx") and theme ("./src/components/alt-layout.tsx")`) {
		t.Errorf("unexpected conflict error: %v", err)
	}

	// Equal path-like theme and layout is allowed (same component).
	got, err = p.resolveDocsTheme(root, &DocsPluginOptions{
		Layout: "src/components/docs-layout.tsx",
		Theme:  themeOption("./src/components/docs-layout.tsx"),
	})
	if err != nil {
		t.Fatalf("expected equal layout/theme to resolve, got %v", err)
	}
	wantModule := filepath.Join(root, "src", "components", "docs-layout")
	if got.module != wantModule {
		t.Errorf("module = %q, want %q", got.module, wantModule)
	}

	// Relative path alone (no layout).
	got, err = p.resolveDocsTheme(root, &DocsPluginOptions{Theme: themeOption("./src/theme.tsx")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.module != filepath.Join(root, "src", "theme") {
		t.Errorf("module = %q, want %q", got.module, filepath.Join(root, "src", "theme"))
	}
}

func TestResolveDocsThemeBareSpecifier(t *testing.T) {
	root, _, p := newDocsFixture(t)

	// Missing package -> error.
	_, err := p.resolveDocsTheme(root, &DocsPluginOptions{Theme: themeOption("missing-theme")})
	if err == nil {
		t.Fatal("expected error for unresolvable theme package")
	}
	if !strings.Contains(err.Error(), `theme package "missing-theme" not found`) {
		t.Errorf("unexpected error: %v", err)
	}

	// Installed package -> bare specifier emitted (module entry from `main`).
	writeThemePkg(t, root, "night-theme", map[string]interface{}{"main": "layout.js"})
	got, err := p.resolveDocsTheme(root, &DocsPluginOptions{Theme: themeOption("night-theme")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.spec != "night-theme" {
		t.Errorf("spec = %q, want %q", got.spec, "night-theme")
	}
	if got.module != "" {
		t.Errorf("module = %q, want empty for bare specifier", got.module)
	}

	// `module` entry is preferred over `main`.
	writeThemePkg(t, root, "@scope/pref-theme", map[string]interface{}{
		"module": "dist/theme.js",
		"main":   "layout.js",
	})
	got, err = p.resolveDocsTheme(root, &DocsPluginOptions{Theme: themeOption("@scope/pref-theme")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.spec != "@scope/pref-theme" {
		t.Errorf("spec = %q, want %q", got.spec, "@scope/pref-theme")
	}

	// Bare specifier with a layout set -> conflict.
	writeThemePkg(t, root, "other-theme", map[string]interface{}{"main": "layout.js"})
	_, err = p.resolveDocsTheme(root, &DocsPluginOptions{
		Layout: "src/components/docs-layout.tsx",
		Theme:  themeOption("other-theme"),
	})
	if err == nil {
		t.Fatal("expected conflict error when layout and bare-specifier theme are both set")
	}
	if !strings.Contains(err.Error(), "both layout") {
		t.Errorf("unexpected conflict error: %v", err)
	}
}

func TestResolveDocsThemeDescriptor(t *testing.T) {
	root, _, p := newDocsFixture(t)

	// Descriptor with an absolute module (as the config bootstrap produces from
	// a file:// URL), with options forwarded.
	absModule := filepath.Join(root, "night-theme", "layout.tsx")
	if err := os.MkdirAll(filepath.Dir(absModule), 0755); err != nil {
		t.Fatal(err)
	}
	desc := DocsThemeDescriptor{
		Name:    "night-theme",
		Module:  absModule,
		Options: map[string]interface{}{"dark": true},
	}
	data, err := json.Marshal(desc)
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.resolveDocsTheme(root, &DocsPluginOptions{Theme: data})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.module != trimComponentExt(absModule) {
		t.Errorf("module = %q, want %q", got.module, trimComponentExt(absModule))
	}
	if got.spec != "" {
		t.Errorf("spec = %q, want empty", got.spec)
	}
	if strings.TrimSpace(string(got.options)) != `{"dark":true}` {
		t.Errorf("options = %s, want {\"dark\":true}", got.options)
	}

	// Descriptor with a root-relative layout path.
	desc2 := DocsThemeDescriptor{Name: "rel-theme", Layout: "./src/components/docs-layout.tsx"}
	data2, _ := json.Marshal(desc2)
	got, err = p.resolveDocsTheme(root, &DocsPluginOptions{Theme: data2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.module != filepath.Join(root, "src", "components", "docs-layout") {
		t.Errorf("module = %q, want %q", got.module, filepath.Join(root, "src", "components", "docs-layout"))
	}

	// Descriptor with a bare module specifier.
	writeThemePkg(t, root, "pkg-theme", map[string]interface{}{"main": "layout.js"})
	desc3 := DocsThemeDescriptor{Name: "pkg-theme", Module: "pkg-theme"}
	data3, _ := json.Marshal(desc3)
	got, err = p.resolveDocsTheme(root, &DocsPluginOptions{Theme: data3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.spec != "pkg-theme" {
		t.Errorf("spec = %q, want %q", got.spec, "pkg-theme")
	}

	// Descriptor with neither module nor layout -> error.
	desc4 := DocsThemeDescriptor{Name: "empty-theme"}
	data4, _ := json.Marshal(desc4)
	_, err = p.resolveDocsTheme(root, &DocsPluginOptions{Theme: data4})
	if err == nil {
		t.Fatal("expected error for descriptor without module or layout")
	}
	if !strings.Contains(err.Error(), `has no module or layout`) {
		t.Errorf("unexpected error: %v", err)
	}

	// Descriptor bare module + layout set -> conflict.
	_, err = p.resolveDocsTheme(root, &DocsPluginOptions{
		Layout: "src/components/docs-layout.tsx",
		Theme:  data3,
	})
	if err == nil {
		t.Fatal("expected conflict error for descriptor module + layout")
	}
}

func TestResolveDocsThemeInvalidInput(t *testing.T) {
	_, _, p := newDocsFixture(t)

	// A number is neither a string theme nor a descriptor.
	_, err := p.resolveDocsTheme(t.TempDir(), &DocsPluginOptions{Theme: json.RawMessage(`42`)})
	if err == nil {
		t.Fatal("expected error for invalid theme JSON shape")
	}
	if !strings.Contains(err.Error(), "theme must be a package name, a component path, or a theme descriptor object") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestResolvedDocsThemeImportSpecifier(t *testing.T) {
	root, genDir, p := newDocsFixture(t)

	// Bare specifier emits as-is.
	got, err := p.resolveDocsTheme(root, &DocsPluginOptions{Theme: themeOption("night-theme")})
	if err == nil && got != nil {
		if spec := got.importSpecifier(genDir); spec != "night-theme" {
			t.Errorf("bare specifier import = %q, want %q", spec, "night-theme")
		}
	}

	// Absolute module becomes a relative, ./-prefixed import from genDir.
	absModule := filepath.Join(root, "src", "components", "docs-layout")
	rel, err := filepath.Rel(genDir, absModule)
	if err != nil {
		t.Fatal(err)
	}
	rel = filepath.ToSlash(rel)
	theme := &resolvedDocsTheme{module: absModule}
	if spec := theme.importSpecifier(genDir); spec != rel {
		t.Errorf("relative import = %q, want %q", spec, rel)
	}
}
