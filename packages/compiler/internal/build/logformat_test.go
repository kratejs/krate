package build

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestColorStatusClasses(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{200, cGreen},
		{301, cCyan},
		{404, cYellow},
		{500, cRed},
	}
	for _, tc := range cases {
		got := colorStatus(tc.code)
		if !strings.Contains(got, tc.want) {
			t.Errorf("colorStatus(%d) = %q, want color %q", tc.code, got, tc.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	if got := formatDuration(250 * time.Microsecond); got != "250μs" {
		t.Errorf("sub-ms = %q, want 250μs", got)
	}
	if got := formatDuration(7 * time.Millisecond); got != "7ms" {
		t.Errorf("ms = %q, want 7ms", got)
	}
	if got := formatDuration(1500 * time.Millisecond); got != "1500ms" {
		t.Errorf("ms = %q, want 1500ms", got)
	}
}

func TestFormatRequestLinePlain(t *testing.T) {
	line := formatRequestLine("GET", "/server-runtime-demo/", 200, 7*time.Millisecond, false)
	if strings.Contains(line, "\033[") {
		t.Errorf("plain line should not contain ANSI codes: %q", line)
	}
	if !strings.Contains(line, "GET /server-runtime-demo/ 200 7ms") {
		t.Errorf("plain line missing fields: %q", line)
	}
}

func TestFormatRequestLineColor(t *testing.T) {
	line := formatRequestLine("POST", "/api/users", 500, 800*time.Microsecond, true)
	if !strings.Contains(line, cRed) {
		t.Errorf("expected red status for 500: %q", line)
	}
	if !strings.Contains(line, "800μs") {
		t.Errorf("expected microsecond duration: %q", line)
	}
}

func TestColorEnabledOptOuts(t *testing.T) {
	t.Setenv("KRATE_NO_COLOR", "1")
	if colorEnabled() {
		t.Error("expected color disabled with KRATE_NO_COLOR")
	}
	t.Setenv("KRATE_NO_COLOR", "")
	t.Setenv("NO_COLOR", "1")
	if colorEnabled() {
		t.Error("expected color disabled with NO_COLOR")
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if colorEnabled() {
		t.Error("expected color disabled with TERM=dumb")
	}
}

func TestPrettyHandlerRendersRequestLine(t *testing.T) {
	var buf bytes.Buffer
	h := newPrettyHandler(&buf, slog.LevelInfo, false)
	logger := slog.New(h)
	logger.Info(requestLogMsg,
		"id", "req-1",
		"method", "GET",
		"path", "/docs/",
		"status", 200,
		"duration", 3*time.Millisecond,
	)

	out := buf.String()
	if !strings.Contains(out, "GET /docs/ 200 3ms") {
		t.Errorf("request line not rendered compactly: %q", out)
	}
	if strings.Contains(out, "msg=") || strings.Contains(out, "level=") {
		t.Errorf("request line should not use structured slog format: %q", out)
	}
}

func TestPrettyHandlerStructuredFallback(t *testing.T) {
	var buf bytes.Buffer
	h := newPrettyHandler(&buf, slog.LevelInfo, false)
	logger := slog.New(h)
	logger.Error("panic recovered", "id", "req-2", "panic", "boom")

	out := buf.String()
	if !strings.Contains(out, "panic recovered") || !strings.Contains(out, "req-2") {
		t.Errorf("non-request records should stay structured: %q", out)
	}
}

func TestPrettyHandlerRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	h := newPrettyHandler(&buf, slog.LevelInfo, false)
	logger := slog.New(h)
	logger.Debug("quiet")
	if buf.Len() != 0 {
		t.Errorf("debug should be suppressed at info level: %q", buf.String())
	}
}
