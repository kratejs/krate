package fs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/internal/sdktest"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
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

func TestReadWriteInsideRoot(t *testing.T) {
	f := newFixture(t)
	if err := f.sc.WriteText("a.txt", "hello"); err != nil { // relative to the first root
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(f.root, "a.txt")); err != nil || string(b) != "hello" {
		t.Fatalf("relative path did not land in the root: %q %v", b, err)
	}
	if s, err := f.sc.ReadText(filepath.Join(f.root, "a.txt")); err != nil || s != "hello" {
		t.Fatalf("%q %v", s, err)
	}
	bin := []byte{0, 1, 2, 0xff}
	if err := f.sc.WriteFile("sub.bin", bin); err != nil {
		t.Fatal(err)
	}
	if got, err := f.sc.ReadFile("sub.bin"); err != nil || !bytes.Equal(got, bin) {
		t.Fatalf("%v %v", got, err)
	}
	if err := f.sc.Mkdir("d/e/f"); err != nil {
		t.Fatal(err)
	}
	if err := f.sc.WriteText("d/e/f/x.txt", "x"); err != nil {
		t.Fatal(err)
	}
	list, err := f.sc.ReadDir("d/e/f")
	if err != nil || len(list) != 1 || list[0].Name != "x.txt" || list[0].Size != 1 || list[0].IsDir {
		t.Fatalf("%+v %v", list, err)
	}
	st, err := f.sc.Stat("d/e")
	if err != nil || !st.IsDir {
		t.Fatalf("%+v %v", st, err)
	}
	if ok, err := f.sc.Exists("d/e/f/x.txt"); !ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	if ok, err := f.sc.Exists("nope"); ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	if err := f.sc.Rename("a.txt", "b.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sc.ReadText("a.txt"); code(err) != "not_found" {
		t.Fatalf("after rename: %v", err)
	}
	if err := f.sc.Remove("d", false); err == nil {
		t.Fatal("removing a non-empty directory without recursive succeeded")
	}
	if err := f.sc.Remove("d", true); err != nil {
		t.Fatal(err)
	}
	if err := f.sc.Remove("d", true); err != nil {
		t.Fatalf("recursive remove of a missing path: %v", err)
	}
	if err := f.sc.Remove("b.txt", false); err != nil {
		t.Fatal(err)
	}
	if err := f.sc.Remove("b.txt", false); code(err) != "not_found" {
		t.Fatalf("remove missing: %v", err)
	}
}

func TestTraversalIsDenied(t *testing.T) {
	f := newFixture(t)
	secret := filepath.Join(f.outside, "secret.txt")
	os.WriteFile(secret, []byte("s"), 0o644)
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
	os.MkdirAll(sibling, 0o755)
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
	os.WriteFile(secret, []byte("s"), 0o644)
	if err := os.Symlink(f.outside, filepath.Join(f.root, "dirlink")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	os.Symlink(secret, filepath.Join(f.root, "filelink"))
	os.Symlink(filepath.Join("..", "outside"), filepath.Join(f.root, "rellink"))

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
	os.WriteFile(filepath.Join(f.root, "real.txt"), []byte("r"), 0o644)
	os.Symlink(filepath.Join(f.root, "real.txt"), filepath.Join(f.root, "ok"))
	if s, err := f.sc.ReadText("ok"); err != nil || s != "r" {
		t.Fatalf("%q %v", s, err)
	}
}

func TestGrant(t *testing.T) {
	f := newFixture(t)
	picked := filepath.Join(f.outside, "picked.txt")
	os.WriteFile(picked, []byte("p"), 0o644)
	other := filepath.Join(f.outside, "other.txt")
	os.WriteFile(other, []byte("o"), 0o644)

	if _, err := f.sc.ReadText(picked); code(err) != "permission_denied" {
		t.Fatalf("before the grant: %v", err)
	}
	if err := f.sc.Grant(picked); err != nil {
		t.Fatal(err)
	}
	if s, err := f.sc.ReadText(picked); err != nil || s != "p" {
		t.Fatalf("after the grant: %q %v", s, err)
	}
	if err := f.sc.WriteText(picked, "p2"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sc.ReadText(other); code(err) != "permission_denied" {
		t.Fatalf("a file grant must not cover its siblings: %v", err)
	}
	if _, err := f.sc.ReadText(filepath.Join(picked, "x")); err == nil {
		t.Fatal("a file grant covered a path below it")
	}
	f.sc.Revoke(picked)
	if _, err := f.sc.ReadText(picked); code(err) != "permission_denied" {
		t.Fatalf("after revoke: %v", err)
	}

	// A directory grant covers the subtree, not the parent.
	dir := filepath.Join(f.outside, "proj")
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "a.go"), []byte("a"), 0o644)
	if err := f.sc.Grant(dir); err != nil {
		t.Fatal(err)
	}
	if s, err := f.sc.ReadText(filepath.Join(dir, "src", "a.go")); err != nil || s != "a" {
		t.Fatalf("%q %v", s, err)
	}
	if err := f.sc.WriteText(filepath.Join(dir, "new.txt"), "n"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sc.ReadText(other); code(err) != "permission_denied" {
		t.Fatalf("a dir grant leaked to its parent: %v", err)
	}
	if err := f.sc.Remove(dir, true); code(err) != "bad_args" {
		t.Fatalf("removing a granted root: %v", err)
	}

	// Granting a path that does not exist yet (a save dialog) works when its
	// parent exists.
	fresh := filepath.Join(f.outside, "saved.txt")
	if err := f.sc.Grant(fresh); err != nil {
		t.Fatal(err)
	}
	if err := f.sc.WriteText(fresh, "w"); err != nil {
		t.Fatal(err)
	}
	if err := f.sc.Grant(filepath.Join(f.outside, "no", "such", "x")); err == nil {
		t.Fatal("granted a path with no existing parent")
	}
	if err := f.sc.Grant(""); err == nil {
		t.Fatal("empty grant accepted")
	}
}

func TestGrantThroughSymlinkGrantsTarget(t *testing.T) {
	f := newFixture(t)
	target := filepath.Join(f.outside, "t.txt")
	os.WriteFile(target, []byte("t"), 0o644)
	link := filepath.Join(f.outside, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	f.sc.Grant(link)
	if s, err := f.sc.ReadText(target); err != nil || s != "t" {
		t.Fatalf("%q %v", s, err)
	}
}

func TestRootsCannotBeRemovedOrMoved(t *testing.T) {
	f := newFixture(t)
	for _, p := range []string{f.root, "."} {
		if err := f.sc.Remove(p, true); code(err) != "bad_args" {
			t.Errorf("Remove(%q): %v", p, err)
		}
		if err := f.sc.Rename(p, "moved"); code(err) != "bad_args" {
			t.Errorf("Rename(%q): %v", p, err)
		}
	}
	if _, err := os.Stat(f.root); err != nil {
		t.Fatal("root vanished")
	}
	f.sc.WriteText("a", "a")
	if err := f.sc.Rename("a", "../outside/a"); code(err) != "permission_denied" {
		t.Errorf("rename out of scope: %v", err)
	}
}

func TestReadLimits(t *testing.T) {
	f := newFixture(t)
	f.sc.Mkdir("d")
	if _, err := f.sc.ReadFile("d"); code(err) != "is_directory" {
		t.Errorf("%v", err)
	}
	os.WriteFile(filepath.Join(f.root, "bad.txt"), []byte("a\xffb"), 0o644)
	if s, err := f.sc.ReadText("bad.txt"); err != nil || s != "a�b" {
		t.Errorf("%q %v", s, err)
	}
	if _, err := f.sc.ReadFile("a\x00b"); code(err) != "bad_args" {
		t.Errorf("NUL: %v", err)
	}
	if _, err := f.sc.ReadFile(""); code(err) != "bad_args" {
		t.Errorf("empty: %v", err)
	}
}

func str(s string) func(*wire.Encoder) { return func(e *wire.Encoder) { e.String(s) } }

func TestRegisterThroughCallEndpoint(t *testing.T) {
	f := newFixture(t)
	reg := func(perms ...string) *sdktest.Caller {
		set, err := permission.Parse(perms)
		if err != nil {
			t.Fatal(err)
		}
		r := scheme.NewRegistry()
		RegisterScope(r, set, f.sc)
		return sdktest.New(t, r)
	}

	// No fs permission by default.
	c := reg()
	if r := c.Call("krate/fs.readText", str("a")); r.ErrCode != "permission_denied" || r.ErrData != `{"permission":"fs:read"}` {
		t.Fatalf("%+v", r)
	}

	// fs:read can read but not write.
	os.WriteFile(filepath.Join(f.root, "a"), []byte("hi"), 0o644)
	c = reg("fs:read")
	if r := c.Call("krate/fs.readText", str("a")); r.OK != `"hi"` {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/fs.writeText", func(e *wire.Encoder) { e.String("a"); e.String("x") }); r.ErrCode != "permission_denied" || r.ErrData != `{"permission":"fs"}` {
		t.Fatalf("%+v", r)
	}
	if b, _ := os.ReadFile(filepath.Join(f.root, "a")); string(b) != "hi" {
		t.Fatal("a denied write ran")
	}

	// fs implies fs:read.
	c = reg("fs")
	if r := c.Call("krate/fs.writeText", func(e *wire.Encoder) { e.String("b.txt"); e.String("data") }); r.Failed {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/fs.readText", str("b.txt")); r.OK != `"data"` {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/fs.writeFile", func(e *wire.Encoder) { e.String("c.bin"); e.Bytes([]byte{1, 2, 3}) }); r.Failed {
		t.Fatalf("%+v", r)
	}
	r := c.Call("krate/fs.readFile", str("c.bin"))
	if r.Failed {
		t.Fatalf("%+v", r)
	}
	if got, err := r.OKDec.Bytes(); err != nil || !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("%v %v", got, err)
	}
	if r := c.Call("krate/fs.exists", str("c.bin")); r.OK != "true" {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/fs.mkdir", str("sub/dir")); r.Failed {
		t.Fatalf("%+v", r)
	}
	r = c.Call("krate/fs.readDir", str("."))
	if r.Failed || !strings.Contains(r.OK, `"name":"b.txt"`) || !strings.Contains(r.OK, `"name":"sub","isDir":true`) || !strings.Contains(r.OK, `"modTime":{"$d":`) {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/fs.stat", str("b.txt")); r.Failed || !strings.Contains(r.OK, `"size":4`) {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/fs.rename", func(e *wire.Encoder) { e.String("b.txt"); e.String("b2.txt") }); r.Failed {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/fs.remove", func(e *wire.Encoder) { e.String("sub"); e.Bool(true) }); r.Failed {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/fs.remove", str("b2.txt")); r.Failed {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/fs.readText", str("../outside/x")); r.ErrCode != "permission_denied" || !strings.Contains(r.ErrData, `"reason":"outside_scope"`) {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/fs.readText", str("missing")); r.ErrCode != "not_found" {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/fs.readText", nil); r.ErrCode != "bad_args" {
		t.Fatalf("%+v", r)
	}
}
