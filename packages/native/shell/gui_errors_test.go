package shell

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// guiLog keeps what the shell logs, so a test can check what reached the
// terminal of `krate native dev`.
var guiLog struct {
	mu    sync.Mutex
	lines []string
}

func guiLogf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	guiLog.mu.Lock()
	guiLog.lines = append(guiLog.lines, line)
	guiLog.mu.Unlock()
	fmt.Fprintf(os.Stderr, "krate-shell: %s\n", line)
}

func guiLogHas(sub string) bool {
	guiLog.mu.Lock()
	defer guiLog.mu.Unlock()
	for _, l := range guiLog.lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// TestGUIPageErrorsReachTheLog posts an error from a real webview the way the
// dev overlay does and checks that the shell logs it tagged with the window,
// using the window it knows the view by and not the one the page claims.
func TestGUIPageErrorsReachTheLog(t *testing.T) {
	if guiCtl == nil {
		t.Skip("set KRATE_NATIVE_GUI=1 to run GUI tests")
	}
	if _, err := guiCtl.Query(`fetch("/__krate/dev/client-error", {method: "POST",
		headers: {"Content-Type": "application/json"},
		body: JSON.stringify({message: "gui-page-error", kind: "error", window: "spoofed",
			stack: "f@src/page.tsx:3:4", url: "krate://app/x.js", line: 3, col: 4})}).then(r => r.status)`); err != nil {
		t.Fatalf("posting the error from the page: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !guiLogHas("[webview] main: gui-page-error") {
		if time.Now().After(deadline) {
			guiLog.mu.Lock()
			defer guiLog.mu.Unlock()
			t.Fatalf("the error never reached the log (or carries another window id): %q", guiLog.lines)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !guiLogHas("[webview]     f@src/page.tsx:3:4") {
		t.Errorf("the stack is missing from the log")
	}
	if guiLogHas("spoofed") {
		t.Errorf("the page's own window id was believed")
	}
}
