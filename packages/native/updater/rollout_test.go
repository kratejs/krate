package updater

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestCheckGatesOnRollout(t *testing.T) {
	f := newDeltaFix(t, KindTree)
	ctx := context.Background()
	check := func(id string) *Update {
		t.Helper()
		f.cfg.InstallID = id
		u, err := Check(ctx, f.cfg)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	// Find one install inside a 30% rollout and one outside.
	var in, out string
	for i := 0; in == "" || out == ""; i++ {
		id := fmt.Sprintf("probe-%d", i)
		if Bucket(id, "1.1.0") < 30 {
			in = id
		} else {
			out = id
		}
	}
	f.rollout = &Rollout{Percentage: 30}
	if check(in) == nil || check(out) != nil {
		t.Fatal("30% rollout gating wrong")
	}
	f.rollout = &Rollout{Percentage: 0}
	if check(in) != nil {
		t.Fatal("halted rollout offered the update")
	}
	f.rollout = &Rollout{Percentage: 100}
	if check(out) == nil {
		t.Fatal("100% rollout withheld the update")
	}
	// Critical and min_version bypass the rollout.
	f.rollout = &Rollout{Percentage: 0}
	f.critical = true
	if u := check(out); u == nil || !u.Critical {
		t.Fatal("critical update was gated")
	}
	f.critical = false
	f.minVersion = "1.0.5"
	if u := check(out); u == nil || !u.Required {
		t.Fatal("min_version did not force the update")
	}
	f.minVersion = "0.9.0"
	if check(out) != nil {
		t.Fatal("min_version below the running version must not force")
	}
	// Start in the future holds everybody back; the clock moves it forward.
	f.minVersion = ""
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	f.cfg.Now = func() time.Time { return now }
	f.rollout = &Rollout{Percentage: 100, Start: now.Add(time.Hour).Format(time.RFC3339)}
	if check(in) != nil {
		t.Fatal("update offered before the rollout start")
	}
	now = now.Add(2 * time.Hour)
	if check(in) == nil {
		t.Fatal("update withheld after the rollout start")
	}
}
