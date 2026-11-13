// Package power reports power state and idle time, delivers suspend, resume,
// lock, unlock, battery and idle events, and holds sleep blockers.
//
// Linux: logind (PrepareForSleep, the session's Lock and Unlock signals and
// LockedHint/IdleHint), UPower or /sys/class/power_supply for the battery,
// GNOME Mutter's IdleMonitor or org.freedesktop.ScreenSaver for idle time, and
// org.gnome.SessionManager, org.freedesktop.PowerManagement or
// org.freedesktop.ScreenSaver for blockers. logind's own Inhibit call returns
// a file descriptor, which the SDK's D-Bus client cannot receive, so it is not
// used.
//
// macOS: NSWorkspace sleep and wake notifications, distributed screen lock
// notifications, IOPSCopyPowerSourcesInfo, CGEventSourceSecondsSinceLastEventType
// and IOPMAssertionCreateWithName.
//
// Windows: WM_POWERBROADCAST, WTSRegisterSessionNotification,
// GetSystemPowerStatus, GetLastInputInfo and SetThreadExecutionState.
//
// Battery and idle events come from sampling every few seconds while an
// event stream is open.
package power

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/jsonwire"
)

// Permissions.
const (
	Permission        = "power"
	PermissionInhibit = "power:inhibit"
)

// Host function ids.
const (
	IDState            = "krate/power.state"
	IDIdleTime         = "krate/power.idleTime"
	IDSetIdleThreshold = "krate/power.setIdleThreshold"
	IDPreventSleep     = "krate/power.preventSleep"
	IDAllowSleep       = "krate/power.allowSleep"
	IDEvents           = "krate/power.events"
)

// State is a reading of the power state.
type State struct {
	OnBattery bool `json:"onBattery"`
	// IdleSeconds is the time since the last input, -1 when unknown.
	IdleSeconds float64 `json:"idleSeconds"`
	Locked      bool    `json:"locked"`
	// IdleState is "locked", "idle" (past the idle threshold), "active" or
	// "unknown".
	IdleState string `json:"idleState"`
}

// Event is a power event.
type Event struct {
	// Kind is "suspend", "resume", "lock", "unlock", "ac", "battery", "idle"
	// or "active".
	Kind string `json:"kind"`
}

// Get returns the current state.
func Get() (State, error) { return jsonwire.Query[State](IDState) }

// IdleTime returns the seconds since the last input, -1 when unknown.
func IdleTime() (float64, error) { return jsonwire.Query[float64](IDIdleTime) }

// SetIdleThreshold sets the idle time after which "idle" is reported; 0 turns
// idle events off.
func SetIdleThreshold(seconds float64) error { return jsonwire.Do(IDSetIdleThreshold, seconds) }

// PreventSleep keeps the system awake, and the display on when display is
// true, until AllowSleep or the end of the process. It returns the blocker id.
func PreventSleep(reason string, display bool) (string, error) {
	return jsonwire.Query[string](IDPreventSleep, reason, display)
}

// AllowSleep releases a blocker.
func AllowSleep(id string) error { return jsonwire.Do(IDAllowSleep, id) }

// OnEvent calls fn for power events until cancel is called.
func OnEvent(fn func(Event)) (cancel func(), err error) { return jsonwire.Events(IDEvents, fn) }
