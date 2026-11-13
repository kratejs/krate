package memstat

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func TestSampleSelfAndChild(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("no implementation on " + runtime.GOOS)
	}
	// A child that stays alive while we sample.
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("ping", "-n", "30", "127.0.0.1")
	} else {
		cmd = exec.Command("sleep", "30")
	}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start a child: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	ps, err := Sample(os.Getpid(), cmd.Process.Pid)
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if len(ps) != 2 {
		t.Fatalf("got %d processes, want 2: %+v", len(ps), ps)
	}
	self, child := ps[0], ps[1]
	if self.PID != os.Getpid() || child.PID != cmd.Process.Pid {
		t.Fatalf("pids out of order: %+v", ps)
	}
	if self.RSS < 1<<20 || self.PSS < 1<<20 {
		t.Errorf("the test process should use over 1 MiB: %+v", self)
	}
	if self.PSS > self.RSS+(1<<20) && runtime.GOOS == "linux" {
		t.Errorf("PSS %d exceeds RSS %d", self.PSS, self.RSS)
	}
	if child.PSS == 0 || child.PSS >= self.PSS*8 {
		t.Errorf("implausible child memory: %+v (self %+v)", child, self)
	}
	if self.Name == "" {
		t.Errorf("no name for the test process")
	}

	// Allocate and touch memory: private dirty must grow.
	if runtime.GOOS == "linux" {
		buf := make([]byte, 64<<20)
		for i := range buf {
			buf[i] = 1
		}
		after, err := Sample(os.Getpid())
		if err != nil {
			t.Fatal(err)
		}
		if after[0].PrivateDirty < self.PrivateDirty+32<<20 {
			t.Errorf("private dirty grew from %d to %d after touching 64 MiB", self.PrivateDirty, after[0].PrivateDirty)
		}
		_ = buf[len(buf)-1]
	}

	kids, err := Descendants(os.Getpid())
	if err != nil {
		t.Fatalf("Descendants: %v", err)
	}
	found := false
	for _, k := range kids {
		if k == cmd.Process.Pid {
			found = true
		}
	}
	if !found {
		t.Errorf("Descendants(%d) = %v, missing child %d", os.Getpid(), kids, cmd.Process.Pid)
	}
}

func TestSampleGonePid(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux semantics")
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skip(err)
	}
	ps, err := Sample(cmd.Process.Pid)
	if err != nil || len(ps) != 0 {
		t.Fatalf("an exited pid should be skipped silently: %v, %+v", err, ps)
	}
}

func TestCollectRoles(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("no implementation on " + runtime.GOOS)
	}
	r, err := Collect([]Target{{os.Getpid(), RoleShell}, {os.Getpid(), RoleGoHost}, {0, "x"}}, "note")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Procs) != 1 || r.Procs[0].Role != RoleShell || r.Total.PSS != r.Procs[0].PSS {
		t.Fatalf("report: %+v", r)
	}
	if r.OS != runtime.GOOS || r.Source != Source() || len(r.Notes) != 1 {
		t.Fatalf("report header: %+v", r)
	}
}
