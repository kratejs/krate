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

func TestResolveRelativeAndAbsolute(t *testing.T) {
	root := setupProject(t)

	got, err := Resolve(root, "./src/foo.txt")
	if err != nil {
		t.Fatalf("Resolve relative: %v", err)
	}
	if want := filepath.Join(root, "src", "foo.txt"); got != want {
		t.Errorf("Resolve relative = %q, want %q", got, want)
	}

	abs := filepath.Join(root, "src", "foo.txt")
	got, err = Resolve(root, abs)
	if err != nil {
		t.Fatalf("Resolve absolute: %v", err)
	}
	if got != abs {
		t.Errorf("Resolve absolute = %q, want %q", got, abs)
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

func TestResolveNodeModules(t *testing.T) {
	root := setupProject(t)

	got, err := Resolve(root, "pkg")
	if err != nil {
		t.Fatalf("Resolve bare package: %v", err)
	}
	if want := filepath.Join(root, "node_modules", "pkg", "index.js"); got != want {
		t.Errorf("Resolve pkg = %q, want %q", got, want)
	}

	got, err = Resolve(root, "@scope/theme")
	if err != nil {
		t.Fatalf("Resolve scoped package: %v", err)
	}
	if want := filepath.Join(root, "node_modules", "@scope", "theme", "index.ts"); got != want {
		t.Errorf("Resolve @scope/theme = %q, want %q", got, want)
	}

	got, err = Resolve(root, "pkg/styles/x.css")
	if err != nil {
		t.Fatalf("Resolve package subpath: %v", err)
	}
	if want := filepath.Join(root, "node_modules", "pkg", "styles", "x.css"); got != want {
		t.Errorf("Resolve pkg/styles/x.css = %q, want %q", got, want)
	}
}

func TestReadFile(t *testing.T) {
	root := setupProject(t)
	got, err := ReadFile(root, "./src/foo.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got != "foo content" {
		t.Errorf("ReadFile = %q, want %q", got, "foo content")
	}
	if _, err := ReadFile(root, "does-not-exist.txt"); err == nil {
		t.Error("expected error for a missing file")
	}
}

func TestWriteFileToRoot(t *testing.T) {
	root := setupProject(t)
	if err := WriteFileToRoot(root, "public/generated.txt", []byte("hello")); err != nil {
		t.Fatalf("WriteFileToRoot: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "public", "generated.txt"))
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("written contents = %q, want %q", data, "hello")
	}

	if err := WriteFileToRoot(root, "../escape.txt", []byte("no")); err == nil {
		t.Fatal("expected traversal error")
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(root), "escape.txt")); statErr == nil {
		t.Fatal("WriteFileToRoot escaped the project root")
	}
}
