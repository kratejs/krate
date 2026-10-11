//go:build windows

package execx

import (
	"os/exec"
	"strconv"
)

// configureTree is a no-op on Windows; taskkill terminates descendants.
func configureTree(cmd *exec.Cmd) {}

// KillTree terminates cmd and all of its child processes.
func KillTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	_ = cmd.Process.Kill()
}
