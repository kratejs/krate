//go:build darwin

package darwin

import (
	"errors"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// AppKit constants used below.
const (
	nsApplicationActivationPolicyRegular = 0
	nsEventTypeApplicationDefined        = 15
	nsEventModifierFlagShift             = 1 << 17
	nsEventModifierFlagCommand           = 1 << 20
	nsTerminateCancel                    = 0
)

// Options configures the application. It mirrors platform.Options plus a
// switch the GUI test needs.
type Options struct {
	AppID   string
	Handler http.Handler
	Debug   bool
	// KeepAlive stops the app from quitting when its last window closes.
	KeepAlive bool
	// Memory is the app.json "memory" profile ("balanced" when empty).
	// UNTESTED on macOS, see memory.go.
	Memory string
	// Features is app.json "features"; nil means not detected, so no
	// feature is switched off.
	Features map[string]bool
}

// WindowOptions configures one window; it mirrors platform.WindowOptions.
type WindowOptions struct {
	Title     string
	Width     int
	Height    int
	Resizable bool
	Path      string
}

// App is the macOS application. The first goroutine to import this package
// is pinned to the main thread (see init), so New and Run must be called from
// the goroutine that runs main.
type App struct {
	opts Options

	nsApp         objc.ID
	appDelegate   objc.ID
	winDelegate   objc.ID
	schemeHandler objc.ID
	navDelegate   objc.ID
	processPool   objc.ID

	ready atomic.Bool
	// purgeStarted is true while the memory cache purge chain runs.
	purgeStarted atomic.Bool
	onReady      func()

	// Touched only on the main thread.
	windows map[objc.ID]*Window
	tasks   map[objc.ID]*taskState
	byWeb   map[objc.ID]*view

	// viewMu guards viewOwner, which WindowOfView reads from any goroutine.
	viewMu      sync.Mutex
	viewOwner   map[caps.ViewRef]caps.WindowRef
	nextView    uint64
	viewHandler func(caps.ViewEvent)
}

var theApp *App

func init() {
	runtime.LockOSThread()
}

// New creates the NSApplication and its delegates. It must be called on the
// main thread, before Run, and only once per process.
func New(opts Options) (*App, error) {
	if opts.Handler == nil {
		return nil, errors.New("darwin: Options.Handler is required")
	}
	if theApp != nil {
		return nil, errors.New("darwin: New called twice")
	}
	if err := initRuntime(); err != nil {
		return nil, err
	}
	if !isMainThread() {
		return nil, errors.New("darwin: New must be called on the main thread")
	}
	a := &App{
		opts:    opts,
		windows: map[objc.ID]*Window{},
		tasks:   map[objc.ID]*taskState{},
		byWeb:   map[objc.ID]*view{},

		viewOwner: map[caps.ViewRef]caps.WindowRef{},
	}
	var err error
	withPool(func() { err = a.setup() })
	if err != nil {
		return nil, err
	}
	theApp = a
	return a, nil
}

func (a *App) setup() error {
	if err := registerClasses(a); err != nil {
		return err
	}
	a.nsApp = cls("NSApplication").Send(sel("sharedApplication"))
	a.nsApp.Send(sel("setActivationPolicy:"), nsApplicationActivationPolicyRegular)
	a.appDelegate = cls("KrateAppDelegate").Send(sel("new"))
	a.winDelegate = cls("KrateWindowDelegate").Send(sel("new"))
	a.schemeHandler = cls("KrateSchemeHandler").Send(sel("new"))
	a.navDelegate = cls("KrateNavDelegate").Send(sel("new"))
	// NSApplication.delegate is unretained; the App (a package global) owns it.
	a.nsApp.Send(sel("setDelegate:"), a.appDelegate)

	// One process pool for every window so scheme registration is shared.
	a.processPool = newProcessPool()
	registerSchemePrivate(a.processPool)

	a.buildMenu()
	return nil
}

// registerSchemePrivate marks the krate scheme as a secure context and
// CORS-enabled (needed for crypto.subtle and fetch). The public WebKit API has
// no way to do this, so it uses WKProcessPool private selectors, and silently
// does nothing if they are absent.
func registerSchemePrivate(pool objc.ID) {
	for _, name := range []string{
		"_registerURLSchemeAsSecure:",
		"_registerURLSchemeAsCORSEnabled:",
	} {
		if s := sel(name); respondsTo(pool, s) {
			pool.Send(s, nsString(Scheme))
		}
	}
}

func menuItem(title string, action objc.SEL, key string, mask uint) objc.ID {
	it := cls("NSMenuItem").Send(sel("alloc")).Send(sel("initWithTitle:action:keyEquivalent:"),
		nsString(title), action, nsString(key))
	it.Send(sel("autorelease"))
	if key != "" {
		it.Send(sel("setKeyEquivalentModifierMask:"), mask)
	}
	return it
}

func newMenu(title string) objc.ID {
	m := cls("NSMenu").Send(sel("alloc")).Send(sel("initWithTitle:"), nsString(title))
	m.Send(sel("autorelease"))
	return m
}

// buildMenu installs the main menu. Without an Edit menu the Cmd+C/V/X/A/Z
// shortcuts never reach a WKWebView text field.
func (a *App) buildMenu() {
	name := goString(cls("NSProcessInfo").Send(sel("processInfo")).Send(sel("processName")))
	if strings.TrimSpace(name) == "" {
		name = "Krate"
	}
	menubar := newMenu("")
	addTop := func(title string, build func(m objc.ID)) objc.ID {
		item := menuItem("", 0, "", 0)
		menubar.Send(sel("addItem:"), item)
		m := newMenu(title)
		item.Send(sel("setSubmenu:"), m)
		build(m)
		return m
	}
	const cmd = nsEventModifierFlagCommand
	const cmdShift = nsEventModifierFlagCommand | nsEventModifierFlagShift

	addTop(name, func(m objc.ID) {
		quit := menuItem("Quit "+name, sel("krateQuit:"), "q", cmd)
		quit.Send(sel("setTarget:"), a.appDelegate)
		m.Send(sel("addItem:"), quit)
	})
	addTop("Edit", func(m objc.ID) {
		m.Send(sel("addItem:"), menuItem("Undo", sel("undo:"), "z", cmd))
		m.Send(sel("addItem:"), menuItem("Redo", sel("redo:"), "z", cmdShift))
		m.Send(sel("addItem:"), cls("NSMenuItem").Send(sel("separatorItem")))
		m.Send(sel("addItem:"), menuItem("Cut", sel("cut:"), "x", cmd))
		m.Send(sel("addItem:"), menuItem("Copy", sel("copy:"), "c", cmd))
		m.Send(sel("addItem:"), menuItem("Paste", sel("paste:"), "v", cmd))
		m.Send(sel("addItem:"), menuItem("Select All", sel("selectAll:"), "a", cmd))
	})
	windowMenu := addTop("Window", func(m objc.ID) {
		m.Send(sel("addItem:"), menuItem("Minimize", sel("performMiniaturize:"), "m", cmd))
		m.Send(sel("addItem:"), menuItem("Zoom", sel("performZoom:"), "", 0))
		m.Send(sel("addItem:"), menuItem("Close", sel("performClose:"), "w", cmd))
	})
	a.nsApp.Send(sel("setMainMenu:"), menubar)
	a.nsApp.Send(sel("setWindowsMenu:"), windowMenu)
}

// Run starts the AppKit event loop on the calling (main) thread and returns
// after Quit. onReady runs from applicationDidFinishLaunching.
//
// Run uses [NSApp run] and ends it with [NSApp stop:] plus a posted
// application-defined event (stop: alone is not noticed until the next event).
// [NSApp terminate:] is deliberately avoided because it calls exit() and Run
// would never return; Cmd+Q and Dock Quit are routed to Quit instead.
func (a *App) Run(onReady func()) error {
	if !isMainThread() {
		return errors.New("darwin: Run must be called on the main thread")
	}
	a.onReady = onReady
	a.nsApp.Send(sel("run"))
	return nil
}

// Dispatch runs fn on the main thread and returns without waiting.
func (a *App) Dispatch(fn func()) { dispatch(fn) }

// Origin is the app origin.
func (a *App) Origin() string { return OriginURL }

// Quit makes Run return. It may be called from any goroutine.
func (a *App) Quit() {
	dispatch(func() {
		a.nsApp.Send(sel("stop:"), objc.ID(0))
		// Wake the loop so it notices the stop request.
		ev := cls("NSEvent").Send(
			sel("otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:"),
			nsEventTypeApplicationDefined, nsPoint{}, 0, 0.0, 0, objc.ID(0), 0, 0, 0)
		a.nsApp.Send(sel("postEvent:atStart:"), ev, true)
	})
}

func (a *App) didFinishLaunching() {
	a.nsApp.Send(sel("activateIgnoringOtherApps:"), true)
	a.ready.Store(true)
	if a.onReady != nil {
		a.onReady()
	}
}

// registerClasses registers the Objective-C classes once. Method type
// encodings come from the Go signatures: ID is "@", SEL ":", bool "B", uint "Q".
func registerClasses(a *App) error {
	nsObject := objc.GetClass("NSObject")
	protos := func(names ...string) []*objc.Protocol {
		var out []*objc.Protocol
		for _, n := range names {
			if p := objc.GetProtocol(n); p != nil {
				out = append(out, p)
			}
		}
		return out
	}

	_, err := objc.RegisterClass("KrateAppDelegate", nsObject, protos("NSApplicationDelegate"), nil, []objc.MethodDef{
		{Cmd: sel("applicationDidFinishLaunching:"), Fn: func(self objc.ID, _ objc.SEL, note objc.ID) {
			a.didFinishLaunching()
		}},
		{Cmd: sel("applicationShouldTerminateAfterLastWindowClosed:"), Fn: func(self objc.ID, _ objc.SEL, app objc.ID) bool {
			// Last-window handling is in Go so Run can return.
			return false
		}},
		{Cmd: sel("applicationShouldTerminate:"), Fn: func(self objc.ID, _ objc.SEL, app objc.ID) uint {
			a.Quit()
			return nsTerminateCancel
		}},
		{Cmd: sel("applicationSupportsSecureRestorableState:"), Fn: func(self objc.ID, _ objc.SEL, app objc.ID) bool {
			return true
		}},
		{Cmd: sel("krateQuit:"), Fn: func(self objc.ID, _ objc.SEL, sender objc.ID) {
			a.Quit()
		}},
	})
	if err != nil {
		return err
	}

	_, err = objc.RegisterClass("KrateWindowDelegate", nsObject, protos("NSWindowDelegate"), nil, []objc.MethodDef{
		{Cmd: sel("windowWillClose:"), Fn: func(self objc.ID, _ objc.SEL, note objc.ID) {
			a.windowWillClose(note.Send(sel("object")))
		}},
		{Cmd: sel("windowShouldClose:"), Fn: func(self objc.ID, _ objc.SEL, nsWin objc.ID) bool {
			if w := a.windows[nsWin]; w != nil {
				if w.preventClose {
					emitWindow(w, caps.WindowCloseRequested)
					return false
				}
				if a.parkable(w) {
					w.release()
					return false
				}
			}
			return true
		}},
		{Cmd: sel("windowDidMiniaturize:"), Fn: func(self objc.ID, _ objc.SEL, note objc.ID) {
			if w := a.windows[note.Send(sel("object"))]; w != nil {
				w.memoryPressure()
			}
		}},
		{Cmd: sel("windowDidBecomeKey:"), Fn: func(self objc.ID, _ objc.SEL, note objc.ID) {
			if w := a.windows[note.Send(sel("object"))]; w != nil {
				emitWindow(w, caps.WindowFocused)
			}
		}},
		{Cmd: sel("windowDidResignKey:"), Fn: func(self objc.ID, _ objc.SEL, note objc.ID) {
			if w := a.windows[note.Send(sel("object"))]; w != nil {
				emitWindow(w, caps.WindowBlurred)
			}
		}},
	})
	if err != nil {
		return err
	}

	_, err = objc.RegisterClass("KrateMenuTarget", nsObject, nil, nil, []objc.MethodDef{
		{Cmd: sel("krateMenu:"), Fn: func(self objc.ID, _ objc.SEL, item objc.ID) {
			a.menuClicked(item)
		}},
	})
	if err != nil {
		return err
	}

	_, err = objc.RegisterClass("KrateSchemeHandler", nsObject, protos("WKURLSchemeHandler"), nil, []objc.MethodDef{
		{Cmd: sel("webView:startURLSchemeTask:"), Fn: func(self objc.ID, _ objc.SEL, webView, task objc.ID) {
			a.startTask(webView, task)
		}},
		{Cmd: sel("webView:stopURLSchemeTask:"), Fn: func(self objc.ID, _ objc.SEL, webView, task objc.ID) {
			a.stopTask(task)
		}},
	})
	if err != nil {
		return err
	}

	_, err = objc.RegisterClass("KrateNavDelegate", nsObject, protos("WKNavigationDelegate"), nil, []objc.MethodDef{
		{Cmd: sel("webView:decidePolicyForNavigationAction:decisionHandler:"), Fn: func(self objc.ID, _ objc.SEL, webView, action, handler objc.ID) {
			a.decideNavigation(action, handler)
		}},
		{Cmd: sel("webView:didFinishNavigation:"), Fn: func(self objc.ID, _ objc.SEL, webView, nav objc.ID) {
			a.navigationFinished(webView)
		}},
	})
	return err
}

// decideNavigation implements the navigation lock. The decision handler is
// void (^)(WKNavigationActionPolicy) with Cancel = 0 and Allow = 1.
func (a *App) decideNavigation(action, handler objc.ID) {
	allow := false
	if u := action.Send(sel("request")).Send(sel("URL")); u != 0 {
		scheme := goString(u.Send(sel("scheme")))
		host := goString(u.Send(sel("host")))
		mainFrame := true
		if frame := action.Send(sel("targetFrame")); frame != 0 {
			mainFrame = objc.Send[bool](frame, sel("isMainFrame"))
		}
		allow = navAllowed(scheme, host, mainFrame)
	}
	if allow {
		invokeBlock1(handler, 1)
	} else {
		invokeBlock1(handler, 0)
	}
}
