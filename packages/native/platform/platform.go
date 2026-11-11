// Package platform is the thin, cgo-free window and webview layer: WebKitGTK
// on Linux (purego), WKWebView on macOS (purego objc) and WebView2 on Windows
// (COM over syscall).
//
// Every method on App and Window may be called from any goroutine; the
// implementation marshals the call onto the platform's main thread.
package platform

import "net/http"

// Options configures the application.
type Options struct {
	// AppID is the reverse-DNS application id, e.g. "com.acme.app".
	AppID string
	// Handler serves every request on the app origin. Responses must be
	// streamed to the webview as they are written (Flush is honored).
	Handler http.Handler
	// Debug enables the webview's developer tools.
	Debug bool
	// KeepAlive keeps the app running when its last window closes. The last
	// live window is then released (hidden, webview destroyed) rather than
	// destroyed, and Show brings it back; see caps.WindowRelease.
	KeepAlive bool
}

// WindowOptions configures one window.
type WindowOptions struct {
	Title     string
	Width     int
	Height    int
	Resizable bool
	// Path is the route to open, relative to the app origin ("/" by default).
	Path string
}

// App is the running application. New must be called on the main goroutine
// (the one that runs main, locked with runtime.LockOSThread in an init).
type App interface {
	// Run starts the platform event loop and blocks until Quit. onReady runs
	// on the main thread once the loop is running.
	Run(onReady func()) error
	// Dispatch runs fn on the main thread and returns without waiting.
	Dispatch(fn func())
	// NewWindow opens a window on the app origin.
	NewWindow(opts WindowOptions) (Window, error)
	// Origin is the app origin, "krate://app" or "https://app.localhost".
	Origin() string
	// Quit stops the event loop; Run returns.
	Quit()
}

// Window is one top-level window with one webview.
type Window interface {
	// Navigate loads path on the app origin.
	Navigate(path string)
	// Eval runs JavaScript in the page without waiting for a result.
	Eval(js string)
	SetTitle(title string)
	SetSize(width, height int)
	Close()
}
