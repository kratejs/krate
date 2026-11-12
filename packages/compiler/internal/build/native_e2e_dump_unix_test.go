//go:build !windows

package build

import (
	"os"
	"syscall"
)

// dumpGoStacks asks a hung krate-shell to print its goroutine stacks to
// stderr (the Go runtime does that on SIGQUIT, then exits).
func dumpGoStacks(p *os.Process) bool { return p.Signal(syscall.SIGQUIT) == nil }
