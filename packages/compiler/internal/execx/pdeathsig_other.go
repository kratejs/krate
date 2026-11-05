//go:build !linux && !windows

package execx

import "syscall"

// setParentDeathSignal is a no-op where the kernel has no parent-death signal
// (macOS, BSDs). The Node sidecars watch their parent process instead.
func setParentDeathSignal(attr *syscall.SysProcAttr) {}
