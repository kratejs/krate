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
