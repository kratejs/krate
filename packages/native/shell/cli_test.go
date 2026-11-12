package shell

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestIsAppDir(t *testing.T) {
	dir := t.TempDir()
	if IsAppDir(dir) {
		t.Error("a directory without app.json is not an app directory")
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsAppDir(dir) {
		t.Error("a directory with app.json is an app directory")
	}
	f := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsAppDir(f) || IsAppDir(filepath.Join(dir, "missing")) {
		t.Error("a file or a missing path is not an app directory")
	}
}

func TestDropLaunchServicesArgs(t *testing.T) {
	got := DropLaunchServicesArgs([]string{"-psn_0_12345", "--dev", "/tmp/a.md", "-psn_x"})
	if want := []string{"--dev", "/tmp/a.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

type dropCtl struct {
	fakeCtl
	got []string
}

func (d *dropCtl) Drop(paths []string) error { d.got = paths; return nil }

func TestServeControlDrop(t *testing.T) {
	var out strings.Builder
	d := &dropCtl{}
	ServeControl(strings.NewReader("drop /a b/c.txt\x1f/d.txt\ndrop\n"), &out, d)
	if want := []string{"/a b/c.txt", "/d.txt"}; !reflect.DeepEqual(d.got, want) {
		t.Errorf("paths = %q", d.got)
	}
	if got := out.String(); got != "ok\nerror drop needs file paths\n" {
		t.Errorf("replies = %q", got)
	}
	out.Reset()
	ServeControl(strings.NewReader("drop /a\n"), &out, &fakeCtl{})
	if got := out.String(); got != "error drop is not supported here\n" {
		t.Errorf("replies = %q", got)
	}
}

// An executable with an app next to it is that app, distributed: it does not
// take --dev (developer tools and a control channel that runs script in its
// pages) unless the app was built with native.debug, and it does not run
// another app directory. Either would let whoever starts the signed executable
// act as the app: its keychain items, its granted files, its camera and
// microphone consent. A bare krate-shell (no app next to it) is unchanged.
func TestDistributedShellRefusesDevAndForeignApps(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "dist", "MyApp")
	own := DefaultAppDir(exe)
	foreign := filepath.Join(root, "evil")
	for _, d := range []string{own, foreign} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, ManifestFile), []byte(`{"appId":"com.acme.app"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkLaunch(exe, "", true); err == nil || !strings.Contains(err.Error(), "--dev") {
		t.Errorf("--dev in a distributed app: %v", err)
	}
	if err := checkLaunch(exe, foreign, false); err == nil || !strings.Contains(err.Error(), "another app") {
		t.Errorf("a foreign app directory: %v", err)
	}
	if err := checkLaunch(exe, own, false); err != nil {
		t.Errorf("its own app directory: %v", err)
	}
	if err := checkLaunch(exe, "", false); err != nil {
		t.Errorf("a plain start: %v", err)
	}
	// native.debug keeps --dev, never a foreign app.
	if err := os.WriteFile(filepath.Join(own, ManifestFile), []byte(`{"appId":"com.acme.app","debug":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkLaunch(exe, "", true); err != nil {
		t.Errorf("--dev with native.debug: %v", err)
	}
	if err := checkLaunch(exe, foreign, true); err == nil {
		t.Error("a foreign app directory with native.debug")
	}
	// A bare shell runs any app, in dev too.
	bare := filepath.Join(root, "tools", "krate-shell")
	if err := checkLaunch(bare, foreign, true); err != nil {
		t.Errorf("a bare shell: %v", err)
	}
}
