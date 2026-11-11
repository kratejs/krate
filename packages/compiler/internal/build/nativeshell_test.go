package build

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestFindPrebuiltShellOrder(t *testing.T) {
	sibling := fakeInstall(t, true)
	dir := t.TempDir()
	flagShell := writeFile(t, filepath.Join(dir, "flag-shell"))
	envShell := writeFile(t, filepath.Join(dir, "env-shell"))
	goos, goarch := runtime.GOOS, runtime.GOARCH

	// Nothing set: the shell next to the executable, found through the
	// symlink in .bin.
	got, src, err := FindPrebuiltShell("", goos, goarch)
	if err != nil || got != sibling || !strings.Contains(src, "next to") {
		t.Fatalf("sibling: %q %q %v (want %s)", got, src, err, sibling)
	}
	// "auto" asks for the same lookup.
	if got, _, _ := FindPrebuiltShell("auto", goos, goarch); got != sibling {
		t.Errorf("auto = %q", got)
	}
	// $KRATE_SHELL beats the sibling.
	t.Setenv(ShellEnv, envShell)
	if got, src, _ := FindPrebuiltShell("", goos, goarch); got != envShell || src != "$KRATE_SHELL" {
		t.Errorf("env: %q %q", got, src)
	}
	// --shell beats $KRATE_SHELL.
	if got, src, _ := FindPrebuiltShell(flagShell, goos, goarch); got != flagShell || src != "--shell" {
		t.Errorf("flag: %q %q", got, src)
	}
	// A path that was given and is missing is an error, not a fallthrough.
	if _, _, err := FindPrebuiltShell(filepath.Join(dir, "nope"), goos, goarch); err == nil {
		t.Error("missing --shell: no error")
	}
	t.Setenv(ShellEnv, filepath.Join(dir, "nope"))
	if _, _, err := FindPrebuiltShell("", goos, goarch); err == nil || !strings.Contains(err.Error(), ShellEnv) {
		t.Errorf("missing $KRATE_SHELL: %v", err)
	}
	// A directory is not a shell.
	if _, _, err := FindPrebuiltShell(dir, goos, goarch); err == nil {
		t.Error("directory accepted as shell")
	}
}

func TestFindPrebuiltShellOtherTargetSkipsSibling(t *testing.T) {
	fakeInstall(t, true)
	other := "arm64"
	if runtime.GOARCH == "arm64" {
		other = "amd64"
	}
	if got, _, err := FindPrebuiltShell("", runtime.GOOS, other); got != "" || err != nil {
		t.Errorf("shell for another architecture: %q %v", got, err)
	}
	if got, _, _ := FindPrebuiltShell("", "plan9", runtime.GOARCH); got != "" {
		t.Errorf("shell for another OS: %q", got)
	}
}

func TestShellFileName(t *testing.T) {
	if ShellFileName("windows") != "krate-shell.exe" || ShellFileName("linux") != "krate-shell" || ShellFileName("darwin") != "krate-shell" {
		t.Error("file names")
	}
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

func TestLocateNativeShellNothingFound(t *testing.T) {
	fakeInstall(t, false)
	_, err := LocateNativeShell(t.TempDir(), "")
	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range []string{"--shell", ShellEnv, "next to the krate executable", "@krate/core"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
}

func TestAssembleShellDist(t *testing.T) {
	out := t.TempDir()
	app := filepath.Join(out, "app")
	if err := os.MkdirAll(filepath.Join(app, "gohost"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(app, "app.json"))
	writeFile(t, filepath.Join(app, "gohost", "krate-gohost"))
	shell := writeFile(t, filepath.Join(t.TempDir(), "krate-shell"))
	exe, err := assembleShellDist(out, app, shell, "Acme: Notes", "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(out, "linux-amd64")
	if filepath.Dir(exe) != dir || filepath.Ext(exe) != "" || strings.ContainsAny(filepath.Base(exe), ":") {
		t.Errorf("exe = %s", exe)
	}
	if st, err := os.Stat(exe); err != nil || st.Mode()&0o111 == 0 {
		t.Errorf("shell not executable: %v", err)
	}
	if st, err := os.Stat(filepath.Join(dir, "app", "gohost", "krate-gohost")); err != nil || st.Mode()&0o111 == 0 {
		t.Errorf("app copy lost the mode of the Go host: %v", err)
	}
	exe, err = assembleShellDist(out, app, shell, "Acme", "windows", "arm64")
	if err != nil || filepath.Base(exe) != "Acme.exe" {
		t.Errorf("windows: %s %v", exe, err)
	}
}

func TestProvideShellUsesTheSibling(t *testing.T) {
	sibling := fakeInstall(t, true)
	b := &Builder{Root: t.TempDir()}
	out := t.TempDir()
	path, note := b.provideShell(out, "", runtime.GOOS, runtime.GOARCH)
	if note != "" || filepath.Base(path) != ShellFileName(runtime.GOOS) {
		t.Fatalf("path %q note %q", path, note)
	}
	if data, _ := os.ReadFile(path); string(data) != "#!/bin/sh\n" {
		t.Errorf("copied %q from %s", data, sibling)
	}
	if _, note := b.provideShell(out, filepath.Join(out, "missing"), runtime.GOOS, runtime.GOARCH); note == "" {
		t.Error("missing shell gave no note")
	}
}
