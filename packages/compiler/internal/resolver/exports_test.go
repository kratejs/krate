package resolver

import (
	"os"
	"path/filepath"
	"strings"
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

func TestExportsConditionKeyOrderWins(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "node_modules", "dual")
	writeFile(t, filepath.Join(pkg, "package.json"), `{"exports":{".":{"node":"./node.mjs","browser":"./browser.mjs","default":"./d.mjs"}}}`)
	for _, f := range []string{"node.mjs", "browser.mjs", "d.mjs"} {
		writeFile(t, filepath.Join(pkg, f), "export {}")
	}
	if got := NodeModuleWith(root, "dual", ClientConditions(false)); filepath.Base(got) != "browser.mjs" {
		t.Errorf("client = %q", got)
	}
	if got := NodeModuleWith(root, "dual", ServerConditions(false)); filepath.Base(got) != "node.mjs" {
		t.Errorf("server = %q", got)
	}
	// key order, not a fixed priority list: "default" listed first wins.
	writeFile(t, filepath.Join(pkg, "package.json"), `{"exports":{".":{"default":"./d.mjs","import":"./node.mjs"}}}`)
	ResetCaches()
	if got := NodeModuleWith(root, "dual", ClientConditions(false)); filepath.Base(got) != "d.mjs" {
		t.Errorf("key order = %q", got)
	}
}

func TestExportsDevelopmentProductionConditions(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "node_modules", "modal")
	writeFile(t, filepath.Join(pkg, "package.json"), `{"exports":{".":{"development":"./dev.mjs","production":"./prod.mjs","default":"./d.mjs"}}}`)
	for _, f := range []string{"dev.mjs", "prod.mjs", "d.mjs"} {
		writeFile(t, filepath.Join(pkg, f), "export {}")
	}
	if got := NodeModuleWith(root, "modal", ClientConditions(true)); filepath.Base(got) != "dev.mjs" {
		t.Errorf("dev = %q", got)
	}
	if got := NodeModuleWith(root, "modal", ClientConditions(false)); filepath.Base(got) != "prod.mjs" {
		t.Errorf("prod = %q", got)
	}
}

func TestExportsWildcardLongestPrefixDeterministic(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "node_modules", "wild")
	writeFile(t, filepath.Join(pkg, "package.json"), `{"exports":{"./*":"./generic/*.mjs","./features/*":"./features/*.mjs","./features/special/*":"./special/*.mjs"}}`)
	writeFile(t, filepath.Join(pkg, "generic", "features", "special", "a.mjs"), "")
	writeFile(t, filepath.Join(pkg, "features", "special", "a.mjs"), "")
	writeFile(t, filepath.Join(pkg, "special", "a.mjs"), "")
	for i := 0; i < 30; i++ {
		got := NodeModule(root, "wild/features/special/a")
		if !strings.HasSuffix(filepath.ToSlash(got), "node_modules/wild/special/a.mjs") {
			t.Fatalf("run %d: got %q", i, got)
		}
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

func TestPackageImportsField(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{"imports":{"#util":"./src/util.ts","#lib/*":"./src/lib/*.ts","#dep":"dep-pkg"}}`)
	writeFile(t, filepath.Join(root, "src", "util.ts"), "")
	writeFile(t, filepath.Join(root, "src", "lib", "a.ts"), "")
	writeFile(t, filepath.Join(root, "node_modules", "dep-pkg", "package.json"), `{"main":"i.js"}`)
	writeFile(t, filepath.Join(root, "node_modules", "dep-pkg", "i.js"), "")
	sub := filepath.Join(root, "src", "components")
	if got := PackageImport(sub, "#util", ClientConditions(false)); filepath.Base(got) != "util.ts" {
		t.Errorf("#util = %q", got)
	}
	if got := PackageImport(sub, "#lib/a", ClientConditions(false)); filepath.Base(got) != "a.ts" {
		t.Errorf("#lib/a = %q", got)
	}
	if got := PackageImport(sub, "#dep", ClientConditions(false)); filepath.Base(got) != "i.js" {
		t.Errorf("#dep = %q", got)
	}
	if got := PackageImport(sub, "#nope", ClientConditions(false)); got != "" {
		t.Errorf("#nope = %q", got)
	}
}
