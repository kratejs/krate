// Package platform is the thin, cgo-free window and webview layer: WebKitGTK
// on Linux (purego), WKWebView on macOS (purego objc) and WebView2 on Windows
// (COM over syscall).
//
// Every method on App and Window may be called from any goroutine; the
// implementation marshals the call onto the platform's main thread.
package platform

import (
	"net/http"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Options configures the application.
type Options struct {
	// AppID is the reverse-DNS application id, e.g. "com.acme.app".
	AppID string
	// AppName is the display name of the app (the product name). The Windows
	// backend shows it on toast notifications. Empty means AppID.
	AppName string
	// IconPath is an optional PNG or ICO file the Windows backend uses as the
	// default toast icon.
	IconPath string
	// Handler serves every request on the app origin. Responses must be
	// streamed to the webview as they are written (Flush is honored).
	Handler http.Handler
	// Debug enables the webview's developer tools.
	Debug bool
	// Memory selects the memory profile of the webview. The zero value is
	// the engine defaults.
	Memory Memory
	// KeepAlive keeps the app running when its last window closes. The last
	// live window is then released (hidden, webview destroyed) rather than
	// destroyed, and Show brings it back; see caps.WindowRelease.
	KeepAlive bool
	// Agent makes the app a menu-bar-only agent at launch: no dock icon and
	// no application menu (macOS NSApplicationActivationPolicyAccessory,
	// like LSUIElement in Info.plist). Other platforms ignore it.
	Agent bool
	// Remote asks the engine for a remote debugging endpoint (see
	// caps.Automation). The shell sets it only in dev runs and for apps
	// built with native.debug.
	Remote caps.RemoteDebug
	// Extensions prepares the engine for browser extensions; WebView2 must
	// know when it creates its environment. See caps.Extensions.
	Extensions bool
}

// Memory configures the webview's memory use.
type Memory struct {
	// Profile is "balanced", "minimal" or "performance"; empty means
	// "performance" (no levers).
	Profile string
	// Features says which optional engine features the app uses; the
	// profiles other than "performance" switch the others off.
	Features Features
}

// Features are the optional webview features an app uses.
type Features struct {
	Media, WebAudio, WebGL, WebRTC, SpellCheck bool
}

// Backend returns the profile name and feature map in the form the macOS and
// Windows backends take. An empty profile means "performance" (no levers),
// and the map is never nil, so a feature is switched off only when Features
// says so.
func (m Memory) Backend() (string, map[string]bool) {
	p := m.Profile
	if p == "" {
		p = "performance"
	}
	return p, map[string]bool{
		"media": m.Features.Media, "webaudio": m.Features.WebAudio, "webgl": m.Features.WebGL,
		"webrtc": m.Features.WebRTC, "spellcheck": m.Features.SpellCheck,
	}
}

// WindowOptions configures one window.
type WindowOptions struct {
	Title     string
	Width     int
	Height    int
	Resizable bool
	// Path is the route to open, relative to the app origin ("/" by default).
	Path string
	// Chrome is the window chrome the window is created with (title bar
	// style, transparency, backdrop and so on). The zero value is a native
	// window. Platforms apply what they can and ignore the rest.
	Chrome ChromeOptions
	// Offscreen opens a window that is never shown on screen: its page renders
	// and can be captured with caps.ViewSnapshots. Only Apps that implement
	// caps.OffscreenWindows honor it; the others open a normal window.
	Offscreen bool
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
