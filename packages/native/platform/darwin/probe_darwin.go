//go:build darwin

package darwin

import (
	"runtime"
	"syscall"

	"github.com/kratejs/krate/packages/native/platform/probe"
)

// minOSVersion is the oldest macOS the shell is tested against (see
// MANUAL_TEST.md).
const minOSVersion = "12.0"

// Probe reports the macOS version. WKWebView is part of the system, so the
// webview is present whenever the OS is new enough. A window needs a GUI
// login session, which cannot be told from here.
func Probe() probe.Info {
	v, _ := syscall.Sysctl("kern.osproductversion")
	info := probe.Info{OS: "darwin", Arch: runtime.GOARCH, OSVersion: v}
	wv := probe.Webview{Name: "WKWebView", Present: true, Version: v, MinVersion: minOSVersion, OK: true}
	if v != "" && !probe.AtLeast(v, minOSVersion) {
		wv.OK = false
		wv.Error = "macOS " + v + " is older than " + minOSVersion
		wv.Hint = "update macOS"
	}
	info.Webview = wv
	info.Display = probe.Display{Available: true, Detail: "window server (not probed; needs a logged-in GUI session)"}
	return info
}
