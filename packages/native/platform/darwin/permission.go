//go:build darwin

package darwin

import (
	"strconv"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// The decision tables of the permission and privacy code, free of
// Objective-C. The Objective-C side is permission_darwin.go.
//
// UNTESTED against the live frameworks until the macOS CI job runs the GUI
// test: the selectors and enum values come from Apple's WebKit,
// AVFoundation, CoreLocation and CoreGraphics headers.

// WKMediaCaptureType (WKUIDelegate, macOS 12).
const (
	wkMediaCaptureCamera              = 0
	wkMediaCaptureMicrophone          = 1
	wkMediaCaptureCameraAndMicrophone = 2
)

// WKPermissionDecision: what the media capture decision handler takes.
const (
	wkPermissionPrompt = 0
	wkPermissionGrant  = 1
	wkPermissionDeny   = 2
)

// _WKDisplayCapturePermissionDecision, the private handler of
// _webView:requestDisplayCapturePermissionForOrigin:initiatedByFrame:
// withSystemAudio:decisionHandler:. Granting shows the system's picker.
const (
	wkDisplayDeny         = 0
	wkDisplayScreenPrompt = 1
)

// captureKinds maps a WKMediaCaptureType to permission kinds. An unknown type
// has no kinds, which denies it.
func captureKinds(t int) []caps.PermissionKind {
	switch t {
	case wkMediaCaptureCamera:
		return []caps.PermissionKind{caps.PermCamera}
	case wkMediaCaptureMicrophone:
		return []caps.PermissionKind{caps.PermMicrophone}
	case wkMediaCaptureCameraAndMicrophone:
		return []caps.PermissionKind{caps.PermCamera, caps.PermMicrophone}
	}
	return nil
}

// captureDecision is the WKPermissionDecision for an answer.
func captureDecision(allow bool) int {
	if allow {
		return wkPermissionGrant
	}
	return wkPermissionDeny
}

// displayDecision is the display capture decision for an answer.
func displayDecision(allow bool) int {
	if allow {
		return wkDisplayScreenPrompt
	}
	return wkDisplayDeny
}

// originString writes a WKSecurityOrigin as the page's origin. A nil origin
// (protocol "" and host "") is the app origin, which the delegate cannot be
// asked about by anything else.
func originString(protocol, host string, port int) string {
	if protocol == "" && host == "" {
		return OriginURL
	}
	s := protocol + "://" + host
	if port > 0 {
		s += ":" + strconv.Itoa(port)
	}
	return s
}

// AVAuthorizationStatus values.
const (
	avNotDetermined = 0
	avRestricted    = 1
	avDenied        = 2
	avAuthorized    = 3
)

// privacyFromAV maps AVCaptureDevice.authorizationStatus.
func privacyFromAV(s int) caps.PrivacyStatus {
	switch s {
	case avNotDetermined:
		return caps.PrivacyNotDetermined
	case avRestricted:
		return caps.PrivacyRestricted
	case avDenied:
		return caps.PrivacyDenied
	case avAuthorized:
		return caps.PrivacyGranted
	}
	return caps.PrivacyUnknown
}

// CLAuthorizationStatus values (macOS).
const (
	clNotDetermined       = 0
	clRestricted          = 1
	clDenied              = 2
	clAuthorizedAlways    = 3
	clAuthorizedWhenInUse = 4
)

// privacyFromCL maps CLLocationManager.authorizationStatus.
func privacyFromCL(s int) caps.PrivacyStatus {
	switch s {
	case clNotDetermined:
		return caps.PrivacyNotDetermined
	case clRestricted:
		return caps.PrivacyRestricted
	case clDenied:
		return caps.PrivacyDenied
	case clAuthorizedAlways, clAuthorizedWhenInUse:
		return caps.PrivacyGranted
	}
	return caps.PrivacyUnknown
}

// privacyFromScreen maps CGPreflightScreenCaptureAccess. The system does not
// say whether the user ever answered, so a refusal is "notDetermined": asking
// is harmless (it shows the prompt once, then the call is silent).
func privacyFromScreen(granted bool) caps.PrivacyStatus {
	if granted {
		return caps.PrivacyGranted
	}
	return caps.PrivacyNotDetermined
}

// A window that CGWindowListCopyWindowInfo lists is capturable when it is a
// normal application window: layer 0, not tiny, and named or owned.
type cgWindow struct {
	number        int
	layer         int
	name, owner   string
	width, height float64
}

// windowSources turns CGWindowListCopyWindowInfo rows into capture sources:
// layer 0 windows at least 40 points on a side that have a title or an owner,
// untitled ones named after their app.
func windowSources(rows []cgWindow) []caps.CaptureSource {
	var out []caps.CaptureSource
	for _, w := range rows {
		if w.layer != 0 || w.width < 40 || w.height < 40 {
			continue
		}
		name := w.name
		if name == "" {
			name = w.owner
		}
		if name == "" {
			continue
		}
		out = append(out, caps.CaptureSource{
			ID: "window:" + strconv.Itoa(w.number), Kind: "window", Name: name, App: w.owner,
			Width: int(w.width), Height: int(w.height),
		})
	}
	return out
}

// screenName names the n-th display (1-based).
func screenName(n int) string { return "Display " + strconv.Itoa(n) }
