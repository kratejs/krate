//go:build unix

package sdkos

import (
	"fmt"
	"os"
	"syscall"
)

// checkPrivate makes sure p is a directory owned by this user. The temp
// directory has a predictable name under a shared directory (<tmp>/<id>-<uid>),
// so another local user could create it first, with permissions that let them
// swap its contents; MkdirAll would then use it as it is. A symlink to a
// directory of the user's own is fine.
func checkPrivate(p string) error {
	st, err := os.Stat(p)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("sdkos: %s is not a directory", p)
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok && int(sys.Uid) != os.Getuid() {
		return fmt.Errorf("sdkos: %s belongs to another user; remove it or use another temp directory", p)
	}
	return nil
}
