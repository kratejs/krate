// Package badge puts a badge and a progress indicator on the app's icon in
// the dock, taskbar or launcher.
//
// macOS: NSDockTile badgeLabel and a progress bar drawn into the dock tile.
// Error and paused progress look like normal progress there.
//
// Windows: ITaskbarList3 SetProgressValue and SetProgressState for progress,
// and SetOverlayIcon with a badge drawn by the SDK for the badge text (counts
// above 99 show as "99+"; the tiny overlay fits two or three glyphs).
//
// Linux: the Unity LauncherEntry D-Bus signal (com.canonical.Unity.
// LauncherEntry), which GNOME Dash to Dock, KDE Plasma's task manager and
// Plank show. It carries a count, a progress fraction and an urgent flag, so
// only numeric badge text is possible; other text is refused with
// krate.ErrUnsupported. The launcher matches the app by its desktop file
// "<appId>.desktop". Indeterminate progress has no representation and shows
// as no progress.
package badge

import (
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/jsonwire"
)

// Permission is the app permission the UI needs.
const Permission = "badge"

// Host function ids.
const (
	IDSet         = "krate/badge.set"
	IDGet         = "krate/badge.get"
	IDSetProgress = "krate/badge.setProgress"
	IDProgress    = "krate/badge.progress"
	IDSetUrgent   = "krate/badge.setUrgent"
)

// ProgressState is the state of the progress indicator.
type ProgressState = caps.ProgressState

// Progress states.
const (
	None          = caps.ProgressNone
	Normal        = caps.ProgressNormal
	Indeterminate = caps.ProgressIndeterminate
	Error         = caps.ProgressError
	Paused        = caps.ProgressPaused
)

// Progress is the progress indicator: a state and, for the determinate
// states, a value from 0 to 1.
type Progress struct {
	State ProgressState `json:"state"`
	Value float64       `json:"value"`
}

// Set shows text on the app icon; "" removes the badge.
func Set(text string) error { return jsonwire.Do(IDSet, text) }

// SetCount shows a count on the app icon; 0 removes the badge.
func SetCount(n int) error {
	if n <= 0 {
		return Set("")
	}
	return Set(itoa(n))
}

// Get returns the badge text last set, "" when none.
func Get() (string, error) { return jsonwire.Query[string](IDGet) }

// SetProgress shows progress on the app icon.
func SetProgress(state ProgressState, value float64) error {
	return jsonwire.Do(IDSetProgress, string(state), value)
}

// ClearProgress removes the progress indicator.
func ClearProgress() error { return SetProgress(None, 0) }

// GetProgress returns the progress last set.
func GetProgress() (Progress, error) { return jsonwire.Query[Progress](IDProgress) }

// SetUrgent asks for the user's attention: a bouncing dock icon, a flashing
// taskbar button or an urgent launcher entry. false cancels the request.
func SetUrgent(on bool) error { return jsonwire.Do(IDSetUrgent, on) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
