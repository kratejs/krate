//go:build windows

package build

import "os"

// Windows has no AF_UNIX path ownership check here; the temp dir is per user.
func ownedByCurrentUser(fi os.FileInfo) bool { return true }
