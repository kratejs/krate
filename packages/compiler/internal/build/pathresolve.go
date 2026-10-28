package build

import "path/filepath"

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
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

// samePath reports whether two module paths name the same file, tolerating a
// symlinked project root on one side.
func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	return canonicalPath(a) == canonicalPath(b)
}
