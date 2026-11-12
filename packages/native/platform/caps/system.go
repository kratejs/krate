package caps

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// OS integration capabilities: badge and progress, displays, power, theme and
// webview crashes. They follow the conventions of caps.go: optional
// interfaces that the per-OS App implements, with every method callable from
// any goroutine.

// ProgressState is the state of the taskbar or dock progress indicator.
type ProgressState string

// Progress states. Windows has all of them; macOS draws normal and
// indeterminate (error and paused look like normal there); Linux launchers
// (Unity LauncherEntry) show a plain bar for every determinate state.
const (
	ProgressNone          ProgressState = "none"
	ProgressNormal        ProgressState = "normal"
	ProgressIndeterminate ProgressState = "indeterminate"
	ProgressError         ProgressState = "error"
	ProgressPaused        ProgressState = "paused"
)

// Progress is the state and value (0..1) of the progress indicator.
type Progress struct {
	State ProgressState
	Value float64
}

// Validate checks a progress request and returns its normalized form: Value is
// clamped to 0..1, and states without a value get 0.
func (p Progress) Validate() (Progress, error) {
	switch p.State {
	case ProgressNone, ProgressIndeterminate:
		return Progress{State: p.State}, nil
	case ProgressNormal, ProgressError, ProgressPaused:
	case "":
		return Progress{}, errors.New("progress state is required (none, normal, indeterminate, error or paused)")
	default:
		return Progress{}, fmt.Errorf("unknown progress state %q (want none, normal, indeterminate, error or paused)", p.State)
	}
	if math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
		return Progress{}, errors.New("progress value must be a finite number between 0 and 1")
	}
	return Progress{State: p.State, Value: math.Min(1, math.Max(0, p.Value))}, nil
}

// Badger is implemented by Apps that can badge the app icon and show progress
// on it themselves (macOS and Windows). On Linux the SDK drives the Unity
// LauncherEntry D-Bus API and does not need this.
type Badger interface {
	// SetBadge shows text on the app icon; "" removes it.
	SetBadge(text string) error
	// SetProgress shows progress on the app icon.
	SetProgress(p Progress) error
}

// Display describes one monitor. Coordinates are in the space WindowState.X
// and Y and WindowControls.SetPosition use (the origin is the top-left of the
// primary display on macOS and Windows, y grows downward), in the units of
// that space: points on macOS, device pixels on Windows.
type Display struct {
	// ID is stable while the display configuration does not change.
	ID   string
	Name string
	// Bounds is the full display rectangle, WorkArea the part not covered by
	// docks, taskbars and menu bars.
	X, Y, Width, Height                 int
	WorkX, WorkY, WorkWidth, WorkHeight int
	Scale                               float64
	Primary                             bool
	RefreshHz                           float64 // 0 when unknown
}

// Displays is implemented by Apps that can list monitors.
type Displays interface {
	// Displays lists the monitors, primary first.
	Displays() ([]Display, error)
	// OnDisplays sets the handler called when monitors are added, removed or
	// change geometry or scale. fn must not block.
	OnDisplays(fn func())
}

// Bounds returns the full rectangle of d.
func (d Display) Bounds() Rect { return Rect{d.X, d.Y, d.Width, d.Height} }

// Work returns the work area of d.
func (d Display) Work() Rect { return Rect{d.WorkX, d.WorkY, d.WorkWidth, d.WorkHeight} }

func (r Rect) overlap(o Rect) int {
	x0, y0 := max(r.X, o.X), max(r.Y, o.Y)
	x1, y1 := min(r.X+r.W, o.X+o.W), min(r.Y+r.H, o.Y+o.H)
	if x1 <= x0 || y1 <= y0 {
		return 0
	}
	return (x1 - x0) * (y1 - y0)
}

// DisplayForRect returns the display with the largest overlap with r. A rect
// off every display gets the display nearest its center; ok is false only
// when there are no displays.
func DisplayForRect(ds []Display, r Rect) (Display, bool) {
	if len(ds) == 0 {
		return Display{}, false
	}
	best, bestArea := -1, 0
	for i, d := range ds {
		if a := r.overlap(d.Bounds()); a > bestArea {
			best, bestArea = i, a
		}
	}
	if best >= 0 {
		return ds[best], true
	}
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	best = 0
	bestDist := math.MaxInt
	for i, d := range ds {
		dx := max(d.X-cx, 0, cx-(d.X+d.Width))
		dy := max(d.Y-cy, 0, cy-(d.Y+d.Height))
		if dist := dx*dx + dy*dy; dist < bestDist {
			best, bestDist = i, dist
		}
	}
	return ds[best], true
}

// CenterIn returns the top-left position that centers a w by h window in the
// work area of d. A window larger than the work area is pinned to its
// top-left corner so the title bar stays reachable.
func CenterIn(d Display, w, h int) (x, y int) {
	x = d.WorkX + (d.WorkWidth-w)/2
	y = d.WorkY + (d.WorkHeight-h)/2
	if w > d.WorkWidth {
		x = d.WorkX
	}
	if h > d.WorkHeight {
		y = d.WorkY
	}
	return x, y
}

// FindDisplay returns the display with this id; an empty id means the
// primary display.
func FindDisplay(ds []Display, id string) (Display, bool) {
	for _, d := range ds {
		if (id == "" && d.Primary) || (id != "" && d.ID == id) {
			return d, true
		}
	}
	if id == "" && len(ds) > 0 {
		return ds[0], true
	}
	return Display{}, false
}

// PowerSample is a reading of the power state.
type PowerSample struct {
	OnBattery bool
	// IdleSeconds is the time since the last keyboard or pointer input; a
	// negative value means the platform cannot tell.
	IdleSeconds float64
	// Locked says the screen is locked, when the platform can tell.
	Locked bool
}

// PowerEventKind names a power event.
type PowerEventKind string

// Power events.
const (
	PowerSuspend PowerEventKind = "suspend"
	PowerResume  PowerEventKind = "resume"
	PowerLock    PowerEventKind = "lock"
	PowerUnlock  PowerEventKind = "unlock"
	PowerOnAC    PowerEventKind = "ac"
	PowerOnBatt  PowerEventKind = "battery"
	PowerIdle    PowerEventKind = "idle"
	PowerActive  PowerEventKind = "active"
)

// PowerEvent is delivered to the handler set with Power.OnPower.
type PowerEvent struct{ Kind PowerEventKind }

// Power is implemented by Apps that can read power state and report suspend,
// resume, lock and unlock themselves (macOS and Windows). On Linux the SDK
// talks to logind, UPower and the screensaver services over D-Bus.
type Power interface {
	// PowerSample reads the current state.
	PowerSample() (PowerSample, error)
	// OnPower sets the handler for suspend, resume, lock and unlock. The
	// battery and idle events are derived by the SDK from PowerSample.
	OnPower(fn func(PowerEvent))
	// PreventSleep holds the system (and, with display, the screen) awake
	// until release is called.
	PreventSleep(reason string, display bool) (release func(), err error)
}

// ThemeInfo is the system appearance.
type ThemeInfo struct {
	Dark         bool
	HighContrast bool
}

// Themer is implemented by Apps that can read the system appearance
// themselves (macOS, Windows). On Linux the SDK reads the desktop portal.
type Themer interface {
	// Theme reads the system appearance (not the app's override).
	Theme() (ThemeInfo, error)
	// OnTheme sets the handler called when the system appearance changes.
	OnTheme(fn func(ThemeInfo))
}

// WindowThemer is implemented by Apps that can set the appearance of their
// native title bars, menus and dialogs.
type WindowThemer interface {
	// SetWindowTheme selects "light" or "dark"; "system" or "" follows the
	// system where the platform can tell (macOS, Windows). On Linux the SDK
	// always passes the resolved "light" or "dark".
	SetWindowTheme(mode string) error
}

// ViewCrash reports a webview process that crashed or was killed.
type ViewCrash struct {
	View   ViewRef
	Window WindowRef
	// Reason is "crashed", "killed", "out-of-memory", "unresponsive" or
	// "unknown".
	Reason string
	// ExitCode is the process exit code when the platform has one.
	ExitCode int
}

// ViewCrashes is implemented by Apps that report webview process crashes
// (WebKitGTK web-process-terminated, WKWebView
// webViewWebContentProcessDidTerminate:, WebView2 ProcessFailed).
type ViewCrashes interface {
	// OnViewCrash sets the handler. fn runs on the main thread and must not
	// block.
	OnViewCrash(fn func(ViewCrash))
	// ReloadView loads the view's current route again.
	ReloadView(v ViewRef) error
}

// LoginItems is implemented by Apps with a platform way to start at login
// that the SDK cannot do from the file system alone (macOS SMAppService).
type LoginItems interface {
	// LoginItem reports whether the app is registered and how.
	LoginItem() (enabled bool, method string, err error)
	// SetLoginItem registers or removes the app.
	SetLoginItem(enabled bool) error
}

// Attention is implemented by Apps that can ask for the user's attention
// (a bouncing dock icon, a flashing taskbar button).
type Attention interface {
	// RequestAttention starts (on) or cancels (!on) the request.
	RequestAttention(on bool) error
}

// TopLeftY converts the y of a rectangle in Cocoa screen coordinates (origin
// at the bottom-left of the primary display, y growing upward) to the
// top-left origin used for windows and displays. primaryHeight is the height
// of the primary display.
func TopLeftY(primaryHeight, y, height float64) int {
	return int(math.Round(primaryHeight - y - height))
}

// OnBatteryFromSourceType reads the string IOPSGetProvidingPowerSourceType
// returns ("AC Power", "Battery Power" or "UPS Power"): true only for the
// battery.
func OnBatteryFromSourceType(s string) bool {
	return strings.EqualFold(strings.TrimSpace(s), "Battery Power")
}
