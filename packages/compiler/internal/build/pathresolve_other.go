//go:build !windows

package build

func canonicalOSPath(path string) string {
	return path
}
