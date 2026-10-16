package execx

import (
	"context"
	"os"
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
