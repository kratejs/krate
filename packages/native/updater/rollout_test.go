package updater

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBucketIsStableAndSpread(t *testing.T) {
	if Bucket("install-a", "1.2.0") != Bucket("install-a", "1.2.0") {
		t.Fatal("bucket is not deterministic")
	}
	if Bucket("install-a", "1.2.0") == Bucket("install-a", "1.3.0") {
		t.Fatal("a new release should reshuffle the cohort")
	}
	var below10, below50 int
	const n = 20000
	for i := 0; i < n; i++ {
		b := Bucket(fmt.Sprintf("id-%d", i), "1.2.0")
		if b < 0 || b >= 100 {
			t.Fatalf("bucket %v out of range", b)
		}
		if b < 10 {
			below10++
		}
		if b < 50 {
			below50++
		}
	}
	if below10 < n*9/100 || below10 > n*11/100 || below50 < n*48/100 || below50 > n*52/100 {
		t.Fatalf("uneven spread: <10%%: %d, <50%%: %d of %d", below10, below50, n)
	}
}

func TestRolloutIsMonotonic(t *testing.T) {
	now := time.Now()
	for i := 0; i < 500; i++ {
		id := fmt.Sprintf("id-%d", i)
		was := false
		for p := 0.0; p <= 100; p += 5 {
			in := (&Rollout{Percentage: p}).Includes(id, "2.0.0", now)
			if was && !in {
				t.Fatalf("%s dropped out when the percentage grew to %v", id, p)
			}
			was = in
		}
		if !was {
			t.Fatalf("%s is not in a 100%% rollout", id)
		}
	}
	if (&Rollout{Percentage: 0}).Includes("x", "2.0.0", now) {
		t.Fatal("a halted rollout includes an install")
	}
	var nilRollout *Rollout
	if !nilRollout.Includes("x", "2.0.0", now) {
		t.Fatal("no rollout means everyone")
	}
}

func TestRolloutStartAndRamp(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	r := &Rollout{Percentage: 80, Start: start.Format(time.RFC3339), RampHours: 10}
	for _, c := range []struct {
		at   time.Duration
		want float64
	}{{-time.Hour, 0}, {0, 0}, {5 * time.Hour, 40}, {10 * time.Hour, 80}, {100 * time.Hour, 80}} {
		if got := r.EffectivePercent(start.Add(c.at)); got < c.want-0.001 || got > c.want+0.001 {
			t.Errorf("at %v: %v want %v", c.at, got, c.want)
		}
	}
	if (&Rollout{Percentage: 30}).EffectivePercent(start) != 30 {
		t.Error("no start means immediately")
	}
}

func TestRolloutValidation(t *testing.T) {
	for _, doc := range []string{
		`{"percentage":101}`, `{"percentage":-1}`, `{"percentage":5,"ramp_hours":-2}`, `{"percentage":5,"start":"tomorrow"}`,
	} {
		f := `{"version":"1.1.0","url":"https://x/a","signature":"` + sigB64() + `","rollout":` + doc + `}`
		if _, err := ParseFeed([]byte(f)); err == nil {
			t.Errorf("%s accepted", doc)
		}
	}
	if _, err := ParseFeed([]byte(`{"version":"1.1.0","url":"https://x/a","signature":"` + sigB64() + `","min_version":"soon"}`)); err == nil {
		t.Error("bad min_version accepted")
	}
}

func sigB64() string {
	return "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="
}

func TestInstallIDPersists(t *testing.T) {
	dir := t.TempDir()
	a := InstallID(dir)
	if len(a) < 16 {
		t.Fatalf("short id %q", a)
	}
	idMu.Lock()
	delete(memoryIDs, dir)
	idMu.Unlock()
	if b := InstallID(dir); b != a {
		t.Fatalf("id changed across runs: %q then %q", a, b)
	}
	if other := InstallID(t.TempDir()); other == a {
		t.Fatal("two installs share an id")
	}
	if _, err := os.Stat(filepath.Join(dir, "install-id")); err != nil {
		t.Fatal(err)
	}
	// Without a directory the id still lasts for the process.
	if InstallID("") != InstallID("") {
		t.Fatal("in-memory id unstable")
	}
}

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

func TestChannelExpandsInURL(t *testing.T) {
	if got := ExpandURLChannel("https://x/{channel}/{target}.json", "linux-amd64", "1.0.0", ""); got != "https://x/stable/linux-amd64.json" {
		t.Fatal(got)
	}
	if got := ExpandURLChannel("https://x/{channel}.json", "t", "1", "beta"); got != "https://x/beta.json" {
		t.Fatal(got)
	}
	dir := t.TempDir()
	c := Config{StateDir: dir, Channel: "nightly"}
	if c.channel() != "nightly" {
		t.Fatal("configured channel ignored")
	}
	if err := SetChannel(dir, "beta"); err != nil {
		t.Fatal(err)
	}
	if c.channel() != "beta" {
		t.Fatal("chosen channel does not win")
	}
	for _, bad := range []string{"", "Beta", "../x", "a b", "-x"} {
		if SetChannel(dir, bad) == nil {
			t.Errorf("channel %q accepted", bad)
		}
	}
	if (&Config{}).channel() != "stable" {
		t.Fatal("default channel")
	}
}
