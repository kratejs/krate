// Package probe holds the result type of the platform webview probe, shared
// by the per-OS platform packages and krate-shell --doctor.
package probe

import (
	"fmt"
	"strconv"
	"strings"
)

// Webview describes the system webview the shell needs.
type Webview struct {
	// Name is "WebKitGTK 4.1", "WKWebView" or "WebView2".
	Name string `json:"name"`
	// Present is true when the webview library or runtime was found.
	Present bool `json:"present"`
	// Version is the library or runtime version, "" when unknown.
	Version string `json:"version,omitempty"`
	// MinVersion is the oldest version the shell supports, "" for none.
	MinVersion string `json:"minVersion,omitempty"`
	// OK is true when Present and not older than MinVersion.
	OK bool `json:"ok"`
	// Libraries are the shared library files that were loaded.
	Libraries []string `json:"libraries,omitempty"`
	// Error says why the webview is unusable.
	Error string `json:"error,omitempty"`
	// Hint says how to fix Error.
	Hint string `json:"hint,omitempty"`
}

// Display describes whether a window can be opened.
type Display struct {
	Available bool   `json:"available"`
	Detail    string `json:"detail,omitempty"`
}

// Info is the full probe result.
type Info struct {
	OS        string  `json:"os"`
	Arch      string  `json:"arch"`
	OSVersion string  `json:"osVersion,omitempty"`
	Webview   Webview `json:"webview"`
	Display   Display `json:"display"`
}

// AtLeast reports whether the dotted version v is not older than min. Missing
// or non-numeric parts count as zero.
func AtLeast(v, min string) bool {
	a, b := parts(v), parts(min)
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x > y
		}
	}
	return true
}

func parts(v string) []int {
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}

// Version formats major.minor.micro.
func Version(major, minor, micro uint32) string {
	return fmt.Sprintf("%d.%d.%d", major, minor, micro)
}
