//go:build darwin

package darwin

import (
	"github.com/ebitengine/purego/objc"
)

// Accessibility (NSAccessibility). AppKit makes NSWindow, NSMenu, NSAlert,
// NSSplitView, the traffic lights and WKWebView accessible by itself; this
// file adds what AppKit cannot know: windows that are not content stay out of
// the accessibility hierarchy, and the status item gets a name.

// hideFromAccessibility removes a window and its content from the
// accessibility hierarchy (an offscreen window).
func hideFromAccessibility(win objc.ID) {
	if win == 0 {
		return
	}
	objs := []objc.ID{win}
	if cv := win.Send(sel("contentView")); cv != 0 {
		objs = append(objs, cv)
	}
	for _, o := range objs {
		if respondsTo(o, sel("setAccessibilityHidden:")) {
			o.Send(sel("setAccessibilityHidden:"), true)
		}
		if respondsTo(o, sel("setAccessibilityElement:")) {
			o.Send(sel("setAccessibilityElement:"), false)
		}
	}
}

// nameTrayButton gives the status item's button an accessibility label (an
// icon-only button is otherwise announced as just "button") and the tooltip
// as help text.
func nameTrayButton(button objc.ID, label, help string) {
	if button == 0 {
		return
	}
	if respondsTo(button, sel("setAccessibilityLabel:")) {
		button.Send(sel("setAccessibilityLabel:"), nsString(label))
	}
	if help != "" && help != label && respondsTo(button, sel("setAccessibilityHelp:")) {
		button.Send(sel("setAccessibilityHelp:"), nsString(help))
	}
}

// makeDefaultAlertButton makes button i the one Return presses. NSAlert makes
// the first button the default; this moves the key equivalent.
func makeDefaultAlertButton(alert objc.ID, i int) {
	btns := alert.Send(sel("buttons"))
	if btns == 0 {
		return
	}
	n := objc.Send[int](btns, sel("count"))
	if i <= 0 || i >= n {
		return
	}
	for j := 0; j < n; j++ {
		b := btns.Send(sel("objectAtIndex:"), j)
		if j == i {
			b.Send(sel("setKeyEquivalent:"), nsString("\r"))
		} else if j == 0 {
			b.Send(sel("setKeyEquivalent:"), nsString(""))
		}
	}
}
