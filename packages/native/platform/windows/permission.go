//go:build windows

package windows

import (
	"net/url"
	"strconv"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// The decision tables of the permission and privacy code, free of COM and
// Win32. The Windows side is
// permission_windows.go and privacy_windows.go.
//
// UNTESTED against a live runtime until the Windows CI job runs the GUI test:
// the enum values come from WebView2.h and the registry layout from
// Microsoft's capability access documentation.

// COREWEBVIEW2_PERMISSION_KIND values (WebView2.h, line 2364) that the SDK
// manages.
const (
	permKindGeolocation   = 3
	permKindNotifications = 4
	permKindClipboardRead = 6
)

// COREWEBVIEW2_PERMISSION_STATE values.
const (
	permStateDefault = 0
	permStateAllow   = 1
)

// permissionKindOf maps a COREWEBVIEW2_PERMISSION_KIND to a kind. managed is
// false for the kinds the SDK leaves to WebView2's own handling (sensors,
// downloads, file access, MIDI, fonts, window management).
func permissionKindOf(k uint32) (kind caps.PermissionKind, managed bool) {
	switch k {
	case permKindMicrophone:
		return caps.PermMicrophone, true
	case permKindCamera:
		return caps.PermCamera, true
	case permKindGeolocation:
		return caps.PermLocation, true
	case permKindNotifications:
		return caps.PermNotifications, true
	case permKindClipboardRead:
		return caps.PermClipboardRead, true
	}
	return "", false
}

// permissionState is the COREWEBVIEW2_PERMISSION_STATE for an answer.
func permissionState(allow bool) uintptr {
	if allow {
		return permStateAllow
	}
	return permStateDeny
}

// originOfURI returns the origin of the page URI a permission request
// carries; an unparsable or opaque one is the app origin, which is the only
// one the navigation lock lets in.
func originOfURI(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return Origin
	}
	return u.Scheme + "://" + u.Host
}

// consentStatus combines the values of a capability access manager key:
// "Deny" anywhere denies, otherwise "Allow" grants, and no value says nothing
// (unknown: the system keeps no switch the app can read).
func consentStatus(values ...string) caps.PrivacyStatus {
	allow := false
	for _, v := range values {
		switch v {
		case "Deny":
			return caps.PrivacyDenied
		case "Allow":
			allow = true
		}
	}
	if allow {
		return caps.PrivacyGranted
	}
	return caps.PrivacyUnknown
}

// consentKey is the ConsentStore subkey of a kind, "" when Windows has no
// switch for it.
func consentKey(kind caps.PermissionKind) string {
	switch kind {
	case caps.PermCamera:
		return "webcam"
	case caps.PermMicrophone:
		return "microphone"
	case caps.PermLocation:
		return "location"
	}
	return ""
}

// winRow is one top-level window found by EnumWindows.
type winRow struct {
	hwnd          uintptr
	title, app    string
	width, height int
	visible       bool
	cloaked       bool
	toolWindow    bool
}

// windowSources keeps the windows a user would pick from a list: visible,
// not cloaked (a window on another virtual desktop or a hidden UWP shell),
// not a tool window, titled and not tiny.
func windowSources(rows []winRow) []caps.CaptureSource {
	var out []caps.CaptureSource
	for _, w := range rows {
		if !w.visible || w.cloaked || w.toolWindow || w.title == "" || w.width < 40 || w.height < 40 {
			continue
		}
		out = append(out, caps.CaptureSource{
			ID: "window:" + strconv.FormatUint(uint64(w.hwnd), 10), Kind: "window", Name: w.title, App: w.app,
			Width: w.width, Height: w.height,
		})
	}
	return out
}

// monitorName names the n-th monitor (1-based).
func monitorName(n int) string { return "Screen " + strconv.Itoa(n) }
