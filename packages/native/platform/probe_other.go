//go:build !linux && !darwin && !windows

package platform

import (
	"runtime"

	"github.com/kratejs/krate/packages/native/platform/probe"
)

func probeOS() probe.Info {
	return probe.Info{
		OS: runtime.GOOS, Arch: runtime.GOARCH,
		Webview: probe.Webview{Name: "none", Error: "desktop windows are not supported on " + runtime.GOOS},
	}
}
