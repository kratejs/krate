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

func TestExportsNullBlocksAndPrivateSubpaths(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "node_modules", "guarded")
	writeFile(t, filepath.Join(pkg, "package.json"), `{"exports":{"./*":"./lib/*.js","./internal/*":null,"./x":{"browser":null,"default":"./lib/x.js"}}}`)
	writeFile(t, filepath.Join(pkg, "lib", "ok.js"), "")
	writeFile(t, filepath.Join(pkg, "lib", "x.js"), "")
	writeFile(t, filepath.Join(pkg, "lib", "internal", "secret.js"), "")
	if got := NodeModule(root, "guarded/ok"); got == "" {
		t.Error("ok should resolve")
	}
	if got := NodeModule(root, "guarded/internal/secret"); got != "" {
		t.Errorf("null target must block, got %q", got)
	}
	if got := NodeModuleWith(root, "guarded", ClientConditions(false)); got != "" {
		t.Errorf("no root export, got %q", got)
	}
	if got := NodeModuleWith(root, "guarded/x", ClientConditions(false)); got != "" {
		t.Errorf("browser:null must block the browser build, got %q", got)
	}
	if got := NodeModuleWith(root, "guarded/x", ServerConditions(false)); got == "" {
		t.Error("server should still resolve x")
	}
}
