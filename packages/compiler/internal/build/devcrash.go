package build

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// recentLogCap is how many console lines the dev server remembers.
	recentLogCap = 200
	// crashLogLines is how many of them an error in the overlay carries.
	crashLogLines = 40
	// crashRepeatWindow suppresses an identical report, so a failing
	// middleware does not flood the overlay with one entry per request.
	crashRepeatWindow = 3 * time.Second
)

// recentLog is a ring of the latest console lines.
var recentLog struct {
	sync.Mutex
	lines []string
}

// recordRecentLine remembers one console line for crash reports. It is a sink
// of the dev log tee.
func recordRecentLine(line []byte) {
	recentLog.Lock()
	defer recentLog.Unlock()
	recentLog.lines = append(recentLog.lines, string(line))
	if over := len(recentLog.lines) - recentLogCap; over > 0 {
		recentLog.lines = append([]string(nil), recentLog.lines[over:]...)
	}
}

// RecentLogLines returns up to n of the latest console lines, oldest first.
func RecentLogLines(n int) []string {
	recentLog.Lock()
	defer recentLog.Unlock()
	lines := recentLog.lines
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return append([]string(nil), lines...)
}

// devCrashHub is the running dev server's hub; crash reports go to its tabs.
// It is nil outside `krate dev`, which makes the report functions no-ops.
var devCrashHub atomic.Pointer[DevHub]

// devLogLocation names where the full output is kept, for the report footer.
var devLogLocation atomic.Value // string

// setDevCrashHub makes hub the target of crash reports and returns the
// function that detaches it.
func setDevCrashHub(hub *DevHub, root string) func() {
	if hub == nil {
		return func() {}
	}
	devCrashHub.Store(hub)
	loc := filepath.ToSlash(filepath.Join(".krate", "dev.log"))
	if p := currentRunLogPath(); p != "" {
		if rel, err := filepath.Rel(root, p); err == nil {
			loc = filepath.ToSlash(rel)
		}
	}
	devLogLocation.Store(loc)
	return func() { devCrashHub.CompareAndSwap(hub, nil) }
}

// reportServerCrash shows a failed server-side part (a sidecar, the
// middleware) in the dev overlay, with the stack when there is one and the
// last console lines, and records it for `krate dev` state readers. It stays
// on screen for tabs that connect later until clearServerCrash(source).
func reportServerCrash(source, detail, stack string) {
	hub := devCrashHub.Load()
	if hub == nil {
		return
	}
	publish := func() {
		hub.PublishServerError(source, buildCrashError(source, detail, stack, RecentLogLines(crashLogLines)))
	}
	if crashTailDelay <= 0 {
		publish()
		return
	}
	// The line that announced the failure is still on its way through the
	// log pipe; wait a moment so the report ends with it.
	pendingCrash.Lock()
	defer pendingCrash.Unlock()
	if t := pendingCrash.timers[source]; t != nil {
		t.Stop()
	}
	if pendingCrash.timers == nil {
		pendingCrash.timers = map[string]*time.Timer{}
	}
	pendingCrash.timers[source] = time.AfterFunc(crashTailDelay, publish)
}

// pendingCrash holds the reports still waiting for the console pipe, so a
// source that recovers in the meantime is not reported after the fact.
var pendingCrash struct {
	sync.Mutex
	timers map[string]*time.Timer
}

// crashTailDelay is how long a report waits for the console pipe to drain.
var crashTailDelay = 150 * time.Millisecond

// clearServerCrash forgets the crash a source reported, once it recovered.
func clearServerCrash(source string) {
	pendingCrash.Lock()
	if t := pendingCrash.timers[source]; t != nil {
		t.Stop()
		delete(pendingCrash.timers, source)
	}
	pendingCrash.Unlock()
	if hub := devCrashHub.Load(); hub != nil {
		hub.ClearServerError(source)
	}
}

// buildCrashError assembles the overlay entry: the message names what failed,
// the stack holds the error's own stack and the last console lines.
func buildCrashError(source, detail, stack string, tail []string) ClientError {
	msg := source + " failed"
	if detail = strings.TrimSpace(detail); detail != "" {
		msg = fmt.Sprintf("%s failed: %s", source, detail)
	}
	var b strings.Builder
	if stack = strings.TrimSpace(stack); stack != "" {
		b.WriteString(stack)
		b.WriteString("\n\n")
	}
	if len(tail) > 0 {
		fmt.Fprintf(&b, "Last %d console line(s):\n", len(tail))
		for _, l := range tail {
			b.WriteString("  " + l + "\n")
		}
	}
	if loc, _ := devLogLocation.Load().(string); loc != "" {
		fmt.Fprintf(&b, "\nFull output: %s", loc)
	}
	return ClientError{Message: msg, Stack: strings.TrimRight(b.String(), "\n"), Kind: "server"}
}
