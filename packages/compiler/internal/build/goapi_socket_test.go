//go:build !windows

package build

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateDir(t *testing.T) {
	base := t.TempDir()

	own := filepath.Join(base, "own")
	if err := os.Mkdir(own, 0o755); err != nil {
		t.Fatal(err)
	}
	if !privateDir(own) {
		t.Error("an own directory with loose permissions should be tightened and accepted")
	}
	if fi, _ := os.Stat(own); fi.Mode().Perm() != 0o700 {
		t.Errorf("mode = %v, want 0700", fi.Mode().Perm())
	}

	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if privateDir(link) {
		t.Error("a symlinked directory must be refused")
	}
	if privateDir(filepath.Join(base, "missing")) {
		t.Error("a missing directory is not private")
	}
}

// A directory owned by someone else (pre-created in the shared temp dir) must
// not be used for the socket. /proc is owned by root and is not ours.
func TestPrivateDirRefusesForeignOwner(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root")
	}
	if !func() bool { _, err := os.Stat("/etc"); return err == nil }() {
		t.Skip("no /etc")
	}
	if privateDir("/etc") {
		t.Error("a root-owned directory must be refused")
	}
}
