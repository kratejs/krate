// Package tsexec runs TypeScript helper scripts (config bootstraps,
// generateStaticParams, etc.) with tsx. It prefers a tsx already installed on
// disk (the project's or the workspace's node_modules, or KRATE_TSX) and only
// falls back to `npx --yes tsx`. It centralizes the temp-bootstrap write,
// file:// URL conversion, subprocess execution, timeout handling, and stderr
// capture that the compiler uses to evaluate user TS at build time.
package tsexec

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/execx"
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

// execLockPath is a machine-wide lock serialising `npx tsx` invocations.
//
// npx installs the requested package into a shared cache dir
// (`$npm_config_cache/_npx/<hash>`), and that install is not concurrency-safe:
// two processes installing at once race on the same rename and one fails with
// ENOTEMPTY. Concurrent build workers, parallel `go test` package binaries, and
// multiple builds on one machine all trigger it (seen on macOS CI). One lock
// held for the duration of the exec fixes it portably, with no extra dependency.
var execLockPath = filepath.Join(os.TempDir(), "krate-npx-exec.lock")

// AcquireExecLock takes the machine-wide npx lock, removing a lock left behind
// by a crashed process once it is stale. It gives up (returning a no-op release)
// after maxWait so a wedged holder can never deadlock a build. Exported so other
// npx callers (e.g. the SSR renderer's tsx launch) can share the same lock.
func AcquireExecLock(maxWait time.Duration) (release func()) {
	const staleAfter = 10 * time.Minute
	deadline := time.Now().Add(maxWait)
	for {
		f, err := os.OpenFile(execLockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = f.WriteString(strconv.Itoa(os.Getpid()))
			_ = f.Close()
			return func() { _ = os.Remove(execLockPath) }
		}
		if fi, serr := os.Stat(execLockPath); serr == nil && time.Since(fi.ModTime()) > staleAfter {
			_ = os.Remove(execLockPath)
			continue
		}
		if time.Now().After(deadline) {
			return func() {}
		}
		time.Sleep(50 * time.Millisecond)
	}
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
// executes it with tsx from the given working directory. A locally installed
// tsx is preferred; otherwise `npx --yes tsx` is used (the `--yes` flag is
// required for non-interactive environments (CI): plain `npx tsx` would
// otherwise prompt "Ok to proceed?" and fail when tsx is neither installed nor
// cached). env is an optional set of KEY=VALUE strings appended to the
// subprocess environment (may be nil). It returns the script's stdout and
// stderr separately, plus a descriptive error (including stderr and timeout
// detection) when execution fails.
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

	// Prefer a tsx already installed on disk (the project or the workspace, e.g.
	// by `pnpm install`). Running it directly skips `npx`, which has to download
	// tsx (and its esbuild binary) into a shared, mutable cache that concurrent
	// installs race on - the source of flaky `npx tsx` failures on macOS CI.
	// Only when no local tsx exists do we fall back to `npx --yes tsx`, holding
	// the machine-wide lock across its install so concurrent callers don't
	// collide in the shared npx cache dir (ENOTEMPTY on macOS CI).
	tsx := resolveTSXRunner(cwd)
	release := func() {}
	if tsx == "" {
		release = AcquireExecLock(4 * time.Minute)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	defer release()

	args := make([]string, 0, 3)
	if tsconfig != "" {
		args = append(args, "--tsconfig", tsconfig)
	}
	args = append(args, path)

	var cmd *exec.Cmd
	if tsx != "" {
		cmd = tsxCommand(ctx, tsx, args)
	} else {
		cmd = execx.Command(ctx, "npx", append([]string{"--yes", "tsx"}, args...)...)
	}
	cmd.Dir = cwd
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	// execx.Command wires process-tree termination and a bounded post-kill wait:
	// on Windows `npx` is a shim that spawns a child `node`; killing only the
	// shim would leave the child holding the stdout/stderr pipes open, so
	// cmd.Wait() would block forever even after the deadline.
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, "", fmt.Errorf("execution timed out after %v", timeout)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			msg := strings.TrimSpace(string(exitErr.Stderr))
			if msg == "" {
				// A signal death or an otherwise silent failure: surface the exit
				// state and any stdout so the error is never just "execution
				// failed:" with nothing to act on.
				msg = fmt.Sprintf("%s (no stderr; stdout: %q)", exitErr.ProcessState, strings.TrimSpace(string(out)))
			}
			return nil, msg, fmt.Errorf("execution failed:\n%s", msg)
		}
		return nil, "", fmt.Errorf("execution: %w", err)
	}
	resultMu.Lock()
	resultCache[key] = out
	resultMu.Unlock()
	return out, "", nil
}

// tsxCommand builds the exec command for a resolved tsx runner. On Windows a
// `.cmd`/`.bat` shim (e.g. `node_modules/.bin/tsx.cmd`) cannot be run directly
// by CreateProcess, so it is wrapped in `cmd /c`.
func tsxCommand(ctx context.Context, runner string, args []string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(runner)) {
		case ".cmd", ".bat":
			return execx.Command(ctx, "cmd", append([]string{"/c", runner}, args...)...)
		}
	}
	return execx.Command(ctx, runner, args...)
}

// resolveTSXRunner returns a tsx executable to run directly, or "" to use
// `npx tsx`. It honors KRATE_TSX first (set it to "off"/"0"/"none" to force
// npx, or to a path to override), then searches for `node_modules/.bin/tsx`
// walking up from cwd and from the process working directory (which, under
// `go test`, is the package directory inside the repo, so the workspace install
// is found). Using a local install is faster and avoids npx's shared cache.
//
// On Windows the `.bin` directory holds `.cmd`/`.ps1` shims that cannot be
// executed directly, and `npx tsx` resolves the local install reliably there
// anyway, so this returns "" unless KRATE_TSX is set.
func resolveTSXRunner(cwd string) string {
	if v := strings.TrimSpace(os.Getenv("KRATE_TSX")); v != "" {
		switch strings.ToLower(v) {
		case "0", "off", "none":
			return ""
		}
		return v
	}
	if runtime.GOOS == "windows" {
		return ""
	}
	starts := make([]string, 0, 2)
	if cwd != "" {
		starts = append(starts, cwd)
	}
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	for _, start := range starts {
		for dir := start; ; {
			cand := filepath.Join(dir, "node_modules", ".bin", "tsx")
			if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
				return cand
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return ""
}
