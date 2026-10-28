//go:build windows

package build

import "syscall"

// canonicalOSPath expands an 8.3 short path component (RUNNER~1) to its long
// form, so paths produced by EvalSymlinks and esbuild compare equal.
func canonicalOSPath(path string) string {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return path
	}
	buf := make([]uint16, 32768)
	n, err := syscall.GetLongPathName(p, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return path
	}
	return syscall.UTF16ToString(buf[:n])
}
