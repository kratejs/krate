package resolver

import (
	"os"
	"path/filepath"
	"testing"
)

func writePkg(t *testing.T, pkgDir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if dir := filepath.Dir(filepath.Join(pkgDir, name)); dir != pkgDir {
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(pkgDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNodeModuleUnscoped(t *testing.T) {
	root := t.TempDir()
	writePkg(t, filepath.Join(root, "node_modules", "formslib"), map[string]string{
		"package.json": `{"main": "dist/main.js"}`,
		"dist/main.js": "export const x = 1;",
	})
	got := NodeModule(root, "formslib")
	want := filepath.Join(root, "node_modules", "formslib", "dist", "main.js")
	if got != want {
		t.Errorf("NodeModule = %q, want %q", got, want)
	}
}

func TestNodeModuleScopedAtProjectRoot(t *testing.T) {
	root := t.TempDir()
	writePkg(t, filepath.Join(root, "node_modules", "@scope", "theme"), map[string]string{
		"package.json":  `{"module": "src/layout.js", "main": "legacy.js"}`,
		"src/layout.js": "export default () => null;",
		"legacy.js":     "export default () => null;",
	})
	got := NodeModule(root, "@scope/theme")
	want := filepath.Join(root, "node_modules", "@scope", "theme", "src", "layout.js")
	if got != want {
		t.Errorf("NodeModule scoped-at-root = %q, want %q", got, want)
	}
}

func TestNodeModuleHoisted(t *testing.T) {
	// A package nested under the project still finds a theme hoisted to the
	// workspaces root's node_modules (walk-up resolution).
	root := t.TempDir()
	inner := filepath.Join(root, "apps", "docs")
	if err := os.MkdirAll(inner, 0755); err != nil {
		t.Fatal(err)
	}
	writePkg(t, filepath.Join(root, "node_modules", "hoisted-theme"), map[string]string{
		"package.json": `{"main": "layout.js"}`,
		"layout.js":    "export default () => null;",
	})
	got := NodeModule(inner, "hoisted-theme")
	want := filepath.Join(root, "node_modules", "hoisted-theme", "layout.js")
	if got != want {
		t.Errorf("NodeModule hoisted = %q, want %q", got, want)
	}
}

func TestNodeModuleMissing(t *testing.T) {
	root := t.TempDir()
	if got := NodeModule(root, "nope"); got != "" {
		t.Errorf("expected empty for missing package, got %q", got)
	}
	if got := NodeModule(root, "@scope/nope"); got != "" {
		t.Errorf("expected empty for missing scoped package, got %q", got)
	}
}

func TestNodeModuleIndexFallback(t *testing.T) {
	root := t.TempDir()
	// No package.json; index.tsx fallback.
	writePkg(t, filepath.Join(root, "node_modules", "baretheme"), map[string]string{
		"index.tsx": "export default () => null;",
	})
	got := NodeModule(root, "baretheme")
	want := filepath.Join(root, "node_modules", "baretheme", "index.tsx")
	if got != want {
		t.Errorf("NodeModule index fallback = %q, want %q", got, want)
	}
}
