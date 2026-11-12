//go:build windows

package build

import "os"

// dumpGoStacks has no SIGQUIT to send on Windows.
func dumpGoStacks(*os.Process) bool { return false }
