package updater

import (
	"fmt"
	"sync"
	"time"
)

// Defaults of the first-start watch.
const (
	// DefaultMaxStarts: a new version gets this many starts to confirm; the
	// start after that restores the previous version.
	DefaultMaxStarts = 1
	// DefaultHealthTimeout: a start that has not confirmed after this long is
	// recorded as unhealthy, so the next launch rolls back.
	DefaultHealthTimeout = 60 * time.Second
)

// BootConfig configures Boot.
type BootConfig struct {
	// StateDir is the updater state directory (required).
	StateDir string
	// Version is the running version.
	Version string
	// MaxStarts and Timeout default to DefaultMaxStarts and
	// DefaultHealthTimeout.
	MaxStarts int
	Timeout   time.Duration
	// FS is the file system the restore works on; default the real one.
	FS FS
	// Now overrides the clock (tests).
	Now func() time.Time
	// Logf receives diagnostics.
	Logf func(format string, args ...any)
}

// BootResult is what Boot found.
type BootResult struct {
	// Guard watches an unconfirmed start; nil when there is none.
	Guard *Guard
	// RolledBack is set when this call restored the previous version. The
	// caller should start Restart and exit.
	RolledBack *RollbackEvent
	// Restart is the executable to start after a rollback.
	Restart string
}

func (c *BootConfig) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *BootConfig) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

// bootAction is the decision of the rollback state machine at start-up.
type bootAction int

const (
	actNone     bootAction = iota // nothing to do
	actClear                      // stale record: forget it
	actWatch                      // an unconfirmed start: count it and watch
	actRollback                   // restore the previous version
)

// decideBoot is the state machine for the start of version running with the
// saved state st.
func decideBoot(st State, running string, maxStarts int) (bootAction, string) {
	if st.Phase != PhasePending {
		return actNone, ""
	}
	if running != st.To {
		// The recorded update is not what runs: it was replaced or undone by
		// other means.
		return actClear, ""
	}
	if st.Unhealthy != "" {
		return actRollback, st.Unhealthy
	}
	if st.Starts >= maxStarts {
		return actRollback, fmt.Sprintf("version %s started %d time(s) without confirming a healthy start (it crashed or exited first)", st.To, st.Starts)
	}
	return actWatch, ""
}

// Boot must run early in every start of an app that updates itself. It
// implements first-start protection:
//
//   - after an update, the new version has MaxStarts starts to call
//     MarkHealthy (the Krate shell does so after the first window loaded);
//   - a start that crashed, or did not confirm within Timeout, counts as
//     failed, and the next start restores the previous version, records a
//     RollbackEvent and reports it in BootResult.
//
// Boot returns a Guard for an unconfirmed start. A crash before Boot is
// reached (a loader error, a panic in package init) is not seen.
func Boot(c BootConfig) (BootResult, error) {
	var res BootResult
	if c.StateDir == "" || c.Version == "" {
		return res, nil
	}
	max := c.MaxStarts
	if max <= 0 {
		max = DefaultMaxStarts
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultHealthTimeout
	}
	fsys := c.FS
	if fsys == nil {
		fsys = OSFS{}
	}
	st := loadState(c.StateDir)
	act, reason := decideBoot(st, c.Version, max)
	switch act {
	case actClear:
		c.logf("updater: ignoring a stale update record (%s to %s, running %s)", st.From, st.To, c.Version)
		if st.Prev != nil {
			discardPrevious(fsys, st.Prev)
		}
		return res, updateState(c.StateDir, func(s *State) { clearPending(s) })
	case actWatch:
		if err := updateState(c.StateDir, func(s *State) { s.Starts++ }); err != nil {
			c.logf("updater: cannot record the first start: %v", err)
		}
		res.Guard = newGuard(c.StateDir, c.Version, timeout, fsys)
		return res, nil
	case actRollback:
		ev := &RollbackEvent{Failed: st.To, Restored: st.From, Reason: reason, At: c.now()}
		var rerr error
		if st.Prev != nil {
			rerr = restorePrevious(fsys, st.Prev)
		} else {
			rerr = fmt.Errorf("updater: no previous version was kept")
		}
		if rerr != nil {
			ev.Reason += "; restoring the previous version failed: " + rerr.Error()
		}
		err := updateState(c.StateDir, func(s *State) {
			clearPending(s)
			s.Failed = append(s.Failed, st.To)
			s.Event = ev
			if rerr == nil {
				s.Phase = PhaseRolledBack
			}
		})
		if rerr != nil {
			return res, fmt.Errorf("updater: rollback of %s failed: %w", st.To, rerr)
		}
		c.logf("updater: rolled back %s to %s: %s", ev.Failed, ev.Restored, ev.Reason)
		res.RolledBack = ev
		res.Restart = st.Prev.relaunchPath()
		return res, err
	}
	return res, nil
}

func clearPending(s *State) {
	s.Phase = PhaseIdle
	s.From, s.To, s.Unhealthy = "", "", ""
	s.Starts = 0
	s.Prev = nil
}

// relaunchPath is what to start after a rollback: the AppImage file itself
// for KindFile, the executable otherwise.
func (p *PrevInfo) relaunchPath() string {
	if p == nil {
		return ""
	}
	if p.Kind == KindFile {
		return p.Target
	}
	return p.Exe
}

// Guard watches the unconfirmed start of an updated version.
type Guard struct {
	dir, version string
	fs           FS
	mu           sync.Mutex
	timer        *time.Timer
	done         bool
}

func newGuard(dir, version string, timeout time.Duration, fsys FS) *Guard {
	g := &Guard{dir: dir, version: version, fs: fsys}
	g.timer = time.AfterFunc(timeout, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.done {
			return
		}
		g.done = true
		_ = updateState(dir, func(s *State) {
			if s.Phase == PhasePending && s.To == version {
				s.Unhealthy = fmt.Sprintf("version %s did not confirm a healthy start within %s", version, timeout)
			}
		})
	})
	return g
}

// MarkHealthy confirms the start: the previous version is deleted and the
// update is final. It is safe to call more than once and on a nil Guard.
func (g *Guard) MarkHealthy() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.timer.Stop()
	g.done = true
	_, err := markHealthy(g.dir, g.version, g.fs)
	return err
}

// MarkHealthy confirms that version runs fine, whether or not a Guard exists
// (for example in a different process). It reports whether an unconfirmed
// update was confirmed.
func MarkHealthy(stateDir, version string) (bool, error) {
	return markHealthy(stateDir, version, OSFS{})
}

func markHealthy(dir, version string, fsys FS) (bool, error) {
	if dir == "" {
		return false, nil
	}
	var prev *PrevInfo
	confirmed := false
	err := updateState(dir, func(s *State) {
		if s.Phase == PhasePending && s.To == version {
			confirmed = true
			prev = s.Prev
			clearPending(s)
		}
	})
	if confirmed {
		discardPrevious(fsys, prev)
	}
	return confirmed, err
}
