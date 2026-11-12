//go:build linux

package linux

import (
	"os"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.PermissionRequests = (*App)(nil)

// permAPI is the permission-request part of WebKitGTK and GObject. Every
// field may be nil on a WebKitGTK that lacks the symbol; the request types
// that need it are then left to the engine's default.
type permAPI struct {
	typeIsA func(inst, gtype uintptr) int32

	allow func(req uintptr)
	deny  func(req uintptr)

	userMediaType func() uintptr
	geoType       func() uintptr
	notifType     func() uintptr
	clipType      func() uintptr
	devInfoType   func() uintptr

	forAudio   func(req uintptr) int32
	forVideo   func(req uintptr) int32
	forDisplay func(req uintptr) int32

	setMock func(s uintptr, on int32)

	// The GType values, resolved on the main thread after the library loads.
	tUserMedia, tGeo, tNotif, tClip, tDevInfo uintptr
}

func loadPermAPI() *permAPI {
	p := &permAPI{}
	wk, err := purego.Dlopen("libwebkit2gtk-4.1.so.0", rtldNow|rtldGlobal)
	if err != nil {
		return p
	}
	gobj, err := purego.Dlopen("libgobject-2.0.so.0", rtldNow|rtldGlobal)
	if err != nil {
		return p
	}
	b := &binder{}
	b.bind(&p.typeIsA, gobj, "g_type_check_instance_is_a")
	b.bind(&p.allow, wk, "webkit_permission_request_allow")
	b.bind(&p.deny, wk, "webkit_permission_request_deny")
	b.bind(&p.userMediaType, wk, "webkit_user_media_permission_request_get_type")
	b.bind(&p.geoType, wk, "webkit_geolocation_permission_request_get_type")
	b.bind(&p.notifType, wk, "webkit_notification_permission_request_get_type")
	b.bind(&p.clipType, wk, "webkit_clipboard_permission_request_get_type")
	b.bind(&p.devInfoType, wk, "webkit_device_info_permission_request_get_type")
	b.bind(&p.forAudio, wk, "webkit_user_media_permission_is_for_audio_device")
	b.bind(&p.forVideo, wk, "webkit_user_media_permission_is_for_video_device")
	b.bind(&p.forDisplay, wk, "webkit_user_media_permission_is_for_display_device")
	b.bind(&p.setMock, wk, "webkit_settings_set_enable_mock_capture_devices")
	if p.userMediaType != nil {
		p.tUserMedia = p.userMediaType()
	}
	if p.geoType != nil {
		p.tGeo = p.geoType()
	}
	if p.notifType != nil {
		p.tNotif = p.notifType()
	}
	if p.clipType != nil {
		p.tClip = p.clipType()
	}
	if p.devInfoType != nil {
		p.tDevInfo = p.devInfoType()
	}
	return p
}

// is reports whether the GObject inst is of a type; false when the type is
// unknown to this WebKitGTK.
func (p *permAPI) is(inst, gtype uintptr) bool {
	return gtype != 0 && p.typeIsA != nil && p.typeIsA(inst, gtype) != 0
}

// classify turns a WebKitPermissionRequest into kinds. handled is false for a
// request type the engine keeps deciding (pointer lock, website data access,
// EME, XR, ...). A device-info request is handled with no kinds, which denies
// it: the page then sees device labels only after it holds a capture
// permission, as in other browsers.
func (p *permAPI) classify(req uintptr) (kinds []caps.PermissionKind, handled bool) {
	switch {
	case p.is(req, p.tUserMedia):
		if p.forAudio != nil && p.forAudio(req) != 0 {
			kinds = append(kinds, caps.PermMicrophone)
		}
		if p.forVideo != nil && p.forVideo(req) != 0 {
			kinds = append(kinds, caps.PermCamera)
		}
		if p.forDisplay != nil && p.forDisplay(req) != 0 {
			kinds = append(kinds, caps.PermScreen)
		}
		if len(kinds) == 0 {
			kinds = append(kinds, caps.PermOther)
		}
		return kinds, true
	case p.is(req, p.tGeo):
		return []caps.PermissionKind{caps.PermLocation}, true
	case p.is(req, p.tNotif):
		return []caps.PermissionKind{caps.PermNotifications}, true
	case p.is(req, p.tClip):
		return []caps.PermissionKind{caps.PermClipboardRead}, true
	case p.is(req, p.tDevInfo):
		return nil, true
	}
	return nil, false
}

var permNext struct {
	sync.Mutex
	n uint64
}

func nextPermID() uint64 {
	permNext.Lock()
	defer permNext.Unlock()
	permNext.n++
	return permNext.n
}

// SetPermissionHandler routes the webview's camera, microphone, screen
// capture, location, notification and clipboard-read requests to h. Without a
// handler they are denied. Request types the engine always decided itself
// (pointer lock, website data access) are not affected.
func (a *App) SetPermissionHandler(h caps.PermissionHandler) {
	a.mu.Lock()
	a.permHandler = h
	a.mu.Unlock()
}

// permissionCallback is the "permission-request" signal handler. It returns
// TRUE for the types it takes over; the request is kept (ref'd) until
// answered.
func permissionCallback(view, req, data uintptr) uintptr {
	a := theApp
	if a == nil || a.perm == nil || a.perm.allow == nil {
		return 0
	}
	kinds, handled := a.perm.classify(req)
	if !handled {
		return 0
	}
	a.mu.Lock()
	h := a.permHandler
	a.mu.Unlock()
	a.api.gObjectRef(req)
	var once sync.Once
	answer := func(allow bool) {
		once.Do(func() {
			a.Dispatch(func() {
				if allow {
					a.perm.allow(req)
				} else {
					a.perm.deny(req)
				}
				a.api.gObjectUnref(req)
			})
		})
	}
	if h == nil || len(kinds) == 0 {
		answer(false)
		return 1
	}
	h(caps.PermissionRequest{ID: nextPermID(), View: caps.ViewRef(data), Origin: Origin, Kinds: kinds}, answer)
	return 1
}

// applyMediaSettings turns on what getUserMedia needs when the page uses it,
// and the mock capture devices when KRATE_NATIVE_MOCK_CAPTURE is set (tests:
// real cameras and microphones are not available in CI).
func (a *App) applyMediaSettings(settings uintptr) {
	if a.perm == nil {
		return
	}
	mock := os.Getenv("KRATE_NATIVE_MOCK_CAPTURE") != ""
	if (a.features.WebRTC || mock) && a.mem != nil {
		for _, prop := range []string{"enable-media", "enable-media-stream", "enable-webrtc"} {
			if fn := a.mem.settings[prop]; fn != nil {
				fn(settings, 1)
			}
		}
	}
	if mock && a.perm.setMock != nil {
		a.perm.setMock(settings, 1)
	}
}
