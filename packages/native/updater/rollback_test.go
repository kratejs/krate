package updater

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// appliedFix applies the 1.1.0 delta update and returns the fixture.
func appliedFix(t *testing.T, kind string) *deltaFix {
	t.Helper()
	f := newDeltaFix(t, kind)
	u, err := f.update()
	if err != nil || u == nil {
		t.Fatalf("%v %v", u, err)
	}
	if err := u.Download(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := u.Apply(); err != nil {
		t.Fatal(err)
	}
	f.assertInstalled()
	return f
}

func (f *deltaFix) boot(version string, max int, timeout time.Duration) BootResult {
	f.t.Helper()
	res, err := Boot(BootConfig{StateDir: f.cfg.StateDir, Version: version, MaxStarts: max, Timeout: timeout})
	if err != nil {
		f.t.Fatal(err)
	}
	return res
}

// assertOld checks the 1.0.0 files are back.
func (f *deltaFix) assertOld() {
	f.t.Helper()
	switch f.kind {
	case KindFile:
		if got, _ := os.ReadFile(f.target); string(got) != f.oldFiles[""] {
			f.t.Fatal("AppImage not restored")
		}
	case KindTree:
		for name, body := range f.oldFiles {
			if !strings.HasSuffix(name, "/") && readFile(f.t, filepath.Join(f.target, name)) != body {
				f.t.Fatalf("%s not restored", name)
			}
		}
		if _, err := os.Stat(filepath.Join(f.target, "app", "new.txt")); err == nil {
			f.t.Fatal("a file of the failed version survived the rollback")
		}
	case KindBundle:
		for name, body := range f.oldFiles {
			if !strings.HasSuffix(name, "/") && readFile(f.t, filepath.Join(f.installDir, name)) != body {
				f.t.Fatalf("%s not restored", name)
			}
		}
		if _, err := os.Stat(filepath.Join(f.target, "Contents", "app", "new.txt")); err == nil {
			f.t.Fatal("a file of the failed version survived the rollback")
		}
	}
	left, _ := filepath.Glob(filepath.Join(f.installDir, ".*"))
	for _, l := range left {
		if strings.Contains(l, "failed") || strings.Contains(l, "prev") {
			f.t.Fatalf("leftover %s", l)
		}
	}
}

func TestRollbackAfterFailedFirstStart(t *testing.T) {
	for _, kind := range []string{KindTree, KindBundle, KindFile} {
		t.Run(kind, func(t *testing.T) {
			f := appliedFix(t, kind)
			// First start of 1.1.0: watched, never confirms (it "crashes").
			r := f.boot("1.1.0", 1, time.Hour)
			if r.Guard == nil || r.RolledBack != nil {
				t.Fatalf("first start: %+v", r)
			}
			r.Guard.timer.Stop()
			// The next launch restores 1.0.0.
			r = f.boot("1.1.0", 1, time.Hour)
			if r.RolledBack == nil {
				t.Fatal("no rollback after an unconfirmed first start")
			}
			ev := r.RolledBack
			if ev.Failed != "1.1.0" || ev.Restored != "1.0.0" || !strings.Contains(ev.Reason, "without confirming") {
				t.Fatalf("event %+v", ev)
			}
			if r.Restart == "" {
				t.Fatal("no executable to restart")
			}
			f.assertOld()
			st := LoadState(f.cfg.StateDir)
			if st.Phase != PhaseRolledBack || st.Event == nil || len(st.Failed) != 1 || st.Failed[0] != "1.1.0" || st.Prev != nil {
				t.Fatalf("state %+v", st)
			}
			// The restored version sees the event and the failed version is
			// not offered again.
			if r := f.boot("1.0.0", 1, time.Hour); r.Guard != nil || r.RolledBack != nil {
				t.Fatalf("restored version boot: %+v", r)
			}
			if u, err := f.update(); err != nil || u != nil {
				t.Fatalf("rolled back version offered again: %+v %v", u, err)
			}
			if err := AckRollback(f.cfg.StateDir); err != nil {
				t.Fatal(err)
			}
			if st := LoadState(f.cfg.StateDir); st.Event != nil || st.Phase != PhaseIdle || len(st.Failed) != 1 {
				t.Fatalf("after ack %+v", st)
			}
		})
	}
}

func TestWatchdogTimeoutMakesNextLaunchRollBack(t *testing.T) {
	f := appliedFix(t, KindTree)
	r := f.boot("1.1.0", 5, 30*time.Millisecond)
	if r.Guard == nil {
		t.Fatal("no guard")
	}
	deadline := time.Now().Add(5 * time.Second)
	for LoadState(f.cfg.StateDir).Unhealthy == "" {
		if time.Now().After(deadline) {
			t.Fatal("watchdog never fired")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Even with many starts allowed, the timeout counts.
	r = f.boot("1.1.0", 5, time.Hour)
	if r.RolledBack == nil || !strings.Contains(r.RolledBack.Reason, "did not confirm a healthy start within") {
		t.Fatalf("%+v", r.RolledBack)
	}
	f.assertOld()
}
