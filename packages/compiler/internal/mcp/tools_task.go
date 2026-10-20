package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func (s *Service) allowedTasks() map[string]bool {
	allowed := map[string]bool{"build": true, "check": true, "test": true, "typecheck": true}
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
		if _, err := exec.LookPath("npm"); err != nil {
			return ErrorResult("npm is not installed; cannot run " + task), nil
		}
		runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(runCtx, "npm", "run", task)
		cmd.Dir = s.root
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		reportProgress(ctx, 1, 1, task+" complete")
		text := string(out)
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
