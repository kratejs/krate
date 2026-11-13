//go:build windows

package build

import (
	"os"
	"testing"
)

// dumpGoStacks has no SIGQUIT to send on Windows.
func dumpGoStacks(*os.Process) bool { return false }

// logNativeStacks is Linux only.
func logNativeStacks(*testing.T, *os.Process) {}
