//go:build linux

package linux

// OS integration: displays (GDK monitors), the application theme for GTK
// title bars, and webview process crashes (web-process-terminated).

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	_ caps.Displays     = (*App)(nil)
	_ caps.WindowThemer = (*App)(nil)
	_ caps.ViewCrashes  = (*App)(nil)
)

// sysAPI holds the GDK and GObject symbols of this file. A missing symbol
// leaves its field nil and the feature answers an error.
type sysAPI struct {
	gdkDisplayNMonitors func(d uintptr) int32
	gdkDisplayMonitor   func(d uintptr, n int32) uintptr
	gdkDisplayScreen    func(d uintptr) uintptr
	gdkMonGeometry      func(m uintptr, r *gdkRect)
	gdkMonWorkarea      func(m uintptr, r *gdkRect)
	gdkMonScale         func(m uintptr) int32
	gdkMonPrimary       func(m uintptr) int32
	gdkMonModel         func(m uintptr) uintptr
	gdkMonMaker         func(m uintptr) uintptr
	gdkMonRefresh       func(m uintptr) int32

	gtkSettingsDefault func() uintptr
	gValueInit         func(v *gValue, t uintptr) uintptr
	gValueSetBool      func(v *gValue, b int32)
	gValueGetBool      func(v *gValue) int32
	gValueUnset        func(v *gValue)
	gObjectSetProp     func(o uintptr, name string, v *gValue)
	gObjectGetProp     func(o uintptr, name string, v *gValue)
}

type gdkRect struct{ x, y, w, h int32 }

// gValue is a GValue: the type and two words of data. Only GLib touches
// the fields; Go just provides the zeroed storage.
type gValue struct {
	_ uintptr
	_ [2]uint64
}

const gTypeBoolean = 5 << 2

var (
	sysOnce sync.Once
	sys     *sysAPI

	sysCbOnce           sync.Once
	cbMonitors, cbCrash uintptr
)

func loadSys() *sysAPI {
	sysOnce.Do(func() {
		s := &sysAPI{}
		b := &binder{}
		open := func(name string) uintptr {
			h, err := purego.Dlopen(name, rtldNow|rtldGlobal)
			if err != nil {
				return 0
			}
			return h
		}
		gdk, gtk, gobj := open("libgdk-3.so.0"), open("libgtk-3.so.0"), open("libgobject-2.0.so.0")
		bind := func(lib uintptr, fn any, sym string) {
			if lib != 0 {
				b.bind(fn, lib, sym)
			}
		}
		bind(gdk, &s.gdkDisplayNMonitors, "gdk_display_get_n_monitors")
		bind(gdk, &s.gdkDisplayMonitor, "gdk_display_get_monitor")
		bind(gdk, &s.gdkDisplayScreen, "gdk_display_get_default_screen")
		bind(gdk, &s.gdkMonGeometry, "gdk_monitor_get_geometry")
		bind(gdk, &s.gdkMonWorkarea, "gdk_monitor_get_workarea")
		bind(gdk, &s.gdkMonScale, "gdk_monitor_get_scale_factor")
		bind(gdk, &s.gdkMonPrimary, "gdk_monitor_is_primary")
		bind(gdk, &s.gdkMonModel, "gdk_monitor_get_model")
		bind(gdk, &s.gdkMonMaker, "gdk_monitor_get_manufacturer")
		bind(gdk, &s.gdkMonRefresh, "gdk_monitor_get_refresh_rate")
		bind(gtk, &s.gtkSettingsDefault, "gtk_settings_get_default")
		bind(gobj, &s.gValueInit, "g_value_init")
		bind(gobj, &s.gValueSetBool, "g_value_set_boolean")
		bind(gobj, &s.gValueGetBool, "g_value_get_boolean")
		bind(gobj, &s.gValueUnset, "g_value_unset")
		bind(gobj, &s.gObjectSetProp, "g_object_set_property")
		bind(gobj, &s.gObjectGetProp, "g_object_get_property")
		sys = s
	})
	return sys
}

func sysCallbacks() {
	sysCbOnce.Do(func() {
		cbMonitors = purego.NewCallback(monitorsChanged)
		cbCrash = purego.NewCallback(webProcessTerminated)
	})
}

// monitorFn is the display change handler; it is guarded by the App mutex.
func monitorsChanged(_, _, _ uintptr) uintptr {
	a := theApp
	if a == nil {
		return 0
	}
	a.mu.Lock()
	fn := a.displaysFn
	a.mu.Unlock()
	if fn != nil {
		fn()
	}
	return 0
}

// Displays lists the GDK monitors, primary first.
func (a *App) Displays() (out []caps.Display, err error) {
	s := loadSys()
	if s.gdkDisplayNMonitors == nil || s.gdkMonGeometry == nil || a.x.gdkDisplayDefault == nil {
		return nil, errors.New("krate: this GDK has no monitor API")
	}
	if !a.callMain(func() { out = a.listDisplays(s) }) {
		return nil, errors.New("krate: application is not running")
	}
	if len(out) == 0 {
		return nil, errors.New("krate: GDK reports no monitors")
	}
	return out, nil
}

func (a *App) listDisplays(s *sysAPI) []caps.Display {
	d := a.x.gdkDisplayDefault()
	if d == 0 {
		return nil
	}
	n := int(s.gdkDisplayNMonitors(d))
	var out []caps.Display
	for i := 0; i < n; i++ {
		m := s.gdkDisplayMonitor(d, int32(i))
		if m == 0 {
			continue
		}
		var g, w gdkRect
		s.gdkMonGeometry(m, &g)
		w = g
		if s.gdkMonWorkarea != nil {
			s.gdkMonWorkarea(m, &w)
		}
		scale := 1.0
		if s.gdkMonScale != nil {
			scale = float64(s.gdkMonScale(m))
		}
		var name string
		if s.gdkMonMaker != nil && s.gdkMonModel != nil {
			name = strings.TrimSpace(goString(s.gdkMonMaker(m)) + " " + goString(s.gdkMonModel(m)))
		}
		if name == "" {
			name = fmt.Sprintf("Display %d", i+1)
		}
		disp := caps.Display{
			ID: fmt.Sprintf("m%d", i), Name: name,
			X: int(g.x), Y: int(g.y), Width: int(g.w), Height: int(g.h),
			WorkX: int(w.x), WorkY: int(w.y), WorkWidth: int(w.w), WorkHeight: int(w.h),
			Scale: scale,
		}
		if s.gdkMonPrimary != nil {
			disp.Primary = s.gdkMonPrimary(m) != 0
		}
		if s.gdkMonRefresh != nil {
			disp.RefreshHz = float64(s.gdkMonRefresh(m)) / 1000
		}
		out = append(out, disp)
	}
	// Some backends mark no monitor primary; the first one is then.
	hasPrimary := false
	for _, d := range out {
		hasPrimary = hasPrimary || d.Primary
	}
	if !hasPrimary && len(out) > 0 {
		out[0].Primary = true
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Primary && !out[j].Primary })
	return out
}

// OnDisplays sets the handler for monitor changes. fn runs on the main thread.
func (a *App) OnDisplays(fn func()) {
	a.mu.Lock()
	first := a.displaysFn == nil
	a.displaysFn = fn
	a.mu.Unlock()
	if !first {
		return
	}
	s := loadSys()
	if s.gdkDisplayScreen == nil {
		return
	}
	sysCallbacks()
	a.callMain(func() {
		d := a.x.gdkDisplayDefault()
		if d == 0 {
			return
		}
		a.api.gSignalConnectData(d, "monitor-added", cbMonitors, 0, 0, 0)
		a.api.gSignalConnectData(d, "monitor-removed", cbMonitors, 0, 0, 0)
		if scr := s.gdkDisplayScreen(d); scr != 0 {
			a.api.gSignalConnectData(scr, "monitors-changed", cbMonitors, 0, 0, 0)
			a.api.gSignalConnectData(scr, "size-changed", cbMonitors, 0, 0, 0)
		}
	})
}

// SetWindowTheme selects the GTK light or dark variant for every window.
// GTK 3 draws its own title bars, so this changes them too.
func (a *App) SetWindowTheme(mode string) error {
	s := loadSys()
	if s.gtkSettingsDefault == nil || s.gObjectSetProp == nil || s.gValueInit == nil {
		return errors.New("krate: this GTK cannot change the theme preference")
	}
	var err error
	if !a.callMain(func() {
		st := s.gtkSettingsDefault()
		if st == 0 {
			err = errors.New("krate: GTK has no default settings object")
			return
		}
		var v gValue
		s.gValueInit(&v, gTypeBoolean)
		on := int32(0)
		if mode == "dark" {
			on = 1
		}
		s.gValueSetBool(&v, on)
		s.gObjectSetProp(st, "gtk-application-prefer-dark-theme", &v)
		s.gValueUnset(&v)
	}) {
		return errors.New("krate: application is not running")
	}
	return err
}

// OnViewCrash sets the handler for web process crashes.
func (a *App) OnViewCrash(fn func(caps.ViewCrash)) {
	a.mu.Lock()
	a.crashFn = fn
	a.mu.Unlock()
}

// connectCrash hooks web-process-terminated on a view.
func (a *App) connectCrash(v *view) {
	sysCallbacks()
	a.api.gSignalConnectData(v.widget, "web-process-terminated", cbCrash, uintptr(v.ref), 0, 0)
}

// crashReason names a WebKitWebProcessTerminationReason.
func crashReason(r uintptr) string {
	switch r {
	case 0:
		return "crashed"
	case 1:
		return "out-of-memory"
	case 2:
		return "killed"
	}
	return "unknown"
}

func webProcessTerminated(_, reason, ref uintptr) uintptr {
	a := theApp
	if a == nil {
		return 0
	}
	v := a.viewByRef(ref)
	if v == nil {
		return 0
	}
	a.mu.Lock()
	fn := a.crashFn
	a.mu.Unlock()
	if fn != nil {
		fn(caps.ViewCrash{View: v.ref, Window: v.w.Ref(), Reason: crashReason(reason)})
	}
	return 0
}

// ReloadView loads the view's current route again. After a crash the view
// shows nothing; loading starts a new web process.
func (a *App) ReloadView(ref caps.ViewRef) error {
	var err error
	if !a.callMain(func() {
		v := a.viewByRef(uintptr(ref))
		switch {
		case v == nil:
			err = fmt.Errorf("krate: no view %d", ref)
		case v.widget == 0:
			err = errReleased
		default:
			v.load(v.currentRoute())
		}
	}) {
		return errors.New("krate: application is not running")
	}
	return err
}
