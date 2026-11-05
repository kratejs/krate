//go:build linux

package execx

import "syscall"

// setParentDeathSignal asks the kernel to SIGKILL the child when the thread
// that started it exits. Krate starts sidecars from long-lived goroutines that
// never lock their OS thread, so in practice that is the whole process dying.
func setParentDeathSignal(attr *syscall.SysProcAttr) {
	attr.Pdeathsig = syscall.SIGKILL
}
