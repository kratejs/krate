package app

import (
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestURLsFromArgs(t *testing.T) {
	args := []string{"--flag", "acme://open/42", "file.txt", "OTHER://x", "http://y", "://bad"}
	if got := URLsFromArgs(args, "acme", "other"); !reflect.DeepEqual(got, []string{"acme://open/42", "OTHER://x"}) {
		t.Fatalf("got %v", got)
	}
	if got := URLsFromArgs(args); len(got) != 3 {
		t.Fatalf("any scheme: %v", got)
	}
}

func TestDesktopEntry(t *testing.T) {
	d := DesktopEntry(DesktopOptions{Name: "Acme", Exec: "/opt/acme app/acme", Icon: "acme", Schemes: []string{"web+acme", "acme"}, StartupWMClass: "com.acme.app"})
	for _, want := range []string{
		"[Desktop Entry]\n", "Name=Acme\n", `Exec="/opt/acme app/acme" %u` + "\n", "Icon=acme\n",
		"MimeType=x-scheme-handler/acme;x-scheme-handler/web+acme;\n", "StartupWMClass=com.acme.app\n", "Terminal=false\n", "Categories=Utility;\n",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("missing %q in\n%s", want, d)
		}
	}
	if d := DesktopEntry(DesktopOptions{Name: "Plain", Exec: "plain"}); strings.Contains(d, "MimeType") || strings.Contains(d, "%u") {
		t.Fatalf("no schemes, yet:\n%s", d)
	}
}

func TestSingleInstance(t *testing.T) {
	dir := t.TempDir()
	got := make(chan SecondInstance, 2)
	first, ok, err := Acquire(dir, "com.acme.app", []string{"a"}, func(si SecondInstance) { got <- si })
	if err != nil || !ok {
		t.Fatalf("first = %v, %v", ok, err)
	}
	defer first.Release()
	inst, ok, err := Acquire(dir, "com.acme.app", []string{"--x", "acme://open/1"}, nil)
	if err != nil || ok || inst != nil {
		t.Fatalf("second = %v, %v, %v", inst, ok, err)
	}
	select {
	case si := <-got:
		if !reflect.DeepEqual(si.Args, []string{"--x", "acme://open/1"}) || !reflect.DeepEqual(si.URLs, []string{"acme://open/1"}) || si.Dir == "" {
			t.Fatalf("second instance event %+v", si)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the first instance heard nothing")
	}
	// Another app id is independent.
	other, ok, err := Acquire(dir, "com.other.app", nil, nil)
	if err != nil || !ok {
		t.Fatalf("other app: %v %v", ok, err)
	}
	other.Release()
	// After release (or a crash that leaves the file) a new instance is first.
	first.Release()
	again, ok, err := Acquire(dir, "com.acme.app", nil, nil)
	if err != nil || !ok {
		t.Fatalf("after release: %v %v", ok, err)
	}
	again.Release()
}

func TestStaleSocketIsReplaced(t *testing.T) {
	dir := t.TempDir()
	first, _, err := Acquire(dir, "stale.app", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a crash: close the listener without removing the file.
	first.ln.Close()
	again, ok, err := Acquire(dir, "stale.app", nil, nil)
	if err != nil || !ok {
		t.Fatalf("stale socket blocked startup: %v %v", ok, err)
	}
	again.Release()
}

func TestLongPathFallsBackToHash(t *testing.T) {
	p, err := socketPath(t.TempDir()+"/"+strings.Repeat("d", 80), "x")
	if err != nil || len(p) > 100 {
		t.Fatalf("%q %v", p, err)
	}
}

func TestFilesFromArgs(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a, b, sp := mk("a.md"), mk("sub/b.txt"), mk("with space.md")
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{
		"--flag", "-psn_0_1234", "", "a.md", filepath.Join("sub", "b.txt"), a, // relative, absolute, duplicate
		"folder", "missing.md", "acme://open/42", "https://example.com/x.md",
	}
	if runtime.GOOS != "windows" { // a Windows file URL carries a drive letter
		args = append(args, (&url.URL{Scheme: "file", Path: filepath.ToSlash(sp)}).String())
	}
	got := FilesFromArgs(args, dir)
	want := []string{a, b}
	if runtime.GOOS != "windows" {
		want = append(want, sp)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FilesFromArgs = %v\nwant %v", got, want)
	}
	// A relative path with no dir argument resolves against the working directory.
	wd, _ := os.Getwd()
	rel, err := filepath.Rel(wd, a)
	if err == nil {
		if got := FilesFromArgs([]string{rel}, ""); !reflect.DeepEqual(got, []string{a}) {
			t.Errorf("relative to the working directory: %v", got)
		}
	}
}

func TestFileURLPaths(t *testing.T) {
	if p, ok := argPath("file:///tmp/a%20b.md", ""); !ok || p != filepath.FromSlash("/tmp/a b.md") && runtime.GOOS != "windows" {
		t.Errorf("file URL = %q %v", p, ok)
	}
	if _, ok := argPath("file://otherhost/tmp/a.md", ""); ok {
		t.Error("a file URL on another host is not a local file")
	}
	if _, ok := argPath("--x=file:///tmp/a", ""); ok {
		t.Error("a flag is not a path")
	}
	if _, ok := argPath("acme://x", ""); ok {
		t.Error("a deep link is not a path")
	}
}

func TestDesktopEntryFileTypes(t *testing.T) {
	d := DesktopEntry(DesktopOptions{Name: "Acme", Exec: "/usr/bin/acme", MimeTypes: []string{"text/markdown", "application/x-acme"}})
	for _, want := range []string{"Exec=/usr/bin/acme %F\n", "MimeType=text/markdown;application/x-acme;\n"} {
		if !strings.Contains(d, want) {
			t.Errorf("missing %q in\n%s", want, d)
		}
	}
	d = DesktopEntry(DesktopOptions{Name: "Acme", Exec: "/usr/bin/acme", Schemes: []string{"acme"}, MimeTypes: []string{"text/markdown"}})
	for _, want := range []string{"Exec=/usr/bin/acme %U\n", "MimeType=x-scheme-handler/acme;text/markdown;\n"} {
		if !strings.Contains(d, want) {
			t.Errorf("missing %q in\n%s", want, d)
		}
	}
}
