package build

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// maxStderrTail bounds how much of a build-time script's stderr is kept for
// diagnostics.
const maxStderrTail = 8 << 10

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	max   int
	buf   []byte
	total int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.total += len(p)
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-t.max:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	s := strings.TrimSpace(string(t.buf))
	if t.total > len(t.buf) {
		s = "(earlier output trimmed) " + s
	}
	return s
}

// scriptRun is the outcome of running a build-time Node/Bun/Deno script.
type scriptRun struct {
	Stdout []byte
	Stderr string
}

// describe summarizes a run for a diagnostic: exit status, stdout size and the
// stderr tail.
func (r scriptRun) describe(status string) string {
	var sb strings.Builder
	sb.WriteString(status)
	if len(r.Stdout) == 0 {
		sb.WriteString(", stdout was empty")
	} else {
		fmt.Fprintf(&sb, ", stdout was %d bytes", len(r.Stdout))
	}
	if r.Stderr != "" {
		sb.WriteString("\nstderr:\n" + r.Stderr)
	} else {
		sb.WriteString(", stderr was empty")
	}
	return sb.String()
}

// scriptFailure is a non-zero exit of a build-time script; it unwraps to the
// underlying *exec.ExitError.
type scriptFailure struct {
	msg string
	err error
}

func (e *scriptFailure) Error() string { return e.msg }
func (e *scriptFailure) Unwrap() error { return e.err }

// runBuildScript runs cmd and captures stdout in full and the tail of stderr.
// A failed run returns the captured output together with an error that names
// the exit status and includes the stderr tail.
func runBuildScript(ctx context.Context, cmd *exec.Cmd, timeoutMsg string) (scriptRun, error) {
	var out bytes.Buffer
	errTail := &tailBuffer{max: maxStderrTail}
	cmd.Stdout = &out
	cmd.Stderr = errTail
	err := cmd.Run()
	run := scriptRun{Stdout: out.Bytes(), Stderr: errTail.String()}
	if err == nil {
		return run, nil
	}
	if ctx.Err() == context.DeadlineExceeded {
		return run, errors.New(run.describe(timeoutMsg))
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return run, &scriptFailure{msg: "execution failed:\n" + run.describe("exit status "+fmt.Sprint(exitErr.ExitCode())), err: err}
	}
	return run, err
}
