package build

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDevBundleID(t *testing.T) {
	for in, want := range map[string]string{
		"com.acme.notes":     "com.acme.notes.dev",
		"com.acme.notes.dev": "com.acme.notes.dev",
		"com.acme notes!":    "com.acme-notes.dev",
		"":                   "com.example.app.dev",
		"..":                 "com.example.app.dev",
	} {
		if got := devBundleID(in); got != want {
			t.Errorf("devBundleID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDevBundleName(t *testing.T) {
	for in, want := range map[string]string{
		"Acme Notes": "Acme Notes",
		"a/b:c":      "a-b-c",
		"":           "App",
		"..":         "App",
	} {
		if got := devBundleName(in); got != want {
			t.Errorf("devBundleName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMacDevBundleWanted(t *testing.T) {
	if !macDevBundleWanted("darwin", "darwin", false) {
		t.Error("macOS dev with a window must use a bundle")
	}
	for _, c := range []struct {
		host, target string
		browser      bool
	}{{"linux", "linux", false}, {"darwin", "linux", false}, {"darwin", "darwin", true}} {
		if macDevBundleWanted(c.host, c.target, c.browser) {
			t.Errorf("%+v must not use a bundle", c)
		}
	}
}

func TestPrepareMacDevBundle(t *testing.T) {
	dir := t.TempDir()
	shell := filepath.Join(dir, "krate-shell")
	if err := os.WriteFile(shell, []byte("shell-v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, ".krate", "dev")
	exe, err := prepareMacDevBundle(out, shell, "com.acme.notes", "Acme Notes")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(out, "Acme Notes.app", "Contents", "MacOS", "Acme Notes"); exe != want {
		t.Fatalf("exe = %s, want %s", exe, want)
	}
	data, _ := os.ReadFile(exe)
	if string(data) != "shell-v1" {
		t.Errorf("executable content = %q", data)
	}
	if st, _ := os.Stat(exe); runtime.GOOS != "windows" && st.Mode()&0o111 == 0 {
		t.Error("executable bit missing")
	}
	plist, _ := os.ReadFile(filepath.Join(out, "Acme Notes.app", "Contents", "Info.plist"))
	for _, want := range []string{"<string>com.acme.notes.dev</string>", "<key>CFBundleExecutable</key>\n\t<string>Acme Notes</string>"} {
		if !strings.Contains(string(plist), want) {
			t.Errorf("Info.plist lacks %q:\n%s", want, plist)
		}
	}

	// Unchanged inputs leave the executable alone; a new shell replaces it.
	before, _ := os.Stat(exe)
	if _, err := prepareMacDevBundle(out, shell, "com.acme.notes", "Acme Notes"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(exe)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("an unchanged shell was copied again")
	}
	if err := os.WriteFile(shell, []byte("shell-version-2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareMacDevBundle(out, shell, "com.acme.notes", "Acme Notes"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(exe); string(data) != "shell-version-2" {
		t.Errorf("executable not refreshed: %q", data)
	}
	if _, err := prepareMacDevBundle(out, filepath.Join(dir, "missing"), "x", "y"); err == nil {
		t.Error("a missing shell must be an error")
	}
}
