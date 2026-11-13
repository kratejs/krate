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
