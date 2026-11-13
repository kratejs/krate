package build

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/native/jsruntime"
)

// maxAPIDurationSeconds caps `export const maxDuration`.
const maxAPIDurationSeconds = 3600

// apiTimeoutGrace is how much longer than the longest route budget the host
// waits for the sidecar, so the sidecar's own 504 reaches the client first.
const apiTimeoutGrace = 2 * time.Second

// readAPIMaxDuration reads `export const maxDuration = <seconds>` from an API
// route source. It returns 0 when the route declares none or the value cannot
// be used; the diagnostics then explain why (warnings to print).
func readAPIMaxDuration(file string) (int, []diag.Diagnostic) {
	data, err := os.ReadFile(file)
	if err != nil {
		return 0, nil
	}
	prog, err := parseSourceFile(file, data)
	if err != nil || prog == nil {
		return 0, nil
	}
	return apiMaxDurationFromProgram(file, string(data), prog)
}

func apiMaxDurationFromProgram(file, source string, prog *ast.Program) (int, []diag.Diagnostic) {
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok {
			continue
		}
		vs, ok := exp.Declaration.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, d := range vs.Decls {
			if d.Name != "maxDuration" || d.Init == nil {
				continue
			}
			warn := func(msg, hint string) (int, []diag.Diagnostic) {
				return 0, []diag.Diagnostic{warnAt(file, source, exp.Position, diag.CodeSegmentExportValue, msg, hint)}
			}
			const hint = "write a positive whole number of seconds, such as `export const maxDuration = 60`"
			n, ok := constNumber(d.Init)
			if !ok {
				return warn("`export const maxDuration` must be a constant, positive whole number of seconds; the value is ignored and the default API timeout applies", hint)
			}
			if n <= 0 || n != math.Trunc(n) || math.IsInf(n, 0) {
				return warn(fmt.Sprintf("`export const maxDuration` must be a positive whole number of seconds, got %v; the value is ignored and the default API timeout applies", n), hint)
			}
			if n > maxAPIDurationSeconds {
				return maxAPIDurationSeconds, []diag.Diagnostic{warnAt(file, source, exp.Position, diag.CodeSegmentExportValue,
					fmt.Sprintf("`export const maxDuration` of %v seconds is above the supported maximum; it is capped at %d", n, maxAPIDurationSeconds), "")}
			}
			return int(n), nil
		}
	}
	return 0, nil
}

// readAPIRouteMeta reads .krate/api-routes.json.
func readAPIRouteMeta(root string) []apiRouteMeta {
	data, err := os.ReadFile(filepath.Join(root, ".krate", "api-routes.json"))
	if err != nil {
		return nil
	}
	var list []apiRouteMeta
	if json.Unmarshal(data, &list) != nil {
		return nil
	}
	return list
}

// apiRouteBudgets returns the per-route handler budget of every route that
// sets maxDuration, keyed by out-dir relative file name.
func apiRouteBudgets(root string) map[string]time.Duration {
	out := map[string]time.Duration{}
	for _, m := range readAPIRouteMeta(root) {
		if m.MaxDuration > 0 {
			out[m.File] = time.Duration(m.MaxDuration) * time.Second
		}
	}
	return out
}

// longestAPIBudget is the longest a request to an API route may take: the
// larger of KRATE_API_TIMEOUT and every route's maxDuration.
func longestAPIBudget(budgets map[string]time.Duration) time.Duration {
	longest := jsruntime.APITimeout()
	for _, d := range budgets {
		if d > longest {
			longest = d
		}
	}
	return longest
}
