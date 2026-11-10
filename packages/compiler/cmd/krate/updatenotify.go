package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

const (
	updateCheckInterval = 24 * time.Hour
	// updateRetryAfter is how long to wait before asking again after a failed
	// lookup (offline, registry down).
	updateRetryAfter = time.Hour
	updateFetchLimit = 3 * time.Second
	// updateExitWait is the longest a finishing command waits for the lookup so
	// a short command still refreshes the cache.
	updateExitWait = 500 * time.Millisecond
)

// updateState is the cached result of the last lookup.
type updateState struct {
	CheckedAt time.Time `json:"checkedAt"`
	Latest    string    `json:"latest,omitempty"`
}

// envTruthy reports whether an environment variable is set to something other
// than empty, 0 or false.
func envTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

// updateNotifierEnv reports whether the environment allows the notifier: it is
// off in CI and when KRATE_NO_UPDATE_NOTIFIER or NO_UPDATE_NOTIFIER is set.
func updateNotifierEnv(getenv func(string) string) bool {
	for _, k := range []string{"CI", "KRATE_NO_UPDATE_NOTIFIER", "NO_UPDATE_NOTIFIER"} {
		if envTruthy(getenv(k)) {
			return false
		}
	}
	return true
}

// notifierCommand reports whether a command may print the update line.
// Commands whose output is read by tools (mcp, config, inspect, explain, info)
// and the ones that manage versions themselves (upgrade, version) stay quiet.
func notifierCommand(cmd string, jsonOutput bool) bool {
	if jsonOutput {
		return false
	}
	switch cmd {
	case "build", "dev", "serve", "preview", "start", "check", "deploy", "doctor":
		return true
	}
	return false
}

// updatePlan decides what a command does about updates: show is the newer
// version to announce ("" for none) and refresh says the cached lookup is old
// enough to ask the registry again.
func updatePlan(now time.Time, st updateState, current string) (show string, refresh bool) {
	if _, ok := parseSemver(current); !ok {
		return "", false // a dev build has no version to compare
	}
	if st.Latest != "" {
		if c, ok := compareSemver(st.Latest, current); ok && c > 0 {
			show = st.Latest
		}
	}
	return show, now.Sub(st.CheckedAt) >= updateCheckInterval || st.CheckedAt.After(now.Add(time.Hour))
}

// updateMessage is the one line shown when a newer version exists.
func updateMessage(current, latest string) string {
	return fmt.Sprintf("Update available: krate v%s -> v%s. Run `krate upgrade` to update (silence with updateNotifier: false).", current, latest)
}

func updateStatePath() (string, error) {
	if p := os.Getenv("KRATE_UPDATE_CHECK_FILE"); p != "" {
		return p, nil
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "krate", "update-check.json"), nil
}

func readUpdateState(path string) updateState {
	var st updateState
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	return st
}

func writeUpdateState(path string, st updateState) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(st)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}

// stderrIsTerminal reports whether stderr is an interactive terminal.
func stderrIsTerminal() bool {
	info, err := os.Stderr.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

var (
	updateOnce sync.Once
	updateDone chan struct{}
)

// startUpdateCheck prints the update line from the cached lookup and, when the
// cache is more than a day old, refreshes it in the background. It never
// delays the command and does nothing in CI, when stderr is not a terminal,
// for tool-facing commands, or when the config or environment opt out.
func startUpdateCheck(cfg *config.Config, command string, jsonOutput bool) {
	updateOnce.Do(func() {
		if !notifierCommand(command, jsonOutput) || !cfg.UpdateNotifierEnabled() ||
			!updateNotifierEnv(os.Getenv) || !stderrIsTerminal() {
			return
		}
		path, err := updateStatePath()
		if err != nil {
			return
		}
		show, refresh := updatePlan(time.Now(), readUpdateState(path), version)
		if show != "" {
			fmt.Fprintf(os.Stderr, "  %s%s%s\n", cYellow, updateMessage(version, show), cReset)
		}
		if !refresh {
			return
		}
		updateDone = make(chan struct{})
		go func() {
			defer close(updateDone)
			ctx, cancel := context.WithTimeout(context.Background(), updateFetchLimit)
			defer cancel()
			latest, err := registryVersion(ctx, registryBase(), krateScope+"core", "latest")
			st := updateState{CheckedAt: time.Now(), Latest: latest}
			if err != nil {
				// Keep the version we knew and try again in an hour.
				st = readUpdateState(path)
				st.CheckedAt = time.Now().Add(updateRetryAfter - updateCheckInterval)
			}
			writeUpdateState(path, st)
		}()
	})
}

// finishUpdateCheck gives a lookup started by this command a moment to store
// its result, so short commands such as `krate build` still refresh the cache.
func finishUpdateCheck() {
	if updateDone == nil {
		return
	}
	select {
	case <-updateDone:
	case <-time.After(updateExitWait):
	}
}
