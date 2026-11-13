package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Phases of the rollback state machine.
//
//	idle         nothing to watch
//	pending      an update was applied; its first start(s) are unconfirmed
//	rolled_back  the update failed and the previous version was restored; the
//	             event waits until the app acknowledges it
const (
	PhaseIdle       = ""
	PhasePending    = "pending"
	PhaseRolledBack = "rolled_back"
)

// RollbackEvent reports a rollback.
type RollbackEvent struct {
	// Failed is the version that was rolled back, Restored the version that
	// runs again.
	Failed   string    `json:"failed"`
	Restored string    `json:"restored"`
	Reason   string    `json:"reason"`
	At       time.Time `json:"at"`
}

// State is what the updater remembers between runs (state.json in the state
// directory).
type State struct {
	Phase string `json:"phase,omitempty"`
	// From is the version that was replaced, To the one that was applied.
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
	// Starts counts the starts of To that did not confirm yet.
	Starts int `json:"starts,omitempty"`
	// Unhealthy is set by the watchdog when To did not confirm in time.
	Unhealthy string    `json:"unhealthy,omitempty"`
	AppliedAt time.Time `json:"applied_at,omitempty"`
	Prev      *PrevInfo `json:"prev,omitempty"`
	// Event is the unacknowledged rollback.
	Event *RollbackEvent `json:"event,omitempty"`
	// Failed lists versions that were rolled back; Check does not offer them.
	Failed []string `json:"failed,omitempty"`
	// Channel is the channel chosen at run time.
	Channel string `json:"channel,omitempty"`
}

var stateMu sync.Mutex

func statePath(dir string) string { return filepath.Join(dir, "state.json") }

func loadState(dir string) State {
	stateMu.Lock()
	defer stateMu.Unlock()
	return readState(dir)
}

func readState(dir string) State {
	var st State
	if dir == "" {
		return st
	}
	if b, err := os.ReadFile(statePath(dir)); err == nil {
		if json.Unmarshal(b, &st) != nil {
			return State{}
		}
	}
	return st
}

// writeState saves atomically; callers hold stateMu.
func writeState(dir string, st State) error {
	if dir == "" {
		return errors.New("updater: no state directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := statePath(dir) + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, statePath(dir)); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// update loads, changes and saves the state under one lock.
func updateState(dir string, f func(*State)) error {
	stateMu.Lock()
	defer stateMu.Unlock()
	st := readState(dir)
	f(&st)
	return writeState(dir, st)
}

func (c *Config) failedVersion(v string) bool {
	for _, f := range loadState(c.StateDir).Failed {
		if f == v {
			return true
		}
	}
	return false
}

// recordApplied marks an applied update as unconfirmed.
func recordApplied(c Config, to string, prev *PrevInfo) error {
	return updateState(c.StateDir, func(st *State) {
		st.Phase = PhasePending
		st.From, st.To = c.Version, to
		st.Starts, st.Unhealthy = 0, ""
		st.AppliedAt = c.now()
		st.Prev = prev
	})
}

// SetChannel chooses the update channel for this install.
func SetChannel(stateDir, channel string) error {
	if !ValidChannel(channel) {
		return fmt.Errorf("updater: %q is not a channel name (lowercase letters, digits, . _ -)", channel)
	}
	return updateState(stateDir, func(st *State) { st.Channel = channel })
}

// LoadState returns the saved state (zero when there is none).
func LoadState(stateDir string) State { return loadState(stateDir) }

// AckRollback clears the pending rollback event.
func AckRollback(stateDir string) error {
	return updateState(stateDir, func(st *State) {
		st.Event = nil
		if st.Phase == PhaseRolledBack {
			st.Phase = PhaseIdle
		}
	})
}
