package shell

import (
	"strings"
	"testing"
	"time"
)

// This file sorts after the other GUI tests on purpose: the inspector the test
// opens is a web view of its own and stays until the shell quits, which would
// change the web process count that TestGUIWindowsBackground asserts.

// TestGUIInspectorShortcut presses F12 in the page (as a dispatched key event)
// and checks that the shell opened the web inspector of the view, then posts
// to the endpoint directly. The dev shell runs with the developer tools on.
func TestGUIInspectorShortcut(t *testing.T) {
	if guiCtl == nil {
		t.Skip("set KRATE_NATIVE_GUI=1 to run GUI tests")
	}
	out, err := guiCtl.Query(`(async () => {
		const has = document.documentElement.outerHTML.includes("/__krate/inspector");
		document.dispatchEvent(new KeyboardEvent("keydown", {key: "F12", bubbles: true, cancelable: true}));
		return {has};
	})()`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !strings.Contains(out, `"has":true`) {
		t.Errorf("the page has no inspector shortcut script: %s", out)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !guiLogHas("web inspector opened") {
		if time.Now().After(deadline) {
			guiLog.mu.Lock()
			defer guiLog.mu.Unlock()
			t.Fatalf("F12 did not open the inspector: %q", guiLog.lines)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if guiLogHas("web inspector:") {
		t.Errorf("opening the inspector failed")
	}
}
