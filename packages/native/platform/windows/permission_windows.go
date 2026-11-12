//go:build windows

package windows

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// The page's permission requests on WebView2: PermissionRequested for
// microphone, camera, geolocation, notifications and clipboard read, and
// ScreenCaptureStarting (ICoreWebView2_27) for getDisplayMedia. Both run
// under a deferral, so the SDK can show a prompt and answer later.
//
// UNTESTED against a live runtime until the Windows CI job runs the GUI test.
// getUserMedia({audio, video}) raises two PermissionRequested events, one per
// device; the SDK remembers the first answer, so one prompt covers both.
// A WebView2 runtime without ICoreWebView2_27 raises no event for screen
// capture and the runtime's own picker is the only gate.

var _ caps.PermissionRequests = (*App)(nil)

var permNextID atomic.Uint64

// permState holds what the handlers need from the App.
type permState struct {
	mu      sync.Mutex
	handler caps.PermissionHandler
}

// SetPermissionHandler routes the webview's permission requests to h.
// Without a handler the managed kinds are denied.
func (a *App) SetPermissionHandler(h caps.PermissionHandler) {
	a.perm.mu.Lock()
	a.perm.handler = h
	a.perm.mu.Unlock()
}

func (a *App) permHandler() caps.PermissionHandler {
	a.perm.mu.Lock()
	defer a.perm.mu.Unlock()
	return a.perm.handler
}

// hookPermissions installs the per-view handlers. Each view has its own, so
// the request is tied to the view that raised it.
func (a *App) hookPermissions(v *view) {
	var token int64
	v.hPerm = newHandler(iidPermissionRequestedHandler, false, func(sender, args uintptr) uintptr {
		return a.onPermission(v, args)
	})
	if hr := vcall(v.webview, wvAddPermissionRequested, uintptr(v.hPerm), uintptr(unsafe.Pointer(&token))); failed(hr) {
		logOnce("permission", "permissions: add_PermissionRequested failed: 0x%08x; the webview's own prompts stay on", uint32(hr))
	}
	wv27 := queryInterface(v.webview, iidWebView2_27)
	if wv27 == nil {
		logOnce("screencapture", "permissions: ICoreWebView2_27 is not available (older WebView2 runtime); screen capture is not gated by the app")
		return
	}
	defer comRelease(wv27)
	v.hScreen = newHandler(iidScreenCaptureStartingHandler, false, func(sender, args uintptr) uintptr {
		return a.onScreenCapture(v, args)
	})
	if hr := vcall(wv27, wv27AddScreenCapture, uintptr(v.hScreen), uintptr(unsafe.Pointer(&token))); failed(hr) {
		logOnce("screencapture2", "permissions: add_ScreenCaptureStarting failed: 0x%08x", uint32(hr))
	}
}

// onPermission is the PermissionRequested handler. It runs on the main thread;
// args is borrowed, so it is AddRef'd until the answer is applied.
func (a *App) onPermission(v *view, argsU uintptr) uintptr {
	if argsU == 0 {
		return sOK
	}
	args := ptr(argsU)
	var raw uint32
	if failed(vcall(args, permArgsGetPermissionKind, uintptr(unsafe.Pointer(&raw)))) {
		return sOK
	}
	kind, managed := permissionKindOf(raw)
	if !managed {
		return sOK
	}
	if deniedPermission(denyMediaCapture(a.cfg.Memory, a.cfg.Features), raw) {
		vcall(args, permArgsPutState, permStateDeny)
		return sOK
	}
	var defU uintptr
	if failed(vcall(args, permArgsGetDeferral, uintptr(unsafe.Pointer(&defU)))) || defU == 0 {
		return sOK
	}
	def := ptr(defU)
	comAddRef(args)
	origin := originOfURI(comString(args, permArgsGetUri))

	var once sync.Once
	answer := func(allow bool) {
		once.Do(func() {
			a.Dispatch(func() {
				vcall(args, permArgsPutState, permissionState(allow))
				// Do not let WebView2 keep the answer: the app owns the
				// remembered decisions (and can reset them).
				if a3 := queryInterface(args, iidPermissionArgs3); a3 != nil {
					vcall(a3, permArgs3PutSavesProfile, 0)
					comRelease(a3)
				}
				vcall(def, deferralComplete)
				comRelease(def)
				comRelease(args)
			})
		})
	}
	h := a.permHandler()
	if h == nil {
		answer(false)
		return sOK
	}
	h(caps.PermissionRequest{ID: permNextID.Add(1), View: v.ref, Origin: origin, Kinds: []caps.PermissionKind{kind}}, answer)
	return sOK
}

// onScreenCapture is the ScreenCaptureStarting handler: a denial cancels the
// capture, an allowance lets WebView2 show its own source picker.
func (a *App) onScreenCapture(v *view, argsU uintptr) uintptr {
	if argsU == 0 {
		return sOK
	}
	args := ptr(argsU)
	var defU uintptr
	if failed(vcall(args, scrArgsGetDeferral, uintptr(unsafe.Pointer(&defU)))) || defU == 0 {
		return sOK
	}
	def := ptr(defU)
	comAddRef(args)
	var once sync.Once
	answer := func(allow bool) {
		once.Do(func() {
			a.Dispatch(func() {
				if !allow {
					vcall(args, scrArgsPutCancel, 1)
				}
				vcall(def, deferralComplete)
				comRelease(def)
				comRelease(args)
			})
		})
	}
	h := a.permHandler()
	if h == nil {
		answer(false)
		return sOK
	}
	h(caps.PermissionRequest{ID: permNextID.Add(1), View: v.ref, Origin: Origin, Kinds: []caps.PermissionKind{caps.PermScreen}}, answer)
	return sOK
}
