// Package shortcut registers global (system-wide) keyboard shortcuts.
//
// Linux/X11: the key is grabbed on the root window (XGrabKey through libX11),
// so it fires even when the app has no focus; a combination another program
// already grabbed is an error. Linux/Wayland: there is no way to grab keys
// from a client; the xdg-desktop-portal GlobalShortcuts interface is used when
// the desktop offers it (the user may be asked to confirm), otherwise
// Register returns krate.ErrUnsupported. Windows: RegisterHotKey on the app's
// hidden window; a combination another app or Windows holds is an error
// ("already registered by another app"). The Windows backend is compile-only
// in this release. The macOS implementation (RegisterEventHotKey) is not part
// of this release and returns krate.ErrUnsupported.
package shortcut

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/app"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/wire"
)

// Permission is the app permission the UI needs to register shortcuts.
const Permission = "shortcut"

// Host function ids.
const (
	IDRegister   = "krate/shortcut.register"
	IDUnregister = "krate/shortcut.unregister"
	IDEvents     = "krate/shortcut.events"
)

// Parse reads an accelerator such as "Ctrl+Shift+K", "Alt+F4" or
// "CmdOrCtrl+Space". "CmdOrCtrl" is Ctrl here (Super on macOS builds).
func Parse(accel string) (caps.Accelerator, error) {
	var a caps.Accelerator
	parts := strings.Split(accel, "+")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			if i == len(parts)-1 && i > 0 && parts[i-1] == "" {
				a.Key = "plus"
				continue
			}
			if i == len(parts)-1 {
				return a, fmt.Errorf("shortcut: %q has no key", accel)
			}
			continue
		}
		if i == len(parts)-1 {
			a.Key = p
			break
		}
		switch strings.ToLower(p) {
		case "ctrl", "control", "cmdorctrl", "commandorcontrol":
			a.Ctrl = true
		case "shift":
			a.Shift = true
		case "alt", "option":
			a.Alt = true
		case "super", "meta", "win", "cmd", "command":
			a.Super = true
		default:
			return a, fmt.Errorf("shortcut: unknown modifier %q in %q", p, accel)
		}
	}
	if a.Key == "" {
		return a, fmt.Errorf("shortcut: %q has no key", accel)
	}
	if !a.Ctrl && !a.Alt && !a.Super && !a.Shift && len(a.Key) == 1 {
		return a, fmt.Errorf("shortcut: %q needs a modifier; a bare letter would steal typing system-wide", accel)
	}
	return a, nil
}

var (
	routeMu  sync.Mutex
	routes   = map[string]func(){}
	routeSub func()
	routeEp  int64
	counter  atomic.Int64
)

// Register grabs accel system-wide and calls fn (on its own goroutine) each
// time it is pressed. The returned function releases the shortcut.
func Register(accel string, fn func()) (unregister func() error, err error) {
	if _, err := Parse(accel); err != nil {
		return nil, err
	}
	handle := fmt.Sprintf("go-%d-%d", os.Getpid(), counter.Add(1))
	routeMu.Lock()
	routes[handle] = fn
	if ep := bridge.Epoch(); routeSub != nil && ep != routeEp {
		routeSub()
		routeSub = nil
	}
	if routeSub == nil {
		routeEp = bridge.Epoch()
		cancel, err := bridge.Subscribe(context.Background(), IDEvents, func(d *wire.Decoder) {
			id, err := decodeEvent(d)
			if err != nil {
				return
			}
			routeMu.Lock()
			f := routes[id]
			routeMu.Unlock()
			if f != nil {
				go f()
			}
		})
		if err != nil {
			delete(routes, handle)
			routeMu.Unlock()
			return nil, err
		}
		routeSub = cancel
	}
	routeMu.Unlock()
	if err := bridge.CallDo(context.Background(), IDRegister, func(e *wire.Encoder) {
		e.String(handle)
		e.String(accel)
	}); err != nil {
		routeMu.Lock()
		delete(routes, handle)
		routeMu.Unlock()
		return nil, err
	}
	var once sync.Once
	return func() (err error) {
		once.Do(func() {
			routeMu.Lock()
			delete(routes, handle)
			routeMu.Unlock()
			err = bridge.CallDo(context.Background(), IDUnregister, func(e *wire.Encoder) { e.String(handle) })
		})
		return
	}, nil
}

// Supported reports whether shortcuts can be registered in this session.
func Supported() bool { return app.Supports("shortcut") }
