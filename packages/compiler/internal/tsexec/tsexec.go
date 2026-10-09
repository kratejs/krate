// Package tsexec runs TypeScript helper scripts (config bootstraps,
// generateStaticParams, etc.) via `npx --yes tsx`. It centralizes the temp-bootstrap
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
	"strings"
	"sync"
	"time"
)

// resultCache memoizes identical bootstrap runs within a single build/session.
// Callers Invalidate() at the start of each build so results can never go stale
// across edits (a bootstrap may import user source that changed).
var (
	resultMu    sync.Mutex
	resultCache = map[string][]byte{}
)

// Invalidate clears the bootstrap result cache. Call once at the start of a
// build.
func Invalidate() {
	resultMu.Lock()
	resultCache = map[string][]byte{}
	resultMu.Unlock()
}

func bootstrapKey(name, content, cwd, tsconfig string, env []string) string {
	return name + "\x00" + content + "\x00" + cwd + "\x00" + tsconfig + "\x00" + strings.Join(env, "\x00")
}

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
// executes it with `npx --yes tsx` from the given working directory. The `--yes`
// flag is required for non-interactive environments (CI): plain `npx tsx` would
// otherwise prompt "Ok to proceed?" and fail when tsx is neither installed nor
// cached. env is an optional set of KEY=VALUE strings appended to the subprocess
// environment (may be nil). It returns the
// script's stdout and stderr separately, plus a descriptive error (including
// stderr and timeout detection) when execution fails.
func RunBootstrap(name, content, cwd string, timeout time.Duration, env []string) (stdout []byte, stderr string, err error) {
	return RunBootstrapOpts(name, content, cwd, timeout, env, "")
}

// RunBootstrapOpts is RunBootstrap with an optional tsconfig path passed to tsx
// via `--tsconfig`. This lets Krate-provided module aliases (e.g.
// `krate/content` -> a codegen'd module) resolve inside bootstraps that import
// user source, since tsx honors compilerOptions.paths.
func RunBootstrapOpts(name, content, cwd string, timeout time.Duration, env []string, tsconfig string) (stdout []byte, stderr string, err error) {
	key := bootstrapKey(name, content, cwd, tsconfig, env)
	resultMu.Lock()
	if out, ok := resultCache[key]; ok {
		resultMu.Unlock()
		return out, "", nil
	}
	resultMu.Unlock()

	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	suffix := hex.EncodeToString(buf)

	path := filepath.Join(os.TempDir(), name+"-"+suffix+".mjs")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return nil, "", fmt.Errorf("writing bootstrap: %w", err)
	}
	defer os.Remove(path)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	args := []string{"--yes", "tsx"}
	if tsconfig != "" {
		args = append(args, "--tsconfig", tsconfig)
	}
	args = append(args, path)
	cmd := exec.CommandContext(ctx, "npx", args...)
	cmd.Dir = cwd
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	configureProcessTree(cmd)
	// A timeout must not outlive itself. On Windows `npx` is a shim that spawns
	// a child `node`; killing only the shim leaves the child holding the
	// stdout/stderr pipes open, so cmd.Wait() would block forever even after the
	// deadline. Kill the whole tree and bound the post-kill I/O wait.
	cmd.Cancel = func() error {
		killProcessTree(cmd)
		return nil
	}
	cmd.WaitDelay = 10 * time.Second
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
	resultMu.Lock()
	resultCache[key] = out
	resultMu.Unlock()
	return out, "", nil
}
