//go:build darwin

package darwin

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/kratejs/krate/packages/native/internal/thumb"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Camera, microphone, screen capture and location on macOS: the WKUIDelegate
// that routes the page's requests to the SDK, the operating system's consent
// state (AVFoundation, CoreGraphics, CoreLocation) and the list of capturable
// screens and windows (CoreGraphics).
//
// UNTESTED against the real frameworks until the macOS CI job runs the GUI
// test (gui_perm_test.go): written from Apple's headers. No camera or
// microphone exists on a CI runner, so the capture path itself cannot run
// there; the test drives the delegate methods directly.

var (
	_ caps.PermissionRequests = (*App)(nil)
	_ caps.Privacy            = (*App)(nil)
	_ caps.CaptureSources     = (*App)(nil)
)

var permNextID atomic.Uint64

// permState holds what the delegate needs from the App.
type permState struct {
	mu      sync.Mutex
	handler caps.PermissionHandler
}

// SetPermissionHandler routes the webview's media capture requests to h.
// Without a handler they are denied.
func (a *App) SetPermissionHandler(h caps.PermissionHandler) {
	a.perm.mu.Lock()
	a.perm.handler = h
	a.perm.mu.Unlock()
}

var blockFns struct {
	once    sync.Once
	copy    func(b uintptr) uintptr
	release func(b uintptr)
}

func loadBlockFns() {
	blockFns.once.Do(func() {
		h, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&blockFns.copy, h, "_Block_copy")
		purego.RegisterLibFunc(&blockFns.release, h, "_Block_release")
	})
}

// registerUIClass creates the WKUIDelegate. Methods outside the protocol (the
// private display capture one) are added to the class like the others; WebKit
// finds them with respondsToSelector.
func registerUIClass(a *App) error {
	protos := []*objc.Protocol{}
	if p := objc.GetProtocol("WKUIDelegate"); p != nil {
		protos = append(protos, p)
	}
	_, err := objc.RegisterClass("KrateUIDelegate", objc.GetClass("NSObject"), protos, nil, []objc.MethodDef{
		{Cmd: sel("webView:requestMediaCapturePermissionForOrigin:initiatedByFrame:type:decisionHandler:"),
			Fn: func(self objc.ID, _ objc.SEL, webView, origin, frame objc.ID, typ int, handler objc.ID) {
				a.decide(webView, origin, handler, captureKinds(typ), captureDecision)
			}},
		{Cmd: sel("_webView:requestDisplayCapturePermissionForOrigin:initiatedByFrame:withSystemAudio:decisionHandler:"),
			Fn: func(self objc.ID, _ objc.SEL, webView, origin, frame objc.ID, audio bool, handler objc.ID) {
				a.decide(webView, origin, handler, []caps.PermissionKind{caps.PermScreen}, displayDecision)
			}},
		{Cmd: sel("webView:requestGeolocationPermissionForOrigin:initiatedByFrame:decisionHandler:"),
			Fn: func(self objc.ID, _ objc.SEL, webView, origin, frame objc.ID, handler objc.ID) {
				a.decide(webView, origin, handler, []caps.PermissionKind{caps.PermLocation}, captureDecision)
			}},
	})
	return err
}

// decide hands one delegate request to the handler. The decision handler is a
// block WebKit gave us; it is copied so it can be called after this method
// returns, and called once on the main thread.
func (a *App) decide(webView, origin, handler objc.ID, kinds []caps.PermissionKind, decision func(bool) int) {
	loadBlockFns()
	var block uintptr
	if handler != 0 && blockFns.copy != nil {
		block = blockFns.copy(uintptr(handler))
	}
	var once sync.Once
	answer := func(allow bool) {
		once.Do(func() {
			dispatch(func() {
				if block != 0 {
					invokeBlock1(objc.ID(block), uintptr(decision(allow)))
					blockFns.release(block)
				} else if handler != 0 {
					invokeBlock1(handler, uintptr(decision(allow)))
				}
			})
		})
	}
	a.perm.mu.Lock()
	h := a.perm.handler
	a.perm.mu.Unlock()
	if h == nil || len(kinds) == 0 {
		answer(false)
		return
	}
	var ref caps.ViewRef
	if v := a.byWeb[webView]; v != nil {
		ref = v.ref
	}
	var proto, host string
	var port int
	if origin != 0 {
		proto, host = goString(origin.Send(sel("protocol"))), goString(origin.Send(sel("host")))
		port = objc.Send[int](origin, sel("port"))
	}
	h(caps.PermissionRequest{ID: permNextID.Add(1), View: ref, Origin: originString(proto, host, port), Kinds: kinds}, answer)
}

// Operating system consent.

var frameworks struct {
	once sync.Once
	cg   uintptr
	cf   uintptr

	preflight func() bool
	request   func() bool

	displayList  func(max uint32, ids *uint32, count *uint32) int32
	displayWide  func(id uint32) uintptr
	displayHigh  func(id uint32) uintptr
	displayImage func(id uint32) uintptr
	windowInfo   func(opts, rel uint32) uintptr
	windowImage  func(rect nsRect, opts, win, imgOpts uint32) uintptr
	imageRelease func(img uintptr)
	cfRelease    func(o uintptr)
}

func loadFrameworks() {
	frameworks.once.Do(func() {
		for _, f := range []string{
			"/System/Library/Frameworks/AVFoundation.framework/AVFoundation",
			"/System/Library/Frameworks/CoreLocation.framework/CoreLocation",
		} {
			_, _ = purego.Dlopen(f, purego.RTLD_GLOBAL|purego.RTLD_NOW)
		}
		cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			return
		}
		cf, _ := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		bind := func(fn any, lib uintptr, name string) {
			if lib == 0 {
				return
			}
			if _, err := purego.Dlsym(lib, name); err == nil {
				purego.RegisterLibFunc(fn, lib, name)
			}
		}
		bind(&frameworks.preflight, cg, "CGPreflightScreenCaptureAccess")
		bind(&frameworks.request, cg, "CGRequestScreenCaptureAccess")
		bind(&frameworks.displayList, cg, "CGGetActiveDisplayList")
		bind(&frameworks.displayWide, cg, "CGDisplayPixelsWide")
		bind(&frameworks.displayHigh, cg, "CGDisplayPixelsHigh")
		bind(&frameworks.displayImage, cg, "CGDisplayCreateImage")
		bind(&frameworks.windowInfo, cg, "CGWindowListCopyWindowInfo")
		bind(&frameworks.windowImage, cg, "CGWindowListCreateImage")
		bind(&frameworks.imageRelease, cg, "CGImageRelease")
		bind(&frameworks.cfRelease, cf, "CFRelease")
	})
}

var errNoPrivacy = errors.New("darwin: this macOS has no such privacy switch")

const (
	avMediaVideo = "vide"
	avMediaAudio = "soun"
)

// PrivacyStatus reads the system's consent without prompting.
func (a *App) PrivacyStatus(kind caps.PermissionKind) (st caps.PrivacyStatus, err error) {
	loadFrameworks()
	withPool(func() { st, err = privacyStatus(kind) })
	return st, err
}

func privacyStatus(kind caps.PermissionKind) (caps.PrivacyStatus, error) {
	switch kind {
	case caps.PermCamera, caps.PermMicrophone:
		dev := cls("AVCaptureDevice")
		if dev == 0 {
			return caps.PrivacyUnknown, errNoPrivacy
		}
		mt := avMediaVideo
		if kind == caps.PermMicrophone {
			mt = avMediaAudio
		}
		return privacyFromAV(objc.Send[int](dev, sel("authorizationStatusForMediaType:"), nsString(mt))), nil
	case caps.PermScreen:
		if frameworks.preflight == nil {
			return caps.PrivacyUnknown, errNoPrivacy
		}
		return privacyFromScreen(frameworks.preflight()), nil
	case caps.PermLocation:
		cl := cls("CLLocationManager")
		if cl == 0 {
			return caps.PrivacyUnknown, errNoPrivacy
		}
		mgr := cl.Send(sel("new"))
		defer release(mgr)
		if s := sel("authorizationStatus"); respondsTo(mgr, s) {
			return privacyFromCL(objc.Send[int](mgr, s)), nil
		}
		return privacyFromCL(objc.Send[int](cl, sel("authorizationStatus"))), nil
	}
	return caps.PrivacyUnknown, fmt.Errorf("darwin: %q has no operating system privacy switch", kind)
}

const privacyAskTimeout = 2 * time.Minute

// RequestPrivacy shows the system prompt for a kind whose state is not
// determined and waits for the answer.
func (a *App) RequestPrivacy(kind caps.PermissionKind) (caps.PrivacyStatus, error) {
	st, err := a.PrivacyStatus(kind)
	if err != nil || st != caps.PrivacyNotDetermined {
		return st, err
	}
	switch kind {
	case caps.PermCamera, caps.PermMicrophone:
		mt := avMediaVideo
		if kind == caps.PermMicrophone {
			mt = avMediaAudio
		}
		done := make(chan bool, 1)
		runOnMain(func() {
			withPool(func() {
				block := objc.NewBlock(func(_ objc.Block, granted bool) { done <- granted })
				defer block.Release()
				cls("AVCaptureDevice").Send(sel("requestAccessForMediaType:completionHandler:"), nsString(mt), objc.ID(block))
			})
		})
		select {
		case <-done:
		case <-time.After(privacyAskTimeout):
		}
	case caps.PermScreen:
		// The call returns at once; the user answers in System Settings and
		// the grant takes effect after the app restarts.
		if frameworks.request != nil {
			frameworks.request()
		}
	case caps.PermLocation:
		runOnMain(func() {
			withPool(func() {
				mgr := cls("CLLocationManager").Send(sel("new"))
				mgrKeep(mgr)
				if s := sel("requestWhenInUseAuthorization"); respondsTo(mgr, s) {
					mgr.Send(s)
				}
			})
		})
		deadline := time.Now().Add(privacyAskTimeout)
		for time.Now().Before(deadline) {
			if s, _ := a.PrivacyStatus(kind); s != caps.PrivacyNotDetermined {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	return a.PrivacyStatus(kind)
}

// The location manager that asked must outlive the call: the system shows
// its prompt for as long as the manager lives.
var (
	keepMu  sync.Mutex
	keepMgr objc.ID
)

func mgrKeep(m objc.ID) {
	keepMu.Lock()
	defer keepMu.Unlock()
	if keepMgr != 0 {
		release(keepMgr)
	}
	keepMgr = m
}

// Capture sources.

const (
	cgWindowListOnScreenOnly       = 1
	cgWindowListIncludingWindow    = 8
	cgWindowListExcludeDesktop     = 16
	cgWindowImageBoundsIgnoreFrame = 1
	nsBitmapImageFileTypePNG       = 4
)

// CaptureSources lists the screens and windows with CoreGraphics. Window
// titles and thumbnails need the Screen Recording permission; without it
// macOS returns names for the app's own windows only and blank images.
// ScreenCaptureKit is not used: it is asynchronous and the picker of
// getDisplayMedia cannot take its result anyway.
func (a *App) CaptureSources(thumbWidth int) (out []caps.CaptureSource, err error) {
	loadFrameworks()
	if frameworks.displayList == nil {
		return nil, fmt.Errorf("%w: CoreGraphics display functions are not available", krate.ErrUnsupported)
	}
	withPool(func() { out = captureSources(thumbWidth) })
	return out, nil
}

func captureSources(thumbWidth int) []caps.CaptureSource {
	var out []caps.CaptureSource
	var ids [16]uint32
	var n uint32
	if frameworks.displayList(uint32(len(ids)), &ids[0], &n) == 0 {
		for i := uint32(0); i < n; i++ {
			id := ids[i]
			s := caps.CaptureSource{ID: fmt.Sprintf("screen:%d", id), Kind: "screen", Name: screenName(int(i) + 1)}
			if frameworks.displayWide != nil {
				s.Width, s.Height = int(frameworks.displayWide(id)), int(frameworks.displayHigh(id))
			}
			if thumbWidth > 0 && frameworks.displayImage != nil {
				if img := frameworks.displayImage(id); img != 0 {
					s.Thumbnail = cgImagePNG(img, thumbWidth)
					frameworks.imageRelease(img)
				}
			}
			out = append(out, s)
		}
	}
	if frameworks.windowInfo == nil {
		return out
	}
	arr := frameworks.windowInfo(cgWindowListOnScreenOnly|cgWindowListExcludeDesktop, 0)
	if arr == 0 {
		return out
	}
	defer frameworks.cfRelease(arr)
	list := objc.ID(arr)
	count := objc.Send[uint](list, sel("count"))
	num := func(d objc.ID, key string) float64 {
		v := d.Send(sel("objectForKey:"), nsString(key))
		if v == 0 {
			return 0
		}
		return objc.Send[float64](v, sel("doubleValue"))
	}
	str := func(d objc.ID, key string) string { return goString(d.Send(sel("objectForKey:"), nsString(key))) }
	var rows []cgWindow
	for i := uint(0); i < count; i++ {
		d := list.Send(sel("objectAtIndex:"), i)
		w := cgWindow{number: int(num(d, "kCGWindowNumber")), layer: int(num(d, "kCGWindowLayer")),
			name: str(d, "kCGWindowName"), owner: str(d, "kCGWindowOwnerName")}
		if b := d.Send(sel("objectForKey:"), nsString("kCGWindowBounds")); b != 0 {
			w.width, w.height = num(b, "Width"), num(b, "Height")
		}
		rows = append(rows, w)
	}
	for _, s := range windowSources(rows) {
		if thumbWidth > 0 && frameworks.windowImage != nil {
			var num int
			fmt.Sscanf(s.ID, "window:%d", &num)
			// CGRectNull asks for the window's own bounds.
			null := nsRect{Origin: nsPoint{X: math.Inf(1), Y: math.Inf(1)}}
			if img := frameworks.windowImage(null, cgWindowListIncludingWindow, uint32(num), cgWindowImageBoundsIgnoreFrame); img != 0 {
				s.Thumbnail = cgImagePNG(img, thumbWidth)
				frameworks.imageRelease(img)
			}
		}
		out = append(out, s)
	}
	return out
}

// cgImagePNG encodes a CGImage as a PNG thumbnail of the given width.
func cgImagePNG(img uintptr, width int) []byte {
	rep := cls("NSBitmapImageRep").Send(sel("alloc")).Send(sel("initWithCGImage:"), img)
	if rep == 0 {
		return nil
	}
	defer release(rep)
	data := rep.Send(sel("representationUsingType:properties:"), nsBitmapImageFileTypePNG, objc.ID(0))
	return thumb.ScalePNG(dataBytes(data), width)
}
