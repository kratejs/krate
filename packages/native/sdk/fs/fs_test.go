package fs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
)

// fixture: <tmp>/root is the app root, <tmp>/outside is not.
type fixture struct {
	root, outside string
	sc            *Scope
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{root: filepath.Join(tmp, "root"), outside: filepath.Join(tmp, "outside")}
	for _, d := range []string{f.root, f.outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.sc = NewScope(func() ([]string, error) { return []string{f.root}, nil })
	return f
}

func code(err error) string {
	var ke *krate.Error
	if errors.As(err, &ke) {
		return ke.Code
	}
	return ""
}

func TestTraversalIsDenied(t *testing.T) {
	f := newFixture(t)
	secret := filepath.Join(f.outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		"../outside/secret.txt",
		"sub/../../outside/secret.txt",
		secret,
		filepath.Join(f.root, "..", "outside", "secret.txt"),
		filepath.Join(f.root, "x", "..", "..", "outside", "secret.txt"),
		"/etc/passwd",
	}
	if runtime.GOOS == "windows" {
		paths = paths[:5]
	}
	for _, p := range paths {
		if _, err := f.sc.ReadFile(p); code(err) != "permission_denied" {
			t.Errorf("ReadFile(%q): %v", p, err)
		}
		if err := f.sc.WriteText(p, "x"); code(err) != "permission_denied" {
			t.Errorf("WriteText(%q): %v", p, err)
		}
		if _, err := f.sc.Exists(p); code(err) != "permission_denied" {
			t.Errorf("Exists(%q) must not answer: %v", p, err)
		}
	}
	if b, _ := os.ReadFile(secret); string(b) != "s" {
		t.Fatal("a denied write changed the file")
	}
	// A sibling whose name merely starts with the root's name is outside.
	sibling := f.root + "-evil"
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sc.ReadDir(sibling); code(err) != "permission_denied" {
		t.Errorf("prefix sibling: %v", err)
	}
	// The error data names the permission and the offending path.
	_, err := f.sc.ReadFile("../outside/secret.txt")
	var ke *krate.Error
	if !errors.As(err, &ke) {
		t.Fatal(err)
	}
	m := ke.Data.(map[string]string)
	if m["permission"] != "fs" || m["reason"] != "outside_scope" {
		t.Fatalf("%v", m)
	}
}

func TestSymlinkEscapeIsDenied(t *testing.T) {
	f := newFixture(t)
	secret := filepath.Join(f.outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.outside, filepath.Join(f.root, "dirlink")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(secret, filepath.Join(f.root, "filelink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "outside"), filepath.Join(f.root, "rellink")); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{"filelink", "dirlink/secret.txt", "rellink/secret.txt", "dirlink/new.txt"} {
		if _, err := f.sc.ReadFile(p); code(err) != "permission_denied" {
			t.Errorf("ReadFile(%q): %v", p, err)
		}
		if err := f.sc.WriteText(p, "pwn"); code(err) != "permission_denied" {
			t.Errorf("WriteText(%q): %v", p, err)
		}
	}
	if _, err := f.sc.ReadDir("dirlink"); code(err) != "permission_denied" {
		t.Errorf("ReadDir through a link: %v", err)
	}
	if b, _ := os.ReadFile(secret); string(b) != "s" {
		t.Fatal("a write went through a symlink")
	}
	if _, err := os.Stat(filepath.Join(f.outside, "new.txt")); err == nil {
		t.Fatal("a file was created outside through a symlink")
	}

	// Removing a link removes the link, never what it points at.
	if err := f.sc.Remove("filelink", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(f.root, "filelink")); err == nil {
		t.Fatal("link still there")
	}
	if b, _ := os.ReadFile(secret); string(b) != "s" {
		t.Fatal("Remove followed the link")
	}
	if err := f.sc.Remove("dirlink", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatal("recursive Remove followed a directory link")
	}

	// A link that stays inside is fine.
	if err := os.WriteFile(filepath.Join(f.root, "real.txt"), []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.root, "real.txt"), filepath.Join(f.root, "ok")); err != nil {
		t.Fatal(err)
	}
	if s, err := f.sc.ReadText("ok"); err != nil || s != "r" {
		t.Fatalf("%q %v", s, err)
	}
}
