//go:build !windows

package execx

import (
	"os/exec"
	"syscall"
)

// configureTree starts the child in its own process group so the whole group can
// be signalled when the command is cancelled.
func configureTree(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// KillTree terminates cmd and all of its child processes.
func KillTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if pgid, err := syscall.Getpgid(cmd.Process.Pid); err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
	_ = cmd.Process.Kill()
}
