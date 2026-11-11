//go:build linux

// Package linux implements the desktop platform layer on Linux with WebKitGTK,
// loaded at run time through purego (no cgo).
//
// All GTK and WebKit calls happen on one OS thread: the thread that runs
// main, locked in init. New must be called from that goroutine.
package linux

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/ebitengine/purego"
)

func init() {
	// Keeps the main goroutine on the process main thread, which GTK needs.
	runtime.LockOSThread()
}

// Logf receives diagnostics (blocked navigations, handler panics).
var Logf = log.Printf

func logf(format string, args ...any) { Logf(format, args...) }

// Origin is the app origin. Pages load from Origin + path.
const Origin = "krate://app"

// Options configures the application.
type Options struct {
	AppID   string
	Handler http.Handler
	Debug   bool
}

// WindowOptions configures one window.
type WindowOptions struct {
	Title     string
	Width     int
	Height    int
	Resizable bool
	Path      string
}

// App is the WebKitGTK application.
type App struct {
	api     *api
	handler http.Handler
	debug   bool
	ctx     uintptr // WebKitWebContext
	mainTid int

	mu          sync.Mutex
	windows     map[uintptr]*Window
	nextWin     uintptr
	onAllClosed func()

	queue struct {
		sync.Mutex
		fns       []func()
		scheduled bool
	}
	stopCh    chan struct{}
	running   atomic.Bool
	stopped   atomic.Bool
	quitEarly atomic.Bool
}

// The C callbacks are created once; purego limits how many may exist.
var (
	theApp *App

	cbIdle, cbScheme, cbPolicy, cbLoad, cbDestroy, cbClose, cbHeader uintptr
	curHeader                                                        http.Header
)

// New initializes GTK and WebKit. It must run on the main goroutine.
func New(opts Options) (*App, error) {
	if theApp != nil {
		return nil, errors.New("krate: only one App per process")
	}
	if opts.Handler == nil {
		return nil, errors.New("krate: Options.Handler is required")
	}
	a, err := loadAPI41()
	if err != nil {
		return nil, err
	}
	app := &App{api: a, handler: opts.Handler, debug: opts.Debug, windows: map[uintptr]*Window{}, stopCh: make(chan struct{}), mainTid: syscall.Gettid()}
	if syscall.Getpid() != app.mainTid {
		return nil, errors.New("krate: New must be called on the main thread (call from the main goroutine)")
	}

	id := opts.AppID
	if id == "" {
		id = "krate-app"
	}
	a.gSetPrgname(id)
	a.gSetApplicationName(id)
	if a.gtkInitCheck(0, 0) == 0 {
		return nil, errNoDisplay
	}

	cbIdle = purego.NewCallback(idleCallback)
	cbScheme = purego.NewCallback(schemeCallback)
	cbPolicy = purego.NewCallback(policyCallback)
	cbLoad = purego.NewCallback(loadCallback)
	cbDestroy = purego.NewCallback(destroyCallback)
	cbClose = purego.NewCallback(closeCallback)
	cbHeader = purego.NewCallback(headerCallback)

	mgr := a.wkDataManagerNew("base-data-directory", dataDir("XDG_DATA_HOME", ".local/share", id),
		"base-cache-directory", dataDir("XDG_CACHE_HOME", ".cache", id), 0)
	if mgr == 0 {
		return nil, errors.New("krate: cannot create website data manager")
	}
	app.ctx = a.wkContextNewWithMgr(mgr)
	if app.ctx == 0 {
		return nil, errors.New("krate: cannot create web context")
	}
	a.wkContextRegisterSchem(app.ctx, "krate", cbScheme, 0, 0)
	sm := a.wkContextSecurityMgr(app.ctx)
	a.wkSecSecure(sm, "krate")
	a.wkSecCORS(sm, "krate")

	theApp = app
	return app, nil
}

func dataDir(env, fallback, id string) string {
	base := os.Getenv(env)
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, fallback)
	}
	dir := filepath.Join(base, id)
	_ = os.MkdirAll(dir, 0o700)
	return dir
}

// WebKitVersion returns the loaded WebKitGTK version, e.g. "2.54.0".
func (a *App) WebKitVersion() string { return a.api.version() }

// Origin returns "krate://app".
func (a *App) Origin() string { return Origin }

// OnAllClosed sets what happens when the last window closes. The default
// (nil) quits the application.
func (a *App) OnAllClosed(fn func()) {
	a.mu.Lock()
	a.onAllClosed = fn
	a.mu.Unlock()
}

// Run runs the GTK main loop until Quit. onReady runs on the main thread
// once the loop is running.
func (a *App) Run(onReady func()) error {
	if !a.onMain() {
		return errors.New("krate: Run must be called on the main thread")
	}
	if a.running.Swap(true) {
		return errors.New("krate: already running")
	}
	if a.quitEarly.Load() {
		a.markStopped()
		return nil
	}
	if onReady != nil {
		a.Dispatch(onReady)
	}
	a.api.gtkMain()
	a.markStopped()
	return nil
}

func (a *App) markStopped() {
	if !a.stopped.Swap(true) {
		close(a.stopCh)
	}
}

// Quit stops the main loop.
func (a *App) Quit() {
	a.Dispatch(func() {
		if a.running.Load() {
			a.api.gtkMainQuit()
		} else {
			a.quitEarly.Store(true)
		}
	})
}

func (a *App) onMain() bool { return syscall.Gettid() == a.mainTid }

// Dispatch queues fn to run on the main thread. One idle source drains the
// whole queue, so the number of C callbacks stays fixed.
func (a *App) Dispatch(fn func()) {
	a.queue.Lock()
	a.queue.fns = append(a.queue.fns, fn)
	schedule := !a.queue.scheduled
	a.queue.scheduled = true
	a.queue.Unlock()
	if schedule {
		a.api.gIdleAddFull(0, cbIdle, 0, 0)
	}
}

// callMain runs fn on the main thread and waits for it. It reports false if
// the loop has already stopped.
func (a *App) callMain(fn func()) bool {
	if a.onMain() {
		fn()
		return true
	}
	if a.stopped.Load() {
		return false
	}
	done := make(chan struct{})
	a.Dispatch(func() { fn(); close(done) })
	select {
	case <-done:
		return true
	case <-a.stopCh:
		return false
	}
}

func idleCallback(data uintptr) uintptr {
	a := theApp
	a.queue.Lock()
	fns := a.queue.fns
	a.queue.fns = nil
	a.queue.scheduled = false
	a.queue.Unlock()
	for _, fn := range fns {
		runGuarded(fn)
	}
	return 0 // G_SOURCE_REMOVE
}

func runGuarded(fn func()) {
	defer func() {
		if p := recover(); p != nil {
			logf("krate: panic on main thread: %v", p)
		}
	}()
	fn()
}

func (a *App) windowByID(id uintptr) *Window {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.windows[id]
}

// NewWindow opens a window on the app origin.
func (a *App) NewWindow(opts WindowOptions) (*Window, error) {
	return a.NewWindowNotify(opts, nil)
}

// NewWindowNotify opens a window and installs onLoad (see OnLoadFinished)
// before the first page can finish loading.
func (a *App) NewWindowNotify(opts WindowOptions, onLoad func()) (*Window, error) {
	var w *Window
	var err error
	if !a.callMain(func() { w, err = a.newWindow(opts, onLoad) }) {
		return nil, fmt.Errorf("krate: application is not running")
	}
	return w, err
}
