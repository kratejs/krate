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
		if err := os.MkdirAll(filepath.Dir(local), 0o700); err == nil {
			return local
		}
	}
	sum := sha256.Sum256([]byte(root))
	short := filepath.Join(os.TempDir(), "krate-"+hex.EncodeToString(sum[:5]), "goapi.sock")
	if len(short) > maxUnixSocketPath {
		return ""
	}
	if err := os.MkdirAll(filepath.Dir(short), 0o700); err != nil {
		return ""
	}
	return short
}
