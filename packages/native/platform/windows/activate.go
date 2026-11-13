package windows

import (
	"unicode/utf16"

	"github.com/kratejs/krate/packages/native/toastscheme"
)

// Toast activation after the app quit, the pure parts. A toast click runs
// the app's registered protocol handler (see package toastscheme). The new
// process looks for a running instance by the name of its hidden utility
// window and hands the click over in a WM_COPYDATA message; when there is no
// instance it keeps the click and delivers it as a notification event once
// the page can receive it (caps.NotifyEvent.Launch).

// sinkWindowName is the title of the hidden utility window of an app.
func sinkWindowName(scheme string) string { return "KrateToastSink:" + scheme }

// encodeActivation returns the UTF-16 payload (with a terminating NUL) of an
// activation URL.
func encodeActivation(uri string) []uint16 {
	return append(utf16.Encode([]rune(uri)), 0)
}

// decodeActivation reverses encodeActivation for the given scheme. ok is false
// for a payload that is not an activation URL of this app.
func decodeActivation(scheme string, units []uint16) (id, action string, ok bool) {
	for len(units) > 0 && units[len(units)-1] == 0 {
		units = units[:len(units)-1]
	}
	if len(units) == 0 || len(units) > 8192 {
		return "", "", false
	}
	return toastscheme.ParseURI(scheme, string(utf16.Decode(units)))
}
