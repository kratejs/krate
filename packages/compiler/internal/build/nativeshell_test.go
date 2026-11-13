package build

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeInstall lays out a @krate/core-<platform> package under a node_modules
// directory and points nativeExecutable at the krate link in .bin, the way a
// shell started from node_modules/.bin runs it. It returns the shell path.
func fakeInstall(t *testing.T, withShell bool) string {
	t.Helper()
	base := t.TempDir()
	pkgBin := filepath.Join(base, "node_modules", "@krate", "core-test", "bin")
	if err := os.MkdirAll(pkgBin, 0o755); err != nil {
		t.Fatal(err)
	}
	krate := filepath.Join(pkgBin, "krate")
	if err := os.WriteFile(krate, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	shell := filepath.Join(pkgBin, ShellFileName(runtime.GOOS))
	if withShell {
		if err := os.WriteFile(shell, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(base, "node_modules", ".bin", "krate")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(krate, link); err != nil {
		t.Skip("symlinks unavailable: " + err.Error())
	}
	old := nativeExecutable
	nativeExecutable = func() (string, error) { return link, nil }
	t.Cleanup(func() { nativeExecutable = old })
	t.Setenv(ShellEnv, "")
	real, _ := filepath.EvalSymlinks(shell)
	if real == "" {
		real = shell
	}
	return real
}

func writeFile(t *testing.T, p string) string {
	t.Helper()
	if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLocateNativeShellOrder(t *testing.T) {
	sibling := fakeInstall(t, true)
	// A checkout whose shell sources are absent: reaching it would be an error,
	// so a result proves the sibling came first.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "packages", "native", "cmd", "krate-shell"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "packages", "native", "go.mod"), []byte("module "+nativeGoModulePath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LocateNativeShell(root, "")
	if err != nil || got != sibling {
		t.Fatalf("sibling before checkout: %q %v", got, err)
	}
	flag := writeFile(t, filepath.Join(t.TempDir(), "s"))
	if got, err := LocateNativeShell(root, flag); err != nil || got != flag {
		t.Fatalf("--shell: %q %v", got, err)
	}
	t.Setenv(ShellEnv, flag)
	if got, err := LocateNativeShell(root, ""); err != nil || got != flag {
		t.Fatalf("env: %q %v", got, err)
	}
}
