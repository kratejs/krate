package build

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// output is the destination for human-readable build progress. The CLI uses
// stdout; long-lived callers such as the MCP server can redirect it to a
// buffer without swapping the process-wide os.Stdout.
type outputWriter struct {
	mu sync.Mutex
	w  io.Writer
}

var progressOutput = &outputWriter{w: os.Stdout}

// SetOutput redirects build/check progress. Callers must restore the previous
// writer when finished. It is safe to call while other goroutines print.
func SetOutput(w io.Writer) {
	progressOutput.mu.Lock()
	if w == nil {
		progressOutput.w = io.Discard
	} else {
		progressOutput.w = w
	}
	progressOutput.mu.Unlock()
}

func printf(format string, args ...any) {
	progressOutput.mu.Lock()
	_, _ = io.WriteString(progressOutput.w, sprintf(format, args...))
	progressOutput.mu.Unlock()
}

func sprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

// errOutput receives the warnings that serve's long-running goroutines print
// (sidecar supervision, readiness, the scriptc proxy). Tests redirect it with
// SetErrOutput; swapping os.Stderr instead races with those goroutines.
var errOutput = &outputWriter{w: os.Stderr}

// SetErrOutput redirects those warnings; nil discards them. Callers restore
// the previous writer when finished. It is safe while other goroutines print.
func SetErrOutput(w io.Writer) {
	errOutput.mu.Lock()
	if w == nil {
		errOutput.w = io.Discard
	} else {
		errOutput.w = w
	}
	errOutput.mu.Unlock()
}

func eprintf(format string, args ...any) {
	errOutput.mu.Lock()
	_, _ = io.WriteString(errOutput.w, sprintf(format, args...))
	errOutput.mu.Unlock()
}

// childStderr is where a supervised child's stderr goes: os.Stderr itself, so
// the child writes to the terminal directly, unless SetErrOutput redirected it.
func childStderr() io.Writer {
	errOutput.mu.Lock()
	defer errOutput.mu.Unlock()
	return errOutput.w
}

// stderr is where a build writes its warnings: os.Stderr unless SetStderr
// gave the build a writer of its own.
func (b *Builder) stderr() io.Writer {
	if b != nil && b.warnOut != nil {
		return b.warnOut
	}
	return os.Stderr
}

// SetStderr sends the warnings of this builder's builds to w instead of
// os.Stderr, so callers (tests, long-lived hosts) can collect them without
// swapping the process-wide os.Stderr. Writes are serialized.
func (b *Builder) SetStderr(w io.Writer) {
	if w == nil {
		b.warnOut = nil
		return
	}
	b.warnOut = &syncWriter{w: w}
}

type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *syncWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
