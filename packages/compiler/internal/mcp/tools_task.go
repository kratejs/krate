package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/execx"
)

func (s *Service) allowedTasks() map[string]bool {
	// build/check are the default safe set. Arbitrary package scripts
	// (test/typecheck) require an explicit allowTasks entry because they run
	// project code with the server's environment. A read-only profile runs no
	// project tasks at all.
	if s.policy != nil && s.policy.ReadOnlyMode {
		return map[string]bool{}
	}
	allowed := map[string]bool{"build": true, "check": true}
	if s.policy != nil && len(s.policy.AllowTasks) > 0 {
		allowed = map[string]bool{}
		for _, t := range s.policy.AllowTasks {
			if t = strings.TrimSpace(t); t != "" {
				allowed[t] = true
			}
		}
	}
	return allowed
}

// toolRunTask runs a policy-approved project task. build/check run in-process;
// test/typecheck invoke the project's package script without a shell.
func (s *Service) toolRunTask(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	task := argString(args, "task")
	if task == "" {
		return ErrorResult("provide task (build, check, test, or typecheck)"), nil
	}
	if s.policy != nil && s.policy.ReadOnlyMode {
		return ErrorResult("run_task is disabled by readOnlyMode in .krate/mcp.json"), nil
	}
	if !s.allowedTasks()[task] {
		return ErrorResult("task " + task + " is not in allowTasks in .krate/mcp.json"), nil
	}
	reportProgress(ctx, 0, 1, "running "+task)
	s.logMessage("info", "running task "+task)
	switch task {
	case "build":
		out, err := s.runBuild(ctx)
		reportProgress(ctx, 1, 1, "build complete")
		if err != nil {
			return ErrorResult(fmt.Sprintf("build failed: %v\n\n%s", err, out)), nil
		}
		return JSONResult(map[string]any{"task": task, "ok": true, "output": strings.TrimSpace(out)})
	case "check":
		return s.toolCheck(ctx, args)
	case "test", "typecheck":
		pm := s.packageManager()
		if _, err := exec.LookPath(pm); err != nil {
			return ErrorResult(pm + " is not installed; cannot run " + task), nil
		}
		runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		args := []string{"run", task}
		if pm == "bun" {
			args = []string{"run", task}
		}
		cmd := execx.Command(runCtx, pm, args...)
		cmd.Dir = s.root
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		reportProgress(ctx, 1, 1, task+" complete")
		text := redactTaskOutput(string(out))
		if len(text) > 1<<20 {
			text = text[:1<<20] + "\n... (truncated)"
		}
		if err != nil {
			return ErrorResult(fmt.Sprintf("%s failed: %v\n\n%s", task, err, text)), nil
		}
		return JSONResult(map[string]any{"task": task, "ok": true, "output": text})
	default:
		return ErrorResult("unsupported task: " + task), nil
	}
}

func (s *Service) packageManager() string {
	switch {
	case mcpFileExists(filepath.Join(s.root, "pnpm-lock.yaml")):
		return "pnpm"
	case mcpFileExists(filepath.Join(s.root, "bun.lockb")) || mcpFileExists(filepath.Join(s.root, "bun.lock")):
		return "bun"
	case mcpFileExists(filepath.Join(s.root, "yarn.lock")):
		return "yarn"
	default:
		return "npm"
	}
}

func mcpFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func redactTaskOutput(text string) string {
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || len(v) < 4 {
			continue
		}
		upper := strings.ToUpper(k)
		if strings.Contains(upper, "SECRET") || strings.Contains(upper, "TOKEN") ||
			strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "API_KEY") ||
			strings.Contains(upper, "PRIVATE_KEY") {
			text = strings.ReplaceAll(text, v, "[redacted]")
		}
	}
	return text
}
