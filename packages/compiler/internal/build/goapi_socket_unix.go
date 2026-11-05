//go:build !windows

package build

import (
	"os"
	"syscall"
)

func ownedByCurrentUser(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}

// hasUnixPerms reports whether directory mode bits are meaningful.
const hasUnixPerms = true
