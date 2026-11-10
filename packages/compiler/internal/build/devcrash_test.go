package build

import (
	"strings"
	"testing"
	"time"
)

func init() { crashTailDelay = 0 }

func resetRecentLog() {
	recentLog.Lock()
	recentLog.lines = nil
	recentLog.Unlock()
}

func TestRecentLogKeepsTheLatestLines(t *testing.T) {
	resetRecentLog()
	t.Cleanup(resetRecentLog)
	for i := 0; i < recentLogCap+30; i++ {
		recordRecentLine([]byte("line " + string(rune('A'+i%26))))
	}
	if got := RecentLogLines(0); len(got) != recentLogCap {
		t.Fatalf("kept %d lines, want %d", len(got), recentLogCap)
	}
	last := RecentLogLines(3)
	if len(last) != 3 || last[2] != "line "+string(rune('A'+(recentLogCap+29)%26)) {
		t.Errorf("last lines = %v", last)
	}
}

func TestBuildCrashError(t *testing.T) {
	ce := buildCrashError("SSR renderer", "exit status 1", "Error: boom\n    at x", []string{"one", "two"})
	if ce.Kind != "server" || ce.Message != "SSR renderer failed: exit status 1" {
		t.Errorf("unexpected message/kind: %+v", ce)
	}
	for _, want := range []string{"Error: boom", "Last 2 console line(s):", "  two"} {
		if !strings.Contains(ce.Stack, want) {
			t.Errorf("stack misses %q:\n%s", want, ce.Stack)
		}
	}
	if bare := buildCrashError("Middleware", "", "", nil); bare.Message != "Middleware failed" {
		t.Errorf("bare report = %+v", bare)
	}
}

func TestReportServerCrashReachesTabsAndIsSticky(t *testing.T) {
	resetRecentLog()
	t.Cleanup(resetRecentLog)
	hub := NewDevHub()
	defer setDevCrashHub(hub, t.TempDir())()
	events, cancel := hub.Subscribe()
	defer cancel()

	recordRecentLine([]byte("renderer: out of memory"))
	reportServerCrash("SSR renderer", "exited unexpectedly", "")

	select {
	case ev := <-events:
		if ev.Type != "client-error" || ev.ClientError == nil || !strings.Contains(ev.ClientError.Message, "SSR renderer failed") {
			t.Fatalf("unexpected event %+v", ev)
		}
		if !strings.Contains(ev.ClientError.Stack, "renderer: out of memory") {
			t.Errorf("the overlay entry should carry the last console lines:\n%s", ev.ClientError.Stack)
		}
	case <-time.After(time.Second):
		t.Fatal("no event published")
	}
	if got := hub.ServerErrors(); len(got) != 1 {
		t.Fatalf("a late tab should see the crash, got %v", got)
	}
	snap := hub.Snapshot()
	if len(snap.ClientErrors) != 1 || snap.ClientErrors[0].Kind != "server" {
		t.Errorf("the dev state should list the crash as a server error: %+v", snap.ClientErrors)
	}

	clearServerCrash("SSR renderer")
	if got := hub.ServerErrors(); len(got) != 0 {
		t.Errorf("a recovered sidecar must not be replayed, got %v", got)
	}
}

func TestReportServerCrashIgnoresRepeats(t *testing.T) {
	hub := NewDevHub()
	defer setDevCrashHub(hub, t.TempDir())()
	events, cancel := hub.Subscribe()
	defer cancel()
	for i := 0; i < 5; i++ {
		reportServerCrash("Middleware", "GET /x: boom", "")
	}
	if n := len(events); n != 1 {
		t.Errorf("an error repeating every request published %d events, want 1", n)
	}
	reportServerCrash("Middleware", "GET /y: other", "")
	if n := len(events); n != 2 {
		t.Errorf("a different error must be published, have %d events", n)
	}
}

func TestReportServerCrashWithoutDevServerIsANoop(t *testing.T) {
	devCrashHub.Store(nil)
	reportServerCrash("SSR renderer", "exited", "") // must not panic
	clearServerCrash("SSR renderer")
}

func TestReportServerCrashWaitsForTheLogPipe(t *testing.T) {
	resetRecentLog()
	t.Cleanup(resetRecentLog)
	crashTailDelay = 20 * time.Millisecond
	t.Cleanup(func() { crashTailDelay = 0 })
	hub := NewDevHub()
	defer setDevCrashHub(hub, t.TempDir())()
	events, cancel := hub.Subscribe()
	defer cancel()

	reportServerCrash("SSR renderer", "exited", "")
	recordRecentLine([]byte("the line that arrived late"))
	select {
	case ev := <-events:
		if !strings.Contains(ev.ClientError.Stack, "the line that arrived late") {
			t.Errorf("report was taken before the pipe drained:\n%s", ev.ClientError.Stack)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event published")
	}
}

func TestClearServerCrashCancelsAPendingReport(t *testing.T) {
	crashTailDelay = 50 * time.Millisecond
	t.Cleanup(func() { crashTailDelay = 0 })
	hub := NewDevHub()
	defer setDevCrashHub(hub, t.TempDir())()
	reportServerCrash("SSR renderer", "exited", "")
	clearServerCrash("SSR renderer")
	time.Sleep(150 * time.Millisecond)
	if got := hub.ServerErrors(); len(got) != 0 {
		t.Errorf("a source that recovered before the report was sent must not be reported: %v", got)
	}
}
