package hosttransport

import (
	"os"
	"path/filepath"
	"strconv"
)

// ShortSocketDir returns a private directory whose path is short enough for a
// socket named file (at most max bytes in all), or "" when there is none. A
// sandboxed macOS app has a long cache and temp path inside its container
// (~/Library/Containers/<id>/Data/...), over the 104-byte sun_path limit, but
// its home directory is the container's Data folder, which is short enough.
func ShortSocketDir(file string, max int) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	dir := filepath.Join(home, ".krate-"+strconv.Itoa(os.Getpid()))
	if len(filepath.Join(dir, file)) > max {
		return ""
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	return dir
}
