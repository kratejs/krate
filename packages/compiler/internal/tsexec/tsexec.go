// Package tsexec runs TypeScript helper scripts (config bootstraps,
// generateStaticParams, etc.) via `npx tsx`. It centralizes the temp-bootstrap
// write, file:// URL conversion, subprocess execution, timeout handling, and
// stderr capture that the compiler uses to evaluate user TS at build time.
package tsexec

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// ImportPath converts an absolute filesystem path to a file:// URL suitable for
// ESM imports on all platforms (required for Windows).
func ImportPath(abs string) string {
	p := filepath.ToSlash(abs)
	if p[0] != '/' {
		p = "file:///" + p
	}
	return p
}

// RunBootstrap writes `content` to a uniquely-named temporary .mjs file and
// executes it with `npx tsx` from the given working directory. It returns the
// script's stdout and stderr separately, plus a descriptive error (including
// stderr and timeout detection) when execution fails.
func RunBootstrap(name, content, cwd string, timeout time.Duration) (stdout []byte, stderr string, err error) {
	buf := make([]byte, 8)
	rand.Read(buf)
	suffix := hex.EncodeToString(buf)

	path := filepath.Join(os.TempDir(), name+"-"+suffix+".mjs")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return nil, "", fmt.Errorf("writing bootstrap: %w", err)
	}
	defer os.Remove(path)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "npx", "tsx", path)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, "", fmt.Errorf("execution timed out after %v", timeout)
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, string(exitErr.Stderr), fmt.Errorf("execution failed:\n%s", string(exitErr.Stderr))
		}
		return nil, "", fmt.Errorf("execution: %w", err)
	}
	return out, "", nil
}
