//go:build !windows

package build

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// dumpGoStacks asks a hung krate-shell to print its goroutine stacks to
// stderr (the Go runtime does that on SIGQUIT, then exits).
func dumpGoStacks(p *os.Process) bool { return p.Signal(syscall.SIGQUIT) == nil }

// logNativeStacks logs the C stacks of every thread of a hung krate-shell on
// Linux when gdb is installed (CI installs it): a hang inside WebKit shows
// only as a cgo call in the goroutine dump. The runners restrict ptrace to
// parents, so gdb runs through passwordless sudo where there is one.
func logNativeStacks(t *testing.T, p *os.Process) {
	if runtime.GOOS != "linux" {
		return
	}
	gdb, err := exec.LookPath("gdb")
	if err != nil {
		return
	}
	args := []string{"-p", strconv.Itoa(p.Pid), "-batch", "-ex", "set pagination off", "-ex", "thread apply all bt 30"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, gdb, args...)
	if sudo, err := exec.LookPath("sudo"); err == nil && exec.Command(sudo, "-n", "true").Run() == nil {
		cmd = exec.CommandContext(ctx, sudo, append([]string{"-n", gdb}, args...)...)
	}
	out, err := cmd.CombinedOutput()
	t.Logf("native stacks of krate-shell (gdb, err %v):\n%s", err, out)
}
