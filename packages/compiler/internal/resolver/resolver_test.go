package resolver

import (
	"os"
	"path/filepath"
	"testing"
)

func tempRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		return resolved
	}
	return root
}

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
	root := tempRoot(t)
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
	root := tempRoot(t)
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
	root := tempRoot(t)
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
	root := tempRoot(t)
	if got := NodeModule(root, "nope"); got != "" {
		t.Errorf("expected empty for missing package, got %q", got)
	}
	if got := NodeModule(root, "@scope/nope"); got != "" {
		t.Errorf("expected empty for missing scoped package, got %q", got)
	}
}

func TestNodeModuleIndexFallback(t *testing.T) {
	root := tempRoot(t)
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

func TestWorkspaceSymlinkResolvesToRealSourcePath(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "packages", "ui")
	writeFile(t, filepath.Join(ws, "package.json"), `{"name":"ui","exports":{".":"./src/index.tsx"}}`)
	writeFile(t, filepath.Join(ws, "src", "index.tsx"), "export const x = 1")
	app := filepath.Join(root, "apps", "web")
	if err := os.MkdirAll(filepath.Join(app, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(ws, filepath.Join(app, "node_modules", "ui")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	got := NodeModule(app, "ui")
	want, _ := filepath.EvalSymlinks(filepath.Join(ws, "src", "index.tsx"))
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if IsCompiledDependency(got) {
		t.Error("workspace source must not count as a compiled dependency")
	}
}

func TestIsCompiledDependency(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "node_modules", "react-lib", "package.json"), `{"main":"dist/i.js"}`)
	writeFile(t, filepath.Join(root, "node_modules", "krate-lib", "package.json"), `{"krate":{"entry":"x"}}`)
	writeFile(t, filepath.Join(root, "node_modules", "ts-lib", "package.json"), `{}`)
	cases := map[string]bool{
		filepath.Join(root, "node_modules", "react-lib", "dist", "i.js"):    true,
		filepath.Join(root, "node_modules", "react-lib", "dist", "i.tsx"):   false,
		filepath.Join(root, "node_modules", "krate-lib", "dist", "i.js"):    false,
		filepath.Join(root, "node_modules", "@krate", "components", "a.js"): false,
		filepath.Join(root, "src", "a.tsx"):                                 false,
	}
	for p, want := range cases {
		if got := IsCompiledDependency(p); got != want {
			t.Errorf("%s = %v want %v", p, got, want)
		}
	}
}

func TestPackageBrowserField(t *testing.T) {
	root := tempRoot(t)
	writePkg(t, filepath.Join(root, "node_modules", "strfield"), map[string]string{
		"package.json": `{"main": "node.js", "browser": "browser.js"}`,
		"node.js":      "module.exports = 'node';",
		"browser.js":   "module.exports = 'browser';",
	})
	writePkg(t, filepath.Join(root, "node_modules", "objfield"), map[string]string{
		"package.json": `{"main": "./lib/node.js", "browser": {"./lib/node.js": "./lib/web.js", "fs": false}}`,
		"lib/node.js":  "module.exports = 'node';",
		"lib/web.js":   "module.exports = 'web';",
	})
	writePkg(t, filepath.Join(root, "node_modules", "withexports"), map[string]string{
		"package.json": `{"main": "node.js", "browser": "browser.js", "exports": {".": "./node.js"}}`,
		"node.js":      "x",
		"browser.js":   "y",
	})
	nm := func(name string) string { return filepath.Join(root, "node_modules", name) }
	for _, tt := range []struct {
		pkg   string
		conds Conditions
		want  string
	}{
		{"strfield", ClientConditions(false), filepath.Join(nm("strfield"), "browser.js")},
		{"strfield", ServerConditions(false), filepath.Join(nm("strfield"), "node.js")},
		{"objfield", ClientConditions(false), filepath.Join(nm("objfield"), "lib", "web.js")},
		{"objfield", ServerConditions(false), filepath.Join(nm("objfield"), "lib", "node.js")},
		{"withexports", ClientConditions(false), filepath.Join(nm("withexports"), "node.js")},
	} {
		if got := NodeModuleWith(root, tt.pkg, tt.conds); got != tt.want {
			t.Errorf("%s %v = %q, want %q", tt.pkg, tt.conds, got, tt.want)
		}
	}
}

func TestPackageSubpathFileBeatsDirectory(t *testing.T) {
	root := tempRoot(t)
	pkg := filepath.Join(root, "node_modules", "stem")
	writePkg(t, pkg, map[string]string{
		"package.json":  `{"main": "index.js"}`,
		"index.js":      "x",
		"util.js":       "file",
		"util/data.js":  "dir without index",
		"both.js":       "file",
		"both/index.js": "dir",
	})
	for sub, want := range map[string]string{"util": "util.js", "both": "both.js"} {
		if got := NodeModuleWith(root, "stem/"+sub, ClientConditions(false)); got != filepath.Join(pkg, want) {
			t.Errorf("stem/%s = %q, want %q", sub, got, want)
		}
	}
}
