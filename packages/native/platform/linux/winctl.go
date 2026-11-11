//go:build linux

package linux

import (
	"unsafe"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	_ caps.WindowControls = (*Window)(nil)
	_ caps.WindowEvents   = (*App)(nil)
)

func (a *App) initExt() {
	a.winHandler = nil
}

// OnWindowEvent sets the handler for window events.
func (a *App) OnWindowEvent(fn func(caps.WindowEvent)) {
	a.mu.Lock()
	a.winHandler = fn
	a.mu.Unlock()
}

func (a *App) emitWindow(w *Window, kind caps.WindowEventKind) {
	a.mu.Lock()
	fn := a.winHandler
	a.mu.Unlock()
	if fn != nil {
		fn(caps.WindowEvent{Window: w.Ref(), Kind: kind})
	}
}

// Ref identifies the window to the capability interfaces.
func (w *Window) Ref() caps.WindowRef { return caps.WindowRef(w.id) }

// WindowByRef returns the window for a Ref, or nil.
func (a *App) WindowByRef(r caps.WindowRef) *Window { return a.windowByID(uintptr(r)) }

func (w *Window) do(fn func(x *ext)) {
	w.app.Dispatch(func() {
		if w.alive {
			fn(w.app.x)
		}
	})
}

// SetAlwaysOnTop keeps the window above others. Wayland compositors ignore
// the request for ordinary toplevels; X11 window managers honor it.
func (w *Window) SetAlwaysOnTop(on bool) {
	w.do(func(x *ext) {
		w.wantAbove = on
		if x.gtkWindowKeepAbove != nil {
			x.gtkWindowKeepAbove(w.win, b2i(on))
		}
	})
}

// SetFullscreen enters or leaves fullscreen.
func (w *Window) SetFullscreen(on bool) {
	w.do(func(x *ext) {
		if on {
			x.gtkWindowFullscreen(w.win)
		} else {
			x.gtkWindowUnfullscreen(w.win)
		}
	})
}

// SetDecorated(false) makes the window frameless.
func (w *Window) SetDecorated(on bool) {
	w.do(func(x *ext) { x.gtkWindowDecorated(w.win, b2i(on)) })
}

// SetPosition moves the window. Wayland ignores it.
func (w *Window) SetPosition(px, py int) {
	w.do(func(x *ext) { x.gtkWindowMove(w.win, int32(px), int32(py)) })
}

// Minimize iconifies the window.
func (w *Window) Minimize() { w.do(func(x *ext) { x.gtkWindowIconify(w.win) }) }

// Maximize maximizes the window.
func (w *Window) Maximize() { w.do(func(x *ext) { x.gtkWindowMaximize(w.win) }) }

// Restore undoes minimize, maximize and fullscreen.
func (w *Window) Restore() {
	w.do(func(x *ext) {
		x.gtkWindowDeiconify(w.win)
		x.gtkWindowUnfullscreen(w.win)
		x.gtkWindowUnmaximize(w.win)
	})
}

// Focus raises and focuses the window.
func (w *Window) Focus() { w.do(func(x *ext) { x.gtkWindowPresent(w.win) }) }

// Show makes the window visible.
func (w *Window) Show() { w.do(func(x *ext) { x.gtkWidgetShow(w.win) }) }

// Hide hides the window without destroying it.
func (w *Window) Hide() { w.do(func(x *ext) { x.gtkWidgetHide(w.win) }) }

// SetPreventClose turns close requests into WindowCloseRequested events.
func (w *Window) SetPreventClose(on bool) { w.do(func(*ext) { w.preventClose = on }) }

// State reads the window state. Maximized, minimized, fullscreen and focus
// come from the GDK window state, which a window manager sets; AlwaysOnTop
// reports the GDK flag or, when no window manager acknowledged it, the
// request.
func (w *Window) State() (st caps.WindowState) {
	w.app.callMain(func() {
		if !w.alive {
			return
		}
		x := w.app.x
		var px, py, ww, hh int32
		x.gtkWindowGetPosition(w.win, &px, &py)
		w.app.api.gtkWindowGetSize(w.win, &ww, &hh)
		st.X, st.Y, st.Width, st.Height = int(px), int(py), int(ww), int(hh)
		st.Title = goString(w.app.api.gtkWindowGetTitle(w.win))
		st.Decorated = x.gtkWindowGetDecorated(w.win) != 0
		st.Visible = x.gtkWidgetGetVisible(w.win) != 0
		st.Focused = x.gtkWindowIsActive(w.win) != 0
		st.AlwaysOnTop = w.wantAbove
		if gw := x.gtkWidgetGetWindow(w.win); gw != 0 && x.gdkWindowGetState != nil {
			f := x.gdkWindowGetState(gw)
			st.Minimized = f&winIconified != 0
			st.Maximized = f&winMaximized != 0
			st.Fullscreen = f&winFullscreen != 0
			st.AlwaysOnTop = st.AlwaysOnTop || f&winAbove != 0
		}
	})
	return
}

// GDKState returns the raw GdkWindowState bits (0 if unmapped); for tests.
func (w *Window) GDKState() (f uint32) {
	w.app.callMain(func() {
		if gw := w.app.x.gtkWidgetGetWindow(w.win); gw != 0 {
			f = w.app.x.gdkWindowGetState(gw)
		}
	})
	return
}

// deleteCallback is the delete-event handler; returning 1 keeps the window.
func deleteCallback(widget, event, id uintptr) uintptr {
	w := theApp.windowByID(id)
	if w != nil && w.preventClose {
		theApp.emitWindow(w, caps.WindowCloseRequested)
		return 1
	}
	return 0
}

// winStateCallback tracks focus changes from GdkEventWindowState.
func winStateCallback(widget, event, id uintptr) uintptr {
	w := theApp.windowByID(id)
	if w == nil || event == 0 {
		return 0
	}
	ev := *(*unsafe.Pointer)(unsafe.Pointer(&event))
	changed := *(*uint32)(unsafe.Add(ev, 20))
	now := *(*uint32)(unsafe.Add(ev, 24))
	w.lastState = now
	if changed&winFocused != 0 {
		if now&winFocused != 0 {
			theApp.emitWindow(w, caps.WindowFocused)
		} else {
			theApp.emitWindow(w, caps.WindowBlurred)
		}
	}
	return 0
}
