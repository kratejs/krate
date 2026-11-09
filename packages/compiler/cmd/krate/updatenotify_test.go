package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestUpdatePlan(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name        string
		st          updateState
		current     string
		wantShow    string
		wantRefresh bool
	}{
		{"never checked", updateState{}, "0.5.0", "", true},
		{"fresh and newer", updateState{CheckedAt: now.Add(-time.Hour), Latest: "0.5.1"}, "0.5.0", "0.5.1", false},
		{"stale and newer", updateState{CheckedAt: now.Add(-25 * time.Hour), Latest: "0.5.1"}, "0.5.0", "0.5.1", true},
		{"up to date", updateState{CheckedAt: now.Add(-time.Hour), Latest: "0.5.0"}, "0.5.0", "", false},
		{"ahead of latest", updateState{CheckedAt: now.Add(-time.Hour), Latest: "0.4.9"}, "0.5.0", "", false},
		{"dev build", updateState{Latest: "0.5.1"}, "dev", "", false},
		{"clock went backwards", updateState{CheckedAt: now.Add(48 * time.Hour)}, "0.5.0", "", true},
	}
	for _, c := range cases {
		show, refresh := updatePlan(now, c.st, c.current)
		if show != c.wantShow || refresh != c.wantRefresh {
			t.Errorf("%s: show=%q refresh=%v, want %q %v", c.name, show, refresh, c.wantShow, c.wantRefresh)
		}
	}
}

func TestUpdateNotifierEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if !updateNotifierEnv(env(nil)) {
		t.Error("allowed by default")
	}
	for _, m := range []map[string]string{{"CI": "1"}, {"CI": "true"}, {"KRATE_NO_UPDATE_NOTIFIER": "1"}, {"NO_UPDATE_NOTIFIER": "yes"}} {
		if updateNotifierEnv(env(m)) {
			t.Errorf("%v must opt out", m)
		}
	}
	for _, m := range []map[string]string{{"CI": ""}, {"CI": "0"}, {"CI": "false"}} {
		if !updateNotifierEnv(env(m)) {
			t.Errorf("%v must not opt out", m)
		}
	}
}

func TestNotifierCommand(t *testing.T) {
	for _, c := range []string{"build", "dev", "serve", "start", "doctor"} {
		if !notifierCommand(c, false) {
			t.Errorf("%s should notify", c)
		}
		if notifierCommand(c, true) {
			t.Errorf("%s --json must stay quiet", c)
		}
	}
	for _, c := range []string{"mcp", "config", "info", "explain", "upgrade", "version", "inspect"} {
		if notifierCommand(c, false) {
			t.Errorf("%s must stay quiet", c)
		}
	}
}

func TestUpdateStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "update-check.json")
	if st := readUpdateState(path); !st.CheckedAt.IsZero() || st.Latest != "" {
		t.Fatalf("missing file should read as empty, got %+v", st)
	}
	want := updateState{CheckedAt: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC), Latest: "0.5.1"}
	writeUpdateState(path, want)
	if got := readUpdateState(path); !got.CheckedAt.Equal(want.CheckedAt) || got.Latest != want.Latest {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

func TestConfigOptOut(t *testing.T) {
	off := false
	if (&config.Config{UpdateCheck: &off}).UpdateNotifierEnabled() {
		t.Error("updateNotifier: false must disable the notifier")
	}
	if !(&config.Config{}).UpdateNotifierEnabled() {
		t.Error("default is enabled")
	}
}
