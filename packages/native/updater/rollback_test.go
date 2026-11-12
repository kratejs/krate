package updater

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDecideBoot(t *testing.T) {
	pending := func(starts int, unhealthy string) State {
		return State{Phase: PhasePending, From: "1.0.0", To: "1.1.0", Starts: starts, Unhealthy: unhealthy}
	}
	cases := []struct {
		name    string
		st      State
		running string
		max     int
		want    bootAction
	}{
		{"idle", State{}, "1.1.0", 1, actNone},
		{"rolled back is idle", State{Phase: PhaseRolledBack}, "1.0.0", 1, actNone},
		{"first start", pending(0, ""), "1.1.0", 1, actWatch},
		{"second start after an unconfirmed first", pending(1, ""), "1.1.0", 1, actRollback},
		{"second start allowed with max 3", pending(1, ""), "1.1.0", 3, actWatch},
		{"third start allowed with max 3", pending(2, ""), "1.1.0", 3, actWatch},
		{"fourth start with max 3", pending(3, ""), "1.1.0", 3, actRollback},
		{"timeout recorded", pending(1, "slow"), "1.1.0", 3, actRollback},
		{"old version running", pending(0, ""), "1.0.0", 1, actClear},
		{"unrelated version running", pending(0, ""), "1.5.0", 1, actClear},
	}
	for _, c := range cases {
		if got, _ := decideBoot(c.st, c.running, c.max); got != c.want {
			t.Errorf("%s: %v want %v", c.name, got, c.want)
		}
	}
}

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

func TestHealthyStartConfirmsAndDeletesPrevious(t *testing.T) {
	f := appliedFix(t, KindTree)
	prev := LoadState(f.cfg.StateDir).Prev.Dir
	r := f.boot("1.1.0", 1, time.Hour)
	if r.Guard == nil {
		t.Fatal("no guard")
	}
	if err := r.Guard.MarkHealthy(); err != nil {
		t.Fatal(err)
	}
	if err := r.Guard.MarkHealthy(); err != nil {
		t.Fatal("second MarkHealthy must be harmless:", err)
	}
	if st := LoadState(f.cfg.StateDir); st.Phase != PhaseIdle || st.Prev != nil {
		t.Fatalf("state %+v", st)
	}
	if _, err := os.Stat(prev); err == nil {
		t.Fatal("previous version kept after confirmation")
	}
	f.assertInstalled()
	// Later launches are plain.
	if r := f.boot("1.1.0", 1, time.Hour); r.Guard != nil || r.RolledBack != nil {
		t.Fatalf("%+v", r)
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

func TestMarkHealthyAfterTimeoutStillCancelsNothingElse(t *testing.T) {
	f := appliedFix(t, KindTree)
	r := f.boot("1.1.0", 3, time.Hour)
	// A late confirmation from another process path (no Guard).
	ok, err := MarkHealthy(f.cfg.StateDir, "1.1.0")
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	r.Guard.timer.Stop()
	if ok, _ := MarkHealthy(f.cfg.StateDir, "1.1.0"); ok {
		t.Fatal("confirmed twice")
	}
	if ok, _ := MarkHealthy(f.cfg.StateDir, "9.9.9"); ok {
		t.Fatal("confirmed a version that is not pending")
	}
}

func TestStaleRecordIsCleared(t *testing.T) {
	f := appliedFix(t, KindTree)
	// The user restored 1.0.0 by hand and starts it: nothing to roll back.
	r := f.boot("1.0.0", 1, time.Hour)
	if r.RolledBack != nil || r.Guard != nil {
		t.Fatalf("%+v", r)
	}
	if st := LoadState(f.cfg.StateDir); st.Phase != PhaseIdle {
		t.Fatalf("%+v", st)
	}
}

func TestRollbackFailureIsReportedNotFatal(t *testing.T) {
	f := appliedFix(t, KindTree)
	f.boot("1.1.0", 1, time.Hour).Guard.timer.Stop()
	// The kept files vanish (a cleaner ran).
	os.RemoveAll(LoadState(f.cfg.StateDir).Prev.Dir)
	_, err := Boot(BootConfig{StateDir: f.cfg.StateDir, Version: "1.1.0", MaxStarts: 1})
	if err == nil {
		t.Fatal("a failed restore must be reported")
	}
	st := LoadState(f.cfg.StateDir)
	if st.Event == nil || !strings.Contains(st.Event.Reason, "restoring the previous version failed") || st.Phase == PhasePending {
		t.Fatalf("%+v", st)
	}
}

func TestApplyWithoutStateDirKeepsNothing(t *testing.T) {
	f := newDeltaFix(t, KindTree)
	f.cfg.StateDir = ""
	u, _ := f.update()
	if err := u.Download(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := u.Apply(); err != nil {
		t.Fatal(err)
	}
	f.assertInstalled()
	if _, err := os.Stat(filepath.Join(f.target, prevName)); err == nil {
		t.Fatal("previous version kept without a state directory")
	}
}
