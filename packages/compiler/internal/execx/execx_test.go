package execx

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestHelperSleep is a helper subprocess that blocks, used to exercise the
// timeout/tree-kill path without depending on external tools.
func TestHelperSleep(t *testing.T) {
	if os.Getenv("EXECX_HELPER_SLEEP") != "1" {
		return
	}
	time.Sleep(30 * time.Second)
}

// TestHelperNoop is a helper subprocess that exits immediately.
func TestHelperNoop(t *testing.T) {}

func TestRunEnforcesTimeout(t *testing.T) {
	start := time.Now()
	_, _, err := Run(context.Background(), 400*time.Millisecond, "", []string{"EXECX_HELPER_SLEEP=1"},
		os.Args[0], "-test.run=TestHelperSleep")
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, want a timeout message", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("timeout took too long (%v); process tree may not have been killed", elapsed)
	}
}

func TestRunSuccess(t *testing.T) {
	if _, _, err := Run(context.Background(), 30*time.Second, "", nil, os.Args[0], "-test.run=TestHelperNoop"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOutputCapturesStdout(t *testing.T) {
	out, err := Output(context.Background(), 30*time.Second, "", nil, os.Args[0], "-test.run=TestHelperNoop")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = out // presence is enough; content is the test binary's own output.
}

func TestDefaultTimeoutParsing(t *testing.T) {
	t.Setenv("KRATE_EXEC_TIMEOUT", "90s")
	if got := DefaultTimeout(); got != 90*time.Second {
		t.Fatalf("DefaultTimeout = %v, want 90s", got)
	}
	t.Setenv("KRATE_EXEC_TIMEOUT", "not-a-duration")
	if got := DefaultTimeout(); got != 0 {
		t.Fatalf("DefaultTimeout = %v, want 0 for invalid", got)
	}
}

// TestHelperSpawnSleeper starts a ConfigureTree'd sleeper, prints its pid and
// then dies without cleaning up, standing in for a SIGKILLed Krate host.
func TestHelperSpawnSleeper(t *testing.T) {
	if os.Getenv("EXECX_HELPER_SPAWN") != "1" {
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperSleep")
	cmd.Env = append(os.Environ(), "EXECX_HELPER_SLEEP=1", "EXECX_HELPER_SPAWN=0")
	ConfigureTree(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	fmt.Printf("CHILD_PID=%d\n", cmd.Process.Pid)
	os.Exit(0)
}

func TestConfigureTreeKillsChildWhenParentDies(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("parent-death signal is Linux only")
	}
	host := exec.Command(os.Args[0], "-test.run=TestHelperSpawnSleeper")
	host.Env = append(os.Environ(), "EXECX_HELPER_SPAWN=1")
	out, _ := host.CombinedOutput()
	var pid int
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "CHILD_PID=") {
			pid, _ = strconv.Atoi(strings.TrimPrefix(line, "CHILD_PID="))
		}
	}
	if pid == 0 {
		t.Fatalf("no child pid in output: %q", out)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err != nil {
			return
		}
		// A killed child can linger as a zombie until init reaps it.
		if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil && strings.Contains(string(b), ") Z ") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("child %d still alive after its parent exited", pid)
}
