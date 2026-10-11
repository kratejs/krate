// Package execx runs external processes with a hard timeout and reliable
// process-tree termination. A hung `go build`, `npm install`, or `git` can no
// longer wedge a Krate build: the deadline is enforced and the whole tree is
// killed (on Windows `npx`/`go` are shims that spawn children, so killing only
// the direct child would leak the real work and block on its pipes).
package execx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Timeout resolves an operation-specific timeout from envVar. Unset -> def;
// "0"/"off"/"none" -> 0 (disabled); a valid Go duration -> that value. This lets
// a slow machine raise one operation's budget without loosening the others.
func Timeout(envVar string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(envVar))
	if v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "0", "off", "none":
		return 0
	}
	if d, err := time.ParseDuration(v); err == nil && d >= 0 {
		return d
	}
	return def
}

// DefaultTimeout returns the process-wide default timeout from the
// KRATE_EXEC_TIMEOUT environment variable (a Go duration, e.g. "90s"), or 0
// when unset/invalid (0 means "no default" - callers may still pass an explicit
// timeout).
func DefaultTimeout() time.Duration {
	v := os.Getenv("KRATE_EXEC_TIMEOUT")
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0
	}
	return d
}

// Command builds an *exec.Cmd wired for tree termination: it is configured to
// start a new process group (Unix), and Cancel kills the whole tree.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	configureTree(cmd)
	cmd.Cancel = func() error {
		KillTree(cmd)
		return nil
	}
	cmd.WaitDelay = 10 * time.Second
	return cmd
}

// ConfigureTree prepares a manually-built *exec.Cmd (e.g. a long-lived sidecar
// started with Start rather than Command) so KillTree can terminate its whole
// process group on Unix.
func ConfigureTree(cmd *exec.Cmd) { configureTree(cmd) }

// Run executes name/args, enforcing timeout (falling back to DefaultTimeout when
// timeout <= 0). dir is the working directory (empty = inherit); env entries are
// appended to the current environment. It returns stdout, stderr, and an error
// that is descriptive on timeout.
func Run(ctx context.Context, timeout time.Duration, dir string, env []string, name string, args ...string) (stdout, stderr []byte, err error) {
	if timeout <= 0 {
		timeout = DefaultTimeout()
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := Command(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	runErr := cmd.Run()
	if runErr != nil && timeout > 0 && ctx.Err() == context.DeadlineExceeded {
		return outBuf.Bytes(), errBuf.Bytes(), fmt.Errorf("%s timed out after %v", name, timeout)
	}
	return outBuf.Bytes(), errBuf.Bytes(), runErr
}

// Output is Run for the common case of needing only stdout.
func Output(ctx context.Context, timeout time.Duration, dir string, env []string, name string, args ...string) ([]byte, error) {
	stdout, stderr, err := Run(ctx, timeout, dir, env, name, args...)
	if err != nil {
		if len(stderr) > 0 {
			return stdout, fmt.Errorf("%w: %s", err, bytes.TrimSpace(stderr))
		}
		return stdout, err
	}
	return stdout, nil
}
