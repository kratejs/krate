// Package window opens and controls application windows.
//
// A Window is a handle (its SDK id); the operations are forwarded to the
// process that owns the platform, so they work from Go host code in dev
// (child process) and in release builds alike. The first window of an app has
// the id "main".
//
// Platform notes: on Wayland compositors always-on-top and absolute window
// positions are ignored (the compositor decides); X11 window managers honor
// both. Minimize, maximize and fullscreen need a window manager.
package window

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// Permission is the app permission the UI needs to control windows.
const Permission = "window"

// Host function ids.
const (
	IDOpen           = "krate/window.Open"
	IDList           = "krate/window.List"
	IDState          = "krate/window.State"
	IDEvents         = "krate/window.events"
	IDClose          = "krate/window.Close"
	IDSetTitle       = "krate/window.SetTitle"
	IDSetSize        = "krate/window.SetSize"
	IDSetPosition    = "krate/window.SetPosition"
	IDSetAlwaysOnTop = "krate/window.SetAlwaysOnTop"
	IDSetFullscreen  = "krate/window.SetFullscreen"
	IDSetFrameless   = "krate/window.SetFrameless"
	IDMinimize       = "krate/window.Minimize"
	IDMaximize       = "krate/window.Maximize"
	IDRestore        = "krate/window.Restore"
	IDFocus          = "krate/window.Focus"
	IDShow           = "krate/window.Show"
	IDHide           = "krate/window.Hide"
	IDPreventClose   = "krate/window.SetPreventClose"
	IDNavigate       = "krate/window.Navigate"
)

// Options configures Open.
type Options struct {
	Title  string
	Width  int // default 1024
	Height int // default 768
	// Path is the route to open, e.g. "/settings". Default "/".
	Path string
	// Fixed makes the window non-resizable.
	Fixed       bool
	Frameless   bool
	AlwaysOnTop bool
	Fullscreen  bool
	Maximized   bool
	// X and Y position the window when HasPosition is set.
	X, Y        int
	HasPosition bool
	// PreventClose delivers a close-requested event instead of closing.
	PreventClose bool
}

// State is a snapshot of a window.
type State struct {
	X, Y        int
	Width       int
	Height      int
	Maximized   bool
	Minimized   bool
	Fullscreen  bool
	AlwaysOnTop bool
	Decorated   bool
	Visible     bool
	Focused     bool
	Title       string
}

// EventKind names a window event.
type EventKind string

// Events.
const (
	CloseRequested EventKind = "close-requested"
	Closed         EventKind = "closed"
	Focused        EventKind = "focus"
	Blurred        EventKind = "blur"
)

// Event is a window event.
type Event struct {
	Window string
	Kind   EventKind
}

// Window is a handle to one window.
type Window struct{ ID string }

// Main is the handle of the first window.
func Main() *Window { return &Window{ID: "main"} }

// Get returns the handle for an id.
func Get(id string) *Window { return &Window{ID: id} }

// Open opens a window on a route of the app.
func Open(o Options) (*Window, error) {
	d, err := bridge.Call(context.Background(), IDOpen, func(e *wire.Encoder) { encodeOptions(e, o) })
	if err != nil {
		return nil, err
	}
	id, err := d.String()
	if err != nil {
		return nil, err
	}
	return &Window{ID: id}, nil
}

// List returns the ids of the open windows.
func List() ([]string, error) {
	d, err := bridge.Call(context.Background(), IDList, nil)
	if err != nil {
		return nil, err
	}
	return codec.ReadStrings(d)
}

func (w *Window) do(id string, args func(*wire.Encoder)) error {
	return bridge.CallDo(context.Background(), id, func(e *wire.Encoder) {
		e.String(w.ID)
		if args != nil {
			args(e)
		}
	})
}

// Close closes the window (even when PreventClose is set).
func (w *Window) Close() error { return w.do(IDClose, nil) }

// SetTitle sets the title.
func (w *Window) SetTitle(title string) error {
	return w.do(IDSetTitle, func(e *wire.Encoder) { e.String(title) })
}

// SetSize resizes the window.
func (w *Window) SetSize(width, height int) error {
	return w.do(IDSetSize, func(e *wire.Encoder) { e.Int(int64(width)); e.Int(int64(height)) })
}

// SetPosition moves the window (ignored under Wayland).
func (w *Window) SetPosition(x, y int) error {
	return w.do(IDSetPosition, func(e *wire.Encoder) { e.Int(int64(x)); e.Int(int64(y)) })
}

// SetAlwaysOnTop keeps the window above others (ignored under Wayland).
func (w *Window) SetAlwaysOnTop(on bool) error {
	return w.do(IDSetAlwaysOnTop, func(e *wire.Encoder) { e.Bool(on) })
}

// SetFullscreen enters or leaves fullscreen.
func (w *Window) SetFullscreen(on bool) error {
	return w.do(IDSetFullscreen, func(e *wire.Encoder) { e.Bool(on) })
}

// SetFrameless removes (true) or restores (false) the title bar and border.
func (w *Window) SetFrameless(on bool) error {
	return w.do(IDSetFrameless, func(e *wire.Encoder) { e.Bool(on) })
}

// Minimize iconifies the window.
func (w *Window) Minimize() error { return w.do(IDMinimize, nil) }

// Maximize maximizes the window.
func (w *Window) Maximize() error { return w.do(IDMaximize, nil) }

// Restore undoes minimize, maximize and fullscreen.
func (w *Window) Restore() error { return w.do(IDRestore, nil) }

// Focus raises and focuses the window.
func (w *Window) Focus() error { return w.do(IDFocus, nil) }

// Show shows a hidden window.
func (w *Window) Show() error { return w.do(IDShow, nil) }

// Hide hides the window without closing it.
func (w *Window) Hide() error { return w.do(IDHide, nil) }

// SetPreventClose makes close requests (title bar button, Alt+F4) emit a
// CloseRequested event instead of closing; call Close to close for real.
func (w *Window) SetPreventClose(on bool) error {
	return w.do(IDPreventClose, func(e *wire.Encoder) { e.Bool(on) })
}

// Navigate loads another route in the window.
func (w *Window) Navigate(path string) error {
	return w.do(IDNavigate, func(e *wire.Encoder) { e.String(path) })
}

// State reads the window's current state.
func (w *Window) State() (State, error) {
	d, err := bridge.Call(context.Background(), IDState, func(e *wire.Encoder) { e.String(w.ID) })
	if err != nil {
		return State{}, err
	}
	return decodeState(d)
}

// OnEvent calls fn for every window event until cancel is called.
func OnEvent(fn func(Event)) (cancel func(), err error) {
	return bridge.Subscribe(context.Background(), IDEvents, func(d *wire.Decoder) {
		if ev, err := decodeEvent(d); err == nil {
			fn(ev)
		}
	})
}

// OnCloseRequested calls fn when this window is asked to close and
// PreventClose is set.
func (w *Window) OnCloseRequested(fn func()) (cancel func(), err error) {
	return OnEvent(func(ev Event) {
		if ev.Window == w.ID && ev.Kind == CloseRequested {
			fn()
		}
	})
}

// StateDir is where SaveState keeps files. The default is
// <user config dir>/<KRATE_APP_ID or "krate-app">/window-state.
func StateDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	id := os.Getenv("KRATE_APP_ID")
	if id == "" {
		id = "krate-app"
	}
	return filepath.Join(base, id, "window-state")
}

var stateName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type savedState struct {
	X, Y       int
	Width      int
	Height     int
	Maximized  bool
	Fullscreen bool
}

// SaveState stores the window's position, size and maximized/fullscreen flags
// under name, for RestoreState in a later run.
func SaveState(w *Window, name string) error {
	if !stateName.MatchString(name) {
		return fmt.Errorf("window: state name %q must match %s", name, stateName)
	}
	st, err := w.State()
	if err != nil {
		return err
	}
	b, _ := json.Marshal(savedState{st.X, st.Y, st.Width, st.Height, st.Maximized, st.Fullscreen})
	dir := StateDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(dir, name+".json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, name+".json"))
}

// RestoreState applies the state saved under name. It reports false, with no
// error, when nothing was saved. Positions are only applied when they fall on
// a plausible screen area (non-negative coordinates).
func RestoreState(w *Window, name string) (bool, error) {
	if !stateName.MatchString(name) {
		return false, fmt.Errorf("window: state name %q must match %s", name, stateName)
	}
	b, err := os.ReadFile(filepath.Join(StateDir(), name+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var s savedState
	if err := json.Unmarshal(b, &s); err != nil {
		return false, nil
	}
	if s.Width > 0 && s.Height > 0 && !s.Maximized && !s.Fullscreen {
		if err := w.SetSize(s.Width, s.Height); err != nil {
			return false, err
		}
	}
	if s.X >= 0 && s.Y >= 0 && !s.Maximized && !s.Fullscreen {
		if err := w.SetPosition(s.X, s.Y); err != nil && !krate.IsUnsupported(err) {
			return false, err
		}
	}
	if s.Maximized {
		return true, w.Maximize()
	}
	if s.Fullscreen {
		return true, w.SetFullscreen(true)
	}
	return true, nil
}
