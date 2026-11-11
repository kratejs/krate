//go:build linux

package linux

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"golang.org/x/sys/unix"
)

var _ caps.Shortcuts = (*App)(nil)

// x11 is a second connection to the X server used only for key grabs, so its
// events never mix with GTK's.
type x11 struct {
	openDisplay    func(name uintptr) uintptr
	closeDisplay   func(d uintptr) int32
	defaultRoot    func(d uintptr) uintptr
	stringToKeysym func(s string) uintptr
	keysymToCode   func(d, sym uintptr) uint8
	grabKey        func(d uintptr, code int32, mods uint32, win uintptr, owner, pm, km int32) int32
	ungrabKey      func(d uintptr, code int32, mods uint32, win uintptr) int32
	sync           func(d uintptr, discard int32) int32
	setErrHandler  func(h uintptr) uintptr
	pending        func(d uintptr) int32
	nextEvent      func(d uintptr, ev unsafe.Pointer) int32
	connNumber     func(d uintptr) int32
	flush          func(d uintptr) int32

	dpy  uintptr
	root uintptr

	mu       sync.Mutex
	grabs    map[string]x11Grab
	fn       func(string)
	stopR    int
	stopW    int
	started  bool
	finished chan struct{}
}

type x11Grab struct {
	code uint8
	mods uint32
}

var (
	cbX11Err  uintptr
	x11ErrHit bool
	x11Once   sync.Once
	x11Inst   *x11
	x11Err    error
)

func x11ErrorCallback(display, event uintptr) uintptr {
	x11ErrHit = true
	return 0
}

func loadX11() (*x11, error) {
	x11Once.Do(func() {
		if os.Getenv("DISPLAY") == "" {
			x11Err = krate.ErrUnsupported
			return
		}
		lib, err := purego.Dlopen("libX11.so.6", rtldNow|rtldGlobal)
		if err != nil {
			x11Err = krate.ErrUnsupported
			return
		}
		x := &x11{grabs: map[string]x11Grab{}}
		b := &binder{}
		b.bind(&x.openDisplay, lib, "XOpenDisplay")
		b.bind(&x.closeDisplay, lib, "XCloseDisplay")
		b.bind(&x.defaultRoot, lib, "XDefaultRootWindow")
		b.bind(&x.stringToKeysym, lib, "XStringToKeysym")
		b.bind(&x.keysymToCode, lib, "XKeysymToKeycode")
		b.bind(&x.grabKey, lib, "XGrabKey")
		b.bind(&x.ungrabKey, lib, "XUngrabKey")
		b.bind(&x.sync, lib, "XSync")
		b.bind(&x.setErrHandler, lib, "XSetErrorHandler")
		b.bind(&x.pending, lib, "XPending")
		b.bind(&x.nextEvent, lib, "XNextEvent")
		b.bind(&x.connNumber, lib, "XConnectionNumber")
		b.bind(&x.flush, lib, "XFlush")
		if len(b.missing) > 0 {
			x11Err = krate.ErrUnsupported
			return
		}
		cbX11Err = purego.NewCallback(x11ErrorCallback)
		x.setErrHandler(cbX11Err)
		x.dpy = x.openDisplay(0)
		if x.dpy == 0 {
			x11Err = krate.ErrUnsupported
			return
		}
		x.root = x.defaultRoot(x.dpy)
		x11Inst = x
	})
	return x11Inst, x11Err
}

// X11 modifier masks.
const (
	xShift   = 1 << 0
	xLock    = 1 << 1
	xControl = 1 << 2
	xMod1    = 1 << 3
	xMod2    = 1 << 4
	xMod4    = 1 << 6
)

func x11Mods(a caps.Accelerator) uint32 {
	var m uint32
	if a.Ctrl {
		m |= xControl
	}
	if a.Shift {
		m |= xShift
	}
	if a.Alt {
		m |= xMod1
	}
	if a.Super {
		m |= xMod4
	}
	return m
}

// x11KeyName maps an accelerator key name to an X keysym name.
func x11KeyName(k string) string {
	switch strings.ToLower(k) {
	case "space":
		return "space"
	case "esc", "escape":
		return "Escape"
	case "enter", "return":
		return "Return"
	case "tab":
		return "Tab"
	case "backspace":
		return "BackSpace"
	case "delete":
		return "Delete"
	case "up", "down", "left", "right":
		return strings.ToUpper(k[:1]) + strings.ToLower(k[1:])
	case "plus":
		return "plus"
	case "minus":
		return "minus"
	}
	if len(k) == 1 {
		return strings.ToLower(k)
	}
	return k
}

// OnShortcut sets the handler for global shortcut presses.
func (a *App) OnShortcut(fn func(handle string)) {
	if x, err := loadX11(); err == nil {
		x.mu.Lock()
		x.fn = fn
		x.mu.Unlock()
		return
	}
	a.mu.Lock()
	a.shortcutFn = fn
	a.mu.Unlock()
}

// RegisterShortcut grabs the key combination system-wide on X11. Under
// Wayland (or when libX11 is missing) it returns krate.ErrUnsupported; the
// SDK then tries the GlobalShortcuts portal.
func (a *App) RegisterShortcut(handle string, acc caps.Accelerator) error {
	if os.Getenv("WAYLAND_DISPLAY") != "" && os.Getenv("KRATE_FORCE_X11_SHORTCUTS") == "" {
		return krate.ErrUnsupported
	}
	x, err := loadX11()
	if err != nil {
		return err
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if _, dup := x.grabs[handle]; dup {
		return fmt.Errorf("krate: shortcut %q is already registered", handle)
	}
	sym := x.stringToKeysym(x11KeyName(acc.Key))
	if sym == 0 {
		return fmt.Errorf("krate: unknown key %q", acc.Key)
	}
	code := x.keysymToCode(x.dpy, sym)
	if code == 0 {
		return fmt.Errorf("krate: key %q is not on this keyboard", acc.Key)
	}
	mods := x11Mods(acc)
	x11ErrHit = false
	for _, extra := range []uint32{0, xLock, xMod2, xLock | xMod2} {
		x.grabKey(x.dpy, int32(code), mods|extra, x.root, 0, 1, 1)
	}
	x.sync(x.dpy, 0)
	if x11ErrHit {
		for _, extra := range []uint32{0, xLock, xMod2, xLock | xMod2} {
			x.ungrabKey(x.dpy, int32(code), mods|extra, x.root)
		}
		x.sync(x.dpy, 0)
		return fmt.Errorf("krate: %q is already grabbed by another program", acc.Key)
	}
	x.grabs[handle] = x11Grab{code: code, mods: mods}
	if !x.started {
		if err := x.startLoop(); err != nil {
			return err
		}
	}
	return nil
}

// UnregisterShortcut releases a grab.
func (a *App) UnregisterShortcut(handle string) error {
	x, err := loadX11()
	if err != nil {
		return err
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	g, ok := x.grabs[handle]
	if !ok {
		return nil
	}
	delete(x.grabs, handle)
	for _, extra := range []uint32{0, xLock, xMod2, xLock | xMod2} {
		x.ungrabKey(x.dpy, int32(g.code), g.mods|extra, x.root)
	}
	x.sync(x.dpy, 0)
	return nil
}

// startLoop runs the event pump on its own goroutine, waking on the X
// connection and on a pipe used only to wake it.
func (x *x11) startLoop() error {
	var p [2]int
	if err := syscall.Pipe(p[:]); err != nil {
		return err
	}
	x.stopR, x.stopW = p[0], p[1]
	x.started = true
	x.finished = make(chan struct{})
	fd := int(x.connNumber(x.dpy))
	go func() {
		defer close(x.finished)
		ev := make([]byte, 192)
		for {
			fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}, {Fd: int32(x.stopR), Events: unix.POLLIN}}
			if _, e := unix.Poll(fds, -1); e != nil && e != syscall.EINTR {
				return
			}
			if fds[1].Revents != 0 {
				return
			}
			x.mu.Lock()
			for x.pending(x.dpy) > 0 {
				x.nextEvent(x.dpy, unsafe.Pointer(&ev[0]))
				if *(*int32)(unsafe.Pointer(&ev[0])) != 2 { // KeyPress
					continue
				}
				state := *(*uint32)(unsafe.Pointer(&ev[80])) &^ (xLock | xMod2)
				code := uint8(*(*uint32)(unsafe.Pointer(&ev[84])))
				fn := x.fn
				for h, g := range x.grabs {
					if g.code == code && g.mods == state && fn != nil {
						handle := h
						go fn(handle)
					}
				}
			}
			x.mu.Unlock()
		}
	}()
	return nil
}

// StopShortcuts releases every grab and ends the event loop.
func (a *App) StopShortcuts() {
	if x11Inst == nil {
		return
	}
	x := x11Inst
	x.mu.Lock()
	for h := range x.grabs {
		g := x.grabs[h]
		for _, extra := range []uint32{0, xLock, xMod2, xLock | xMod2} {
			x.ungrabKey(x.dpy, int32(g.code), g.mods|extra, x.root)
		}
		delete(x.grabs, h)
	}
	x.sync(x.dpy, 0)
	started := x.started
	x.started = false
	x.mu.Unlock()
	if started {
		syscall.Write(x.stopW, []byte{1})
		<-x.finished
		syscall.Close(x.stopR)
		syscall.Close(x.stopW)
	}
}
