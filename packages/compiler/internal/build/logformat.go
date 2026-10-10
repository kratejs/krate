package build

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/mattn/go-isatty"
)

// request-line formatting

// colorStatus renders an HTTP status code with a severity colour: green for
// 2xx, cyan for 3xx, yellow for 4xx, red for 5xx.
func colorStatus(code int) string {
	switch {
	case code >= 200 && code < 300:
		return fmt.Sprintf("%s%d%s", cGreen, code, cReset)
	case code >= 300 && code < 400:
		return fmt.Sprintf("%s%d%s", cCyan, code, cReset)
	case code >= 400 && code < 500:
		return fmt.Sprintf("%s%d%s", cYellow, code, cReset)
	default:
		return fmt.Sprintf("%s%d%s", cRed, code, cReset)
	}
}

// formatDuration renders a request duration, preferring microseconds for
// sub-millisecond requests so fast static hits are not shown as "0ms".
func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return strconv.FormatInt(d.Microseconds(), 10) + "μs"
	}
	return strconv.FormatInt(d.Milliseconds(), 10) + "ms"
}

// formatRequestLine renders a single request log line. With color it mirrors
// the CLI's build output (dim timestamp, colored status, dim duration);
// otherwise it emits a clean, aligned plain-text line for CI and piped output.
func formatRequestLine(method, path string, status int, dur time.Duration, color bool) string {
	durStr := formatDuration(dur)
	if !color {
		return fmt.Sprintf("%s %s %s %d %s", time.Now().Format("15:04:05"), method, path, status, durStr)
	}
	return fmt.Sprintf("  %s %s %s %s %s%s%s",
		cGray+time.Now().Format("15:04:05")+cReset,
		method,
		path,
		colorStatus(status),
		cGray, durStr, cReset,
	)
}

// colorEnabled reports whether ANSI styling should be used: enabled only on a
// real terminal and disabled when the environment opts out (NO_COLOR,
// KRATE_NO_COLOR, or TERM=dumb).
func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("KRATE_NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return isTTY(os.Stdout)
}

// isTTY reports whether f refers to an interactive terminal (including Cygwin
// terminals on Windows).
func isTTY(f *os.File) bool {
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// pretty slog handler

// requestLogMsg is the record message used by the request-logging middleware.
// The handler special-cases it to render a compact request line.
const requestLogMsg = "request"

// prettyHandler is an slog.Handler that renders request records as a single
// styled line and delegates every other record to a structured text handler.
type prettyHandler struct {
	mu    *sync.Mutex
	w     io.Writer
	level slog.Leveler
	color bool
	text  slog.Handler
}

func newPrettyHandler(w io.Writer, level slog.Leveler, color bool) *prettyHandler {
	return &prettyHandler{
		mu:    &sync.Mutex{},
		w:     w,
		level: level,
		color: color,
		text:  slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}),
	}
}

func (h *prettyHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level.Level()
}

func (h *prettyHandler) Handle(ctx context.Context, r slog.Record) error {
	if !h.Enabled(ctx, r.Level) {
		return nil
	}
	if r.Message != requestLogMsg {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.text.Handle(ctx, r)
	}

	var method, path string
	var status int
	var dur time.Duration
	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "method":
			method = a.Value.String()
		case "path":
			path = a.Value.String()
		case "status":
			status = int(a.Value.Int64())
		case "duration":
			dur = a.Value.Duration()
		}
		return true
	})

	line := formatRequestLine(method, path, status, dur, h.color)
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, line+"\n")
	return err
}

func (h *prettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	n := *h
	n.text = h.text.WithAttrs(attrs)
	return &n
}

func (h *prettyHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	n := *h
	n.text = h.text.WithGroup(name)
	return &n
}

// newLogger builds the serving logger writing to stdout, matching the CLI's
// build output stream. Default level is Info; verbose raises it to Debug.
func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(newPrettyHandler(os.Stdout, level, colorEnabled()))
}
