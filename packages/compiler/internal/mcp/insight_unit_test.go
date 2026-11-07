package mcp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/build"
)

func TestPaginateBoundsAndCursors(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	page, next, rerr := paginate(items, map[string]any{"limit": float64(2)}, 10, 100)
	if rerr != nil || len(page) != 2 || next == "" {
		t.Fatalf("page 1 = %v %q %v", page, next, rerr)
	}
	page, next, _ = paginate(items, map[string]any{"limit": float64(2), "cursor": next}, 10, 100)
	if page[0] != 3 || next == "" {
		t.Fatalf("page 2 = %v %q", page, next)
	}
	page, next, _ = paginate(items, map[string]any{"limit": float64(2), "cursor": next}, 10, 100)
	if len(page) != 1 || next != "" {
		t.Fatalf("last page = %v %q", page, next)
	}
	// A limit above the maximum is clamped; a cursor past the end is empty.
	page, _, _ = paginate(items, map[string]any{"limit": float64(9999)}, 10, 3)
	if len(page) != 3 {
		t.Errorf("max clamp = %v", page)
	}
	page, next, _ = paginate(items, map[string]any{"cursor": encodeCursor(99)}, 10, 100)
	if len(page) != 0 || next != "" || page == nil {
		t.Errorf("past the end = %v %q", page, next)
	}
	if _, _, rerr := paginate(items, map[string]any{"cursor": "***"}, 10, 100); rerr == nil || rerr.Code != codeInvalidParams {
		t.Errorf("invalid cursor = %v", rerr)
	}
	if page, _, _ := paginate([]int(nil), nil, 10, 100); page == nil || len(page) != 0 {
		t.Errorf("empty input must give an empty array, got %#v", page)
	}
}

func TestTruncateTextKeepsRunesWhole(t *testing.T) {
	if got, cut := truncateText("héllo", 2); got != "h" || !cut {
		t.Errorf("truncateText = %q %v", got, cut)
	}
	if got, cut := truncateText("abc", 10); got != "abc" || cut {
		t.Errorf("short text = %q %v", got, cut)
	}
	if got, cut := truncateText("abc", 0); got != "abc" || cut {
		t.Errorf("no limit = %q %v", got, cut)
	}
}

func TestScrubRootRemovesTheMachinePath(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }"})
	in := "error in " + svc.root + "/src/pages/a.tsx and root " + svc.root + " done"
	got := svc.scrubRoot(in)
	if strings.Contains(got, svc.root) || !strings.Contains(got, "src/pages/a.tsx") || !strings.Contains(got, "root . done") {
		t.Errorf("scrubRoot = %q", got)
	}
}

func TestLeadingDirective(t *testing.T) {
	for src, want := range map[string]string{
		"// @server\nexport default 1":                "server",
		"\n\n// note\n// @runtime\nexport default 1":  "runtime",
		"'use client';\n// @static\nexport default 1": "static",
		"/* header */\n// @client\nexport default 1":  "client",
		"import x from 'y';\n// @server\n":            "",
		"export default 1\n// @server\n":              "",
		"// plain comment\nexport default 1":          "",
	} {
		if got := leadingDirective(src); got != want {
			t.Errorf("leadingDirective(%q) = %q, want %q", src, got, want)
		}
	}
}

func TestShortReasonsPutsBrowserAPIHintsLast(t *testing.T) {
	rs := []hydrationReason{
		{Kind: "browser-api", Name: "window", Line: 1},
		{Kind: "hook", Name: "useState", Line: 5},
		{Kind: "event-handler", Name: "onClick", Line: 9},
		{Kind: "browser-api", Name: "document", Line: 12},
	}
	got := shortReasons(rs, 3)
	if len(got) != 3 || got[0].Name != "useState" || got[1].Name != "onClick" || got[2].Kind != "browser-api" {
		t.Errorf("shortReasons = %+v", got)
	}
}

func TestReadProjectTextAppliesTheReadPolicy(t *testing.T) {
	svc := newTestService(t, map[string]string{
		".env":        "SECRET=1\n",
		"src/a.txt":   "hello world",
		"notes/b.txt": "denied",
	})
	writePolicy(t, svc, `{"deny":["notes/**"]}`)
	if _, err := svc.readProjectText(".env", 0); err == nil {
		t.Error(".env must not be readable")
	}
	if _, err := svc.readProjectText("notes/b.txt", 0); err == nil {
		t.Error("denied path must not be readable")
	}
	if _, err := svc.readProjectText("../outside", 0); err == nil {
		t.Error("traversal must fail")
	}
	got, err := svc.readProjectText("src/a.txt", 5)
	if err != nil || got != "hello" {
		t.Errorf("limited read = %q %v", got, err)
	}
}

func TestTailBufferKeepsTheEnd(t *testing.T) {
	tb := &tailBuffer{}
	_, _ = tb.Write([]byte(strings.Repeat("a", tailMax)))
	_, _ = tb.Write([]byte("\x1b[31mTHE END\x1b[0m"))
	got := tb.String()
	if len(got) > tailMax || !strings.HasSuffix(got, "THE END") || strings.Contains(got, "\x1b") {
		t.Errorf("tail = %d bytes, suffix %q", len(got), got[len(got)-10:])
	}
}

func TestTailDevLogHandlesLargeFilesAndUntimestampedLines(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }"})
	now := time.Now()
	var b strings.Builder
	for i := 0; i < 30000; i++ {
		b.WriteString(devLogLine(now, "filler line number "+strings.Repeat("x", 30)))
	}
	b.WriteString("legacy line without a timestamp\n")
	b.WriteString(devLogLine(now, "the last line"))
	writeFile(t, svc.root, ".krate/dev.log", b.String())
	if info, _ := os.Stat(svc.root + "/.krate/dev.log"); info.Size() < 1<<20 {
		t.Fatalf("fixture too small: %d", info.Size())
	}
	lines, err := svc.tailDevLog(3)
	if err != nil || len(lines) != 3 {
		t.Fatalf("tail = %v %v", lines, err)
	}
	if lines[2].Text != "the last line" || lines[2].Time.IsZero() || lines[1].Text != "legacy line without a timestamp" || !lines[1].Time.IsZero() {
		t.Errorf("tail = %+v", lines)
	}
}

func TestDevServerStatusReportsAnOlderServerWithoutState(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }"})
	mux := http.NewServeMux()
	mux.HandleFunc("/__krate/status", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	ts := httptest.NewServer(mux)
	defer ts.Close()
	writeDevRecord(t, svc, ts.Listener.Addr().String(), os.Getpid())
	st := mustOK(t, svc, "dev_server_status", nil)
	if st["running"] != true || !strings.Contains(st["stateError"].(string), "older krate version") {
		t.Errorf("status = %v", st)
	}
	if msg := mustFail(t, svc, "get_errors", nil); !strings.Contains(msg, "state is unavailable") {
		t.Errorf("get_errors = %s", msg)
	}
}

func TestTeeDevLogTruncatesWhenItGrowsTooLarge(t *testing.T) {
	root := t.TempDir()
	origOut, origErr := os.Stdout, os.Stderr
	stop := build.TeeDevLog(root)
	line := strings.Repeat("y", 1000) + "\n"
	for i := 0; i < 4300; i++ {
		_, _ = os.Stdout.WriteString(line)
	}
	_, _ = os.Stdout.WriteString("after the truncation\n")
	stop()
	os.Stdout, os.Stderr = origOut, origErr
	data, err := os.ReadFile(build.DevLogPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 5<<20 || !strings.Contains(string(data), "[log truncated]") || !strings.HasSuffix(strings.TrimSpace(string(data)), "after the truncation") {
		t.Errorf("log is %d bytes; truncated=%v", len(data), strings.Contains(string(data), "[log truncated]"))
	}
}

func TestCompatCheckReportsThePreactVersionForReactPackages(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx":                  "export default function P() { return <h1>x</h1>; }",
		"node_modules/uses-react/package.json": `{"name":"uses-react","version":"1.0.0","dependencies":{"react":"^18"}}`,
		"node_modules/preact/package.json":     `{"name":"preact","version":"10.24.0"}`,
	})
	m := mustOK(t, svc, "compat_check", map[string]any{"package": "uses-react"})
	if m["verdict"] != "island" || m["preact"] != "10.24.0" {
		t.Errorf("compat = %v", m)
	}
	if !contains(strs(list(t, m, "evidence")), "CommonJS-only entry: usable, but not tree-shakeable") {
		t.Errorf("evidence = %v", m["evidence"])
	}
}
