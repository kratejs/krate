package platform

import "github.com/kratejs/krate/packages/native/platform/probe"

// Probe reports the state of the system webview and display on this machine
// without opening a window.
func Probe() probe.Info { return probeOS() }
