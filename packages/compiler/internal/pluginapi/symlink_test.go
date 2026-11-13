package pluginapi

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// symlinkedProject has <root>/link -> <outside>, where <outside> holds
// secret.txt and lies beside the project, not in it.
func symlinkedProject(t *testing.T) (root, outside string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	base := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(base); err == nil {
		base = resolved
	}
	root = filepath.Join(base, "project")
	outside = filepath.Join(base, "outside")
	for _, dir := range []string{root, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	return root, outside
}

func TestWriteFileToRootRejectsSymlinkEscape(t *testing.T) {
	root, outside := symlinkedProject(t)
	if err := WriteFileToRoot(root, "link/planted.txt", []byte("x")); err == nil {
		t.Error("write through a symlink that leaves the project was allowed")
	}
	if _, err := os.Stat(filepath.Join(outside, "planted.txt")); err == nil {
		t.Error("a file was written outside the project root")
	}
}

func TestResolveRejectsSymlinkEscape(t *testing.T) {
	root, _ := symlinkedProject(t)
	for _, spec := range []string{"./link/secret.txt", filepath.Join(root, "link", "secret.txt"), "link/secret.txt"} {
		if _, err := Resolve(root, spec); err == nil {
			t.Errorf("Resolve(%q) followed a symlink out of the project", spec)
		}
		if _, err := ReadFile(root, spec); err == nil {
			t.Errorf("ReadFile(%q) read a file outside the project", spec)
		}
	}
}
