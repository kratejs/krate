package pluginapi

import (
	"os"
	"path/filepath"
	"testing"
)

// setupProject builds a temp project with:
//
//	<root>/src/foo.txt
//	<root>/node_modules/pkg/index.js
//	<root>/node_modules/pkg/styles/x.css
//	<root>/node_modules/@scope/theme/package.json (module: index.ts)
//	<root>/node_modules/@scope/theme/index.ts
func setupProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/foo.txt", "foo content")
	write("node_modules/pkg/index.js", "module.exports = 1")
	write("node_modules/pkg/styles/x.css", ".x{}")
	write("node_modules/@scope/theme/package.json", `{"module":"index.ts"}`)
	write("node_modules/@scope/theme/index.ts", "export const x = 1")
	return root
}

func TestWithinRoot(t *testing.T) {
	root := t.TempDir()
	if !WithinRoot(root, filepath.Join(root, "a", "b")) {
		t.Error("expected nested path to be within root")
	}
	if !WithinRoot(root, root) {
		t.Error("expected root to be within itself")
	}
	if WithinRoot(root, filepath.Join(root, "..", "escape")) {
		t.Error("expected parent path to be outside root")
	}
}

func TestResolveRejectsTraversal(t *testing.T) {
	root := setupProject(t)
	if _, err := Resolve(root, "../outside.txt"); err == nil {
		t.Fatal("expected traversal error for relative escape")
	}
	outside := filepath.Join(filepath.Dir(root), "outside.txt")
	if _, err := Resolve(root, outside); err == nil {
		t.Fatal("expected traversal error for absolute path outside root")
	}
}
