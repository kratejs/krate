package build

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// maxUnixSocketPath is a conservative bound under both the Linux (108) and
// macOS (104) sockaddr_un path limits.
const maxUnixSocketPath = 100

// goAPISocketPath returns the AF_UNIX socket path for the Go API host, or ""
// when no usable path fits the OS limit (the host then uses a loopback port).
// The project-local path is preferred; a long project path falls back to a
// short, per-project directory under the system temp dir.
func goAPISocketPath(root string) string {
	local := filepath.Join(root, ".krate", "run", "goapi.sock")
	if len(local) <= maxUnixSocketPath {
		if err := os.MkdirAll(filepath.Dir(local), 0o700); err == nil && privateDir(filepath.Dir(local)) {
			return local
		}
	}
	sum := sha256.Sum256([]byte(root))
	short := filepath.Join(os.TempDir(), "krate-"+hex.EncodeToString(sum[:5]), "goapi.sock")
	if len(short) > maxUnixSocketPath {
		return ""
	}
	if err := os.MkdirAll(filepath.Dir(short), 0o700); err != nil || !privateDir(filepath.Dir(short)) {
		return ""
	}
	return short
}

// privateDir reports whether dir is a real directory (not a symlink) owned by
// the current user and closed to everyone else. MkdirAll succeeds silently on a
// directory that already exists, and the short path under the shared temp dir
// is predictable, so another local user could pre-create it and then serve the
// socket the host talks to.
func privateDir(dir string) bool {
	fi, err := os.Lstat(dir)
	if err != nil || !fi.IsDir() || !ownedByCurrentUser(fi) {
		return false
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return false
		}
		fi, err = os.Lstat(dir)
		if err != nil || fi.Mode().Perm()&0o077 != 0 {
			return false
		}
	}
	return true
}
