//go:build windows

package windows

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	xw "golang.org/x/sys/windows"
)

func init() {
	// The Win32 message loop, COM apartment and every window must live on
	// one OS thread. Package init runs on the main goroutine's thread;
	// locking here keeps main on it.
	runtime.LockOSThread()
}

// Config configures the application.
type Config struct {
	// AppID names the per-user data folder (%LOCALAPPDATA%\<AppID>\WebView2).
	AppID string
	// Handler serves every request on the app origin.
	Handler http.Handler
	// Debug enables the developer tools.
	Debug bool
	// KeepAlive stops the app from quitting when its last window closes.
	KeepAlive bool
}

// WindowConfig configures one window. Width and Height are the client area in
// device-independent pixels (1/96 inch).
type WindowConfig struct {
	Title     string
	Width     int
	Height    int
	Resizable bool
	Path      string
}

// App is the running application.
type App struct {
	cfg Config

	q       queue
	mainTID uint32
	msgWnd  uintptr
	hInst   uintptr

	// Main-thread state.
	env         unsafe.Pointer // ICoreWebView2Environment
	envWaiters  []func()
	envStarted  bool
	windows     map[uintptr]*Window
	everOpened  bool
	runErr      error
	hResource   unsafe.Pointer // shared WebResourceRequested handler
	hNavigation unsafe.Pointer // shared NavigationStarting handler
	hFrameNav   unsafe.Pointer // shared FrameNavigationStarting handler
	hNewWindow  unsafe.Pointer // shared NewWindowRequested handler

	exited chan struct{}
	once   sync.Once
}

var (
	theApp   *App
	appMu    sync.Mutex
	classOne sync.Once
	classErr error
)

const (
	appClassName = "KrateNativeWindow"
	msgClassName = "KrateNativeMessage"
)

// New creates the application. It must be called on the main goroutine (the
// thread locked by this package's init).
func New(cfg Config) (*App, error) {
	appMu.Lock()
	defer appMu.Unlock()
	if theApp != nil {
		return nil, errors.New("windows: App already created")
	}
	if cfg.Handler == nil {
		return nil, errors.New("windows: Config.Handler is required")
	}
	initCallbacks()
	setDPIAware()

	hr, _, _ := pCoInitializeEx.Call(0, coinitApartment)
	if failed(hr) && uint32(hr) != rpcEChangedMode {
		return nil, fmt.Errorf("windows: CoInitializeEx failed: 0x%08x", uint32(hr))
	}

	a := &App{
		cfg:     cfg,
		mainTID: currentThreadID(),
		windows: map[uintptr]*Window{},
		exited:  make(chan struct{}),
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	a.hInst = h
	if err := a.registerClasses(); err != nil {
		return nil, err
	}
	hwnd, _, e := pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(wstr(msgClassName))), 0, 0, 0, 0, 0, 0,
		hwndMessage, 0, a.hInst, 0)
	if hwnd == 0 {
		return nil, fmt.Errorf("windows: creating message window: %v", e)
	}
	a.msgWnd = hwnd

	a.hResource = newHandler(iidWebResourceRequestedHandler, false, a.onResourceRequested)
	a.hNavigation = newHandler(iidNavigationStartingHandler, false, a.onNavigationStarting)
	a.hFrameNav = newHandler(iidNavigationStartingHandler, false, a.onFrameNavigationStarting)
	a.hNewWindow = newHandler(iidNewWindowRequestedHandler, false, a.onNewWindowRequested)
	theApp = a
	return a, nil
}

func (a *App) registerClasses() error {
	classOne.Do(func() {
		cursor, _, _ := pLoadCursorW.Call(0, idcArrow)
		wc := wndClassEx{
			size:       uint32(unsafe.Sizeof(wndClassEx{})),
			style:      csHRedraw | csVRedraw,
			wndProc:    wndProcPtr(),
			instance:   a.hInst,
			cursor:     cursor,
			background: colorWindow + 1,
			className:  wstr(appClassName),
		}
		if r, _, e := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			classErr = fmt.Errorf("windows: RegisterClassEx: %v", e)
			return
		}
		mc := wndClassEx{
			size:      uint32(unsafe.Sizeof(wndClassEx{})),
			wndProc:   wndProcPtr(),
			instance:  a.hInst,
			className: wstr(msgClassName),
		}
		if r, _, e := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&mc))); r == 0 {
			classErr = fmt.Errorf("windows: RegisterClassEx: %v", e)
		}
	})
	return classErr
}

// Origin returns the app origin.
func (a *App) Origin() string { return Origin }

func (a *App) onMain() bool { return currentThreadID() == a.mainTID }

// Dispatch runs fn on the main thread and returns without waiting.
func (a *App) Dispatch(fn func()) {
	if !a.q.push(fn) {
		return
	}
	for i := 0; i < 100; i++ {
		if r, _, _ := pPostMessageW.Call(a.msgWnd, wmDispatch, 0, 0); r != 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	a.q.wakeFailed()
	logf("could not wake the main thread; Dispatch is stalled until the next call")
}

// Quit stops the event loop; Run returns.
func (a *App) Quit() {
	a.Dispatch(func() { pPostQuitMessage.Call(0) })
}

// Run starts the event loop and blocks until Quit. onReady runs on the main
// thread once the WebView2 environment exists and the loop is pumping.
func (a *App) Run(onReady func()) error {
	if !a.onMain() {
		return errors.New("windows: Run must be called on the main thread")
	}
	defer a.once.Do(func() { close(a.exited) })

	if err := a.startEnvironment(); err != nil {
		return err
	}
	a.withEnv(func() {
		if onReady != nil {
			onReady()
		}
	})

	var m msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}

	for h, w := range a.windows {
		w.markNoQuit()
		pDestroyWindow.Call(h)
	}
	return a.runErr
}

// pumpOnce waits up to ms milliseconds for input and dispatches pending
// messages. It reports false if WM_QUIT was seen (and re-posts it), so a
// caller waiting in a nested loop can give up.
func (a *App) pumpOnce(ms uint32) bool {
	pMsgWaitForMultiple.Call(0, 0, uintptr(ms), qsAllInput, mwmoInputAvailable)
	var m msg
	for {
		r, _, _ := pPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmRemove)
		if r == 0 {
			return true
		}
		if m.message == wmQuit {
			pPostQuitMessage.Call(m.wParam)
			return false
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// startEnvironment asks WebView2 to create the shared environment. The result
// arrives through the completion handler once the loop runs.
func (a *App) startEnvironment() error {
	if a.envStarted {
		return nil
	}
	a.envStarted = true
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		d, err := os.UserCacheDir()
		if err != nil {
			return fmt.Errorf("windows: cannot locate %%LOCALAPPDATA%%: %w", err)
		}
		local = d
	}
	dir := userDataDir(local, a.cfg.AppID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("windows: creating user data folder: %w", err)
	}
	handler := newHandler(iidEnvCompletedHandler, true, a.environmentCompleted)
	if err := createEnvironment(dir, handler); err != nil {
		a.envStarted = false
		return err
	}
	return nil
}

func (a *App) environmentCompleted(hr, env uintptr) uintptr {
	if failed(hr) || env == 0 {
		a.runErr = fmt.Errorf("windows: creating the WebView2 environment failed: 0x%08x", uint32(hr))
		pPostQuitMessage.Call(1)
		return sOK
	}
	a.env = ptr(env)
	comAddRef(a.env)
	waiters := a.envWaiters
	a.envWaiters = nil
	for _, f := range waiters {
		f()
	}
	return sOK
}

// withEnv runs fn once the environment exists (main thread only).
func (a *App) withEnv(fn func()) {
	if a.env != nil {
		fn()
		return
	}
	a.envWaiters = append(a.envWaiters, fn)
}

// NewWindow opens a window on the app origin. Called from another goroutine
// it returns once the webview is ready (or failed). Called on the main thread
// (for example from onReady) it cannot wait, so it returns immediately and
// the page loads in the background; Navigate and Eval calls are queued until
// the webview exists.
func (a *App) NewWindow(cfg WindowConfig) (*Window, error) {
	w := &Window{app: a, ready: make(chan struct{})}
	if a.onMain() {
		if err := a.createWindow(cfg, w); err != nil {
			return nil, err
		}
		return w, nil
	}
	created := make(chan error, 1)
	a.Dispatch(func() { created <- a.createWindow(cfg, w) })
	select {
	case err := <-created:
		if err != nil {
			return nil, err
		}
	case <-a.exited:
		return nil, errors.New("windows: app is not running")
	}
	select {
	case <-w.ready:
	case <-a.exited:
		return nil, errors.New("windows: app stopped before the window was ready")
	}
	if w.err != nil {
		return nil, w.err
	}
	return w, nil
}

var wndProcCB atomic.Uintptr

func wndProcPtr() uintptr {
	if v := wndProcCB.Load(); v != 0 {
		return v
	}
	v := xw.NewCallback(wndProc)
	wndProcCB.Store(v)
	return v
}

func wndProc(hwnd, message, wParam, lParam uintptr) (ret uintptr) {
	defer func() {
		if r := recover(); r != nil {
			logf("panic in window procedure: %v", r)
			ret = 0
		}
	}()
	a := theApp
	if a == nil {
		r, _, _ := pDefWindowProcW.Call(hwnd, message, wParam, lParam)
		return r
	}
	if hwnd == a.msgWnd {
		if message == wmDispatch {
			a.q.drain()
			return 0
		}
		r, _, _ := pDefWindowProcW.Call(hwnd, message, wParam, lParam)
		return r
	}
	if w := a.windows[hwnd]; w != nil {
		if handled, r := winExtMessage(a, w, message, wParam, lParam); handled {
			return r
		}
		switch message {
		case wmSize:
			if wParam != sizeMinimized {
				w.resize()
			}
		case wmSetFocus:
			w.focus()
		case wmDpiChanged:
			rc := (*rect)(ptr(lParam))
			pSetWindowPos.Call(hwnd, 0, uintptr(rc.left), uintptr(rc.top),
				uintptr(rc.right-rc.left), uintptr(rc.bottom-rc.top), swpNoZOrder|swpNoActivate)
			return 0
		case wmDestroy:
			a.windowDestroyed(w)
		}
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, message, wParam, lParam)
	return r
}

func (a *App) windowDestroyed(w *Window) {
	w.destroy()
	delete(a.windows, w.hwnd)
	if len(a.windows) == 0 && a.everOpened && !a.cfg.KeepAlive && !w.noQuit {
		pPostQuitMessage.Call(0)
	}
}
