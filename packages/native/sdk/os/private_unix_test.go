//go:build unix

package sdkos

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirsMustBelongToTheUser(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root owns everything it creates")
	}
	// / is a directory owned by root: a stand-in for a directory
	// another local user pre-created at the predictable temp path.
	if err := checkPrivate("/"); err == nil {
		t.Fatal("a directory owned by another user was accepted")
	}
	if err := checkPrivate(t.TempDir()); err != nil {
		t.Fatalf("own directory: %v", err)
	}
	f := filepath.Join(t.TempDir(), "f")
	os.WriteFile(f, nil, 0o600)
	if err := checkPrivate(f); err == nil {
		t.Fatal("a file was accepted as a directory")
	}
}
