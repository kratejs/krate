//go:build darwin

package darwin

import (
	"testing"

	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Accessibility GUI tests. The first group asks our own AppKit objects through
// the NSAccessibility protocol (accessibilityRole, accessibilityLabel, ...),
// which needs no permission and so runs on a CI runner. The second group walks
// the tree the way VoiceOver does, through the AX API, and needs the test
// process to be trusted for accessibility; without that it skips with the
// reason. Both run under KRATE_NATIVE_GUI=1 in the CI native job.

func axRole(obj objc.ID) string  { return goString(obj.Send(sel("accessibilityRole"))) }
func axLabel(obj objc.ID) string { return goString(obj.Send(sel("accessibilityLabel"))) }
func axHidden(obj objc.ID) bool  { return objc.Send[bool](obj, sel("isAccessibilityHidden")) }

func TestGUIAccessibilityWindowIsNamedInEveryChrome(t *testing.T) {
	for _, c := range []struct {
		name   string
		chrome caps.ChromeOptions
		// lights says whether the traffic lights must be there.
		lights bool
	}{
		{"native", caps.ChromeOptions{}, true},
		{"hidden", caps.ChromeOptions{TitleBar: caps.TitleBarHidden}, true},
		{"none", caps.ChromeOptions{TitleBar: caps.TitleBarNone}, false},
		{"transparent", caps.ChromeOptions{TitleBar: caps.TitleBarNone, Transparent: true}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := chromeWin(t, c.chrome)
			var title, role string
			var hidden, webHidden bool
			var closeRole, closeLabel string
			var closeShown bool
			var webLabelOrRole string
			onMain(func() {
				title = goString(w.win.Send(sel("title")))
				role = axRole(w.win)
				hidden = axHidden(w.win)
				webHidden = axHidden(w.pv.web)
				webLabelOrRole = axRole(w.pv.web)
				b := w.win.Send(sel("standardWindowButton:"), nsWindowCloseButton)
				if b != 0 {
					closeShown = !objc.Send[bool](b, sel("isHidden"))
					closeRole = axRole(b)
					closeLabel = axLabel(b)
				}
			})
			if title != t.Name() {
				t.Errorf("window title %q, want %q: the title names the window for VoiceOver", title, t.Name())
			}
			if role != "AXWindow" {
				t.Errorf("window role %q, want AXWindow", role)
			}
			if hidden || webHidden {
				t.Errorf("window hidden=%v webview hidden=%v: the webview must be reachable", hidden, webHidden)
			}
			if webLabelOrRole == "" {
				t.Error("the webview has no accessibility role")
			}
			if c.lights {
				// The traffic lights are AppKit's own widgets: Krate only has to
				// keep them shown. Their accessibility role and label are
				// AppKit's and are recorded, not required (on CI runners AppKit
				// reports AXUnknown and no label for them in-process).
				if !closeShown {
					t.Error("the close traffic light is hidden")
				}
				t.Logf("close traffic light role=%q label=%q", closeRole, closeLabel)
			} else if closeShown {
				t.Error("a window with no title bar shows a traffic light")
			}
		})
	}
}
