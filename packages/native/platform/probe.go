package platform

import (
	"runtime"

	"github.com/kratejs/krate/packages/native/platform/probe"
)

// Probe reports the state of the system webview and display on this machine
// without opening a window.
func Probe() probe.Info { return probeOS() }

func unsupportedProbe() probe.Info {
	return probe.Info{
		OS: runtime.GOOS, Arch: runtime.GOARCH,
		Webview: probe.Webview{Name: "none", Error: "desktop windows are not supported on " + runtime.GOOS},
	}
}
