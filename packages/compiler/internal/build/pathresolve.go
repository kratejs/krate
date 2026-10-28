package build

import (
	"os"
	"path/filepath"
)

// canonicalPath resolves symlinks when the path exists. esbuild canonicalizes
// module paths before handing them to plugins, while the builder keeps paths
// under the project root as given. On macOS /var is a symlink to /private/var,
// so comparing the two forms directly misses client boundaries and server
// exports.
func canonicalPath(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return canonicalOSPath(filepath.Clean(path))
}

// samePath reports whether two module paths name the same file, tolerating a
// symlinked project root on one side.
func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	if canonicalPath(a) == canonicalPath(b) {
		return true
	}
	return sameFileIdentity(a, b)
}

// sameFileIdentity falls back to the filesystem identity when the paths differ
// in spelling (8.3 short names, case, a junction root).
func sameFileIdentity(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}
