//go:build windows

package windows

import (
	"runtime"

	xw "golang.org/x/sys/windows"

	"github.com/kratejs/krate/packages/native/platform/probe"
)

// Probe reports the installed WebView2 Runtime version from the EdgeUpdate
// registration, the same place the loader reads.
func Probe() probe.Info {
	info := probe.Info{OS: "windows", Arch: runtime.GOARCH}
	if v := xw.RtlGetVersion(); v != nil {
		info.OSVersion = probe.Version(v.MajorVersion, v.MinorVersion, v.BuildNumber)
	}
	wv := probe.Webview{Name: "WebView2"}
	if rts := findRuntimes(); len(rts) > 0 {
		wv.Present, wv.OK, wv.Version = true, true, rts[0].pv
		if rts[0].location != "" {
			wv.Libraries = []string{rts[0].location}
		}
	} else {
		wv.Error = "the WebView2 Runtime is not installed"
		wv.Hint = "install the Evergreen runtime from " + bootstrapperURL + " (it ships with Windows 11 and current Microsoft Edge), or ship WebView2Loader.dll next to the executable"
	}
	info.Webview = wv
	info.Display = probe.Display{Available: true, Detail: "desktop session (not probed)"}
	return info
}
