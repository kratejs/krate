//go:build windows

package build

import "os"

// Windows has no AF_UNIX path ownership check here; the temp dir is per user.
func ownedByCurrentUser(fi os.FileInfo) bool { return true }

// hasUnixPerms is false here: Go reports every directory as 0777, and access
// is governed by ACLs that the per-user temp directory already restricts.
const hasUnixPerms = false
