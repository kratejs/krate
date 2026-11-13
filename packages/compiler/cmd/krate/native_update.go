package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/native/updater"

	"github.com/kratejs/krate/packages/compiler/internal/native/pkg"
)

// updateFlags are the flags of krate native rollout and krate native patch,
// and the release controls of krate native package.
type updateFlags struct {
	DeltaFrom                 []string
	DeltaCount                int
	Channel                   string
	Rollout                   string
	RolloutStart, RolloutRamp string
	MinVersion                string
	Critical, Uncritical      bool
	Halt                      bool
	Feeds                     []string

	PatchOld, PatchNew, PatchKind, PatchFrom, PatchTo string
	PatchAppID, PatchTarget, PatchExe, PatchOut       string
	PatchURL                                          string
}

var updateValuedFlags = map[string]bool{
	"--delta-from": true, "--delta-count": true, "--channel": true, "--rollout": true,
	"--rollout-start": true, "--rollout-ramp-hours": true, "--min-version": true, "--feed": true,
	"--patch-old": true, "--patch-new": true, "--patch-kind": true, "--patch-from": true, "--patch-to": true,
	"--patch-app-id": true, "--patch-target": true, "--patch-exe": true, "--patch-out": true, "--patch-url": true,
}

func splitList(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// applyUpdateFlag stores a value of an update flag.
func (u *updateFlags) set(name, val string) error {
	switch name {
	case "--delta-from":
		u.DeltaFrom = append(u.DeltaFrom, splitList(val)...)
	case "--delta-count":
		n, err := strconv.Atoi(val)
		if err != nil || n < 1 {
			return fmt.Errorf("--delta-count needs a positive number, got %q", val)
		}
		u.DeltaCount = n
	case "--channel":
		if !updater.ValidChannel(val) {
			return fmt.Errorf("--channel %q: use lowercase letters, digits, . _ -", val)
		}
		u.Channel = val
	case "--rollout":
		u.Rollout = val
	case "--rollout-start":
		u.RolloutStart = val
	case "--rollout-ramp-hours":
		u.RolloutRamp = val
	case "--min-version":
		u.MinVersion = val
	case "--feed":
		u.Feeds = append(u.Feeds, splitList(val)...)
	case "--patch-old":
		u.PatchOld = val
	case "--patch-new":
		u.PatchNew = val
	case "--patch-kind":
		u.PatchKind = val
	case "--patch-from":
		u.PatchFrom = val
	case "--patch-to":
		u.PatchTo = val
	case "--patch-app-id":
		u.PatchAppID = val
	case "--patch-target":
		u.PatchTarget = val
	case "--patch-exe":
		u.PatchExe = val
	case "--patch-out":
		u.PatchOut = val
	case "--patch-url":
		u.PatchURL = val
	}
	return nil
}

// rollout builds the Rollout the flags describe, nil when none was asked.
func (u *updateFlags) rollout() (*updater.Rollout, error) {
	if u.Halt {
		return &updater.Rollout{Percentage: 0}, nil
	}
	if u.Rollout == "" {
		if u.RolloutStart != "" || u.RolloutRamp != "" {
			return nil, fmt.Errorf("--rollout-start and --rollout-ramp-hours need --rollout <percent>")
		}
		return nil, nil
	}
	p, err := strconv.ParseFloat(strings.TrimSuffix(u.Rollout, "%"), 64)
	if err != nil || p < 0 || p > 100 {
		return nil, fmt.Errorf("--rollout needs a percentage from 0 to 100, got %q", u.Rollout)
	}
	r := &updater.Rollout{Percentage: p, Start: u.RolloutStart}
	if u.RolloutRamp != "" {
		if r.RampHours, err = strconv.ParseFloat(u.RolloutRamp, 64); err != nil || r.RampHours < 0 {
			return nil, fmt.Errorf("--rollout-ramp-hours needs a number of hours, got %q", u.RolloutRamp)
		}
	}
	return r, nil
}

// runNativeRollout changes the rollout controls of written feeds:
//
//	krate native rollout --feed dist-native/packages/latest-linux-amd64.json --rollout 25
//	krate native rollout --feed a.json,b.json --halt
//	krate native rollout --feed a.json --rollout 100 --critical
func runNativeRollout(flags cliFlags) {
	u := flags.Update
	if len(u.Feeds) == 0 {
		fmt.Fprintf(os.Stderr, "%sError:%s krate native rollout needs --feed <latest-*.json> (comma separated or repeated)\n", cRed, cReset)
		exit(1)
	}
	r, err := u.rollout()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cRed, cReset, err)
		exit(1)
	}
	edit := pkg.FeedEdit{Rollout: r}
	if u.Critical {
		t := true
		edit.Critical = &t
	}
	if u.Uncritical {
		f := false
		edit.Critical = &f
	}
	if u.MinVersion != "" {
		v := u.MinVersion
		if v == "none" {
			v = ""
		}
		edit.MinVersion = &v
	}
	if r == nil && edit.Critical == nil && edit.MinVersion == nil {
		fmt.Fprintf(os.Stderr, "%sError:%s nothing to change: pass --rollout <percent>, --halt, --critical, --uncritical or --min-version <version|none>\n", cRed, cReset)
		exit(1)
	}
	for _, f := range u.Feeds {
		if err := pkg.EditFeed(f, edit); err != nil {
			reportPkgError(err)
			exit(1)
		}
		fmt.Printf("  %s+%s updated %s\n", cGreen, cReset, f)
	}
	fmt.Printf("  Upload the changed feed files; no signature changes.\n")
}

// runNativePatch writes one signed patch between two update files and prints
// the feed entry for it.
func runNativePatch(flags cliFlags) {
	u := flags.Update
	missing := ""
	for name, v := range map[string]string{"--patch-old": u.PatchOld, "--patch-new": u.PatchNew, "--patch-kind": u.PatchKind,
		"--patch-from": u.PatchFrom, "--patch-to": u.PatchTo, "--patch-app-id": u.PatchAppID, "--patch-target": u.PatchTarget, "--patch-out": u.PatchOut} {
		if v == "" {
			missing += " " + name
		}
	}
	if missing != "" {
		fmt.Fprintf(os.Stderr, "%sError:%s krate native patch needs:%s (--patch-exe too for --patch-kind tree)\n", cRed, cReset, missing)
		exit(1)
	}
	key, err := pkg.LoadUpdaterKey(os.Getenv)
	if err == nil && key == nil {
		err = fmt.Errorf("set %s or %s", pkg.EnvUpdaterKey, pkg.EnvUpdaterKeyFile)
	}
	if err != nil {
		reportPkgError(err)
		exit(1)
	}
	fp, err := pkg.WritePatch(u.PatchOld, u.PatchNew, u.PatchOut, u.PatchKind, u.PatchExe,
		updater.PatchMeta{AppID: u.PatchAppID, Target: u.PatchTarget, From: u.PatchFrom, To: u.PatchTo}, key, u.PatchURL)
	if err != nil {
		reportPkgError(err)
		exit(1)
	}
	doc, _ := json.MarshalIndent(fp, "    ", "  ")
	fmt.Printf("  %s+%s wrote %s (%d bytes)\n\n  Add this to \"patches\" in the feed:\n\n    %s\n", cGreen, cReset, u.PatchOut, fp.Size, doc)
}
