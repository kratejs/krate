package resolver

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveExportsImportCondition(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "node_modules", "radix-ui")
	writeFile(t, filepath.Join(pkg, "package.json"), `{
		"main": "./dist/index.js",
		"module": "./dist/index.mjs",
		"exports": {
			".": {
				"import": { "default": "./dist/index.mjs" },
				"require": { "default": "./dist/index.js" }
			}
		}
	}`)
	writeFile(t, filepath.Join(pkg, "dist", "index.mjs"), "export const x = 1;")
	writeFile(t, filepath.Join(pkg, "dist", "index.js"), "module.exports = {};")

	got := NodeModule(root, "radix-ui")
	if filepath.Base(got) != "index.mjs" {
		t.Errorf("expected index.mjs (import condition), got %q", got)
	}
}

func TestResolveExportsSubpathWildcard(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "node_modules", "radix-ui")
	writeFile(t, filepath.Join(pkg, "package.json"), `{
		"exports": {
			"./*": { "import": { "default": "./dist/*.mjs" } }
		}
	}`)
	writeFile(t, filepath.Join(pkg, "dist", "popover.mjs"), "export const Popover = {};")

	got := NodeModule(root, "radix-ui/popover")
	if filepath.Base(got) != "popover.mjs" {
		t.Errorf("expected popover.mjs, got %q", got)
	}
}

func TestResolveScopedSubpathNoExports(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "node_modules", "@radix-ui", "react-slot")
	writeFile(t, filepath.Join(pkg, "package.json"), `{"module": "./dist/index.mjs"}`)
	writeFile(t, filepath.Join(pkg, "dist", "index.mjs"), "export const Slot = {};")

	got := NodeModule(root, "@radix-ui/react-slot")
	if filepath.Base(got) != "index.mjs" {
		t.Errorf("expected index.mjs, got %q", got)
	}
}

func TestResolveExportsStringTarget(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "node_modules", "tiny")
	writeFile(t, filepath.Join(pkg, "package.json"), `{"exports": "./entry.mjs"}`)
	writeFile(t, filepath.Join(pkg, "entry.mjs"), "export default 1;")

	got := NodeModule(root, "tiny")
	if filepath.Base(got) != "entry.mjs" {
		t.Errorf("expected entry.mjs, got %q", got)
	}
}

func TestResolveExtensionlessSubpath(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "node_modules", "lib")
	writeFile(t, filepath.Join(pkg, "package.json"), `{}`)
	writeFile(t, filepath.Join(pkg, "util.mjs"), "export const u = 1;")

	got := NodeModule(root, "lib/util")
	if filepath.Base(got) != "util.mjs" {
		t.Errorf("expected util.mjs, got %q", got)
	}
}
