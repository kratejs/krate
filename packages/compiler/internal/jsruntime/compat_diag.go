package jsruntime

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Runtime option keys CompatDiagnostics accepts.
const (
	CompatAPIRuntime        = "ssr.apiRuntime"
	CompatMiddlewareRuntime = "ssr.middlewareRuntime"
)

// CompatDiagnostics turns the unsupported references of a scan into KR7160
// warnings: one per (file, api) for project files and one per (package, api)
// for files under node_modules, at the first reference. Paths are reported
// relative to root. option is CompatAPIRuntime or CompatMiddlewareRuntime and
// picks the runtime the message and hint name.
func CompatDiagnostics(scan CompatScan, root, option string) []diag.Diagnostic {
	refs := scan.Unsupported()
	sort.SliceStable(refs, func(i, j int) bool {
		a, b := refs[i], refs[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Col < b.Col
	})
	kind := "API"
	if option == CompatMiddlewareRuntime {
		kind = "middleware"
	}
	ledger := Compat()
	lines := map[string][]string{}
	seen := map[[2]string]bool{}
	var out []diag.Diagnostic
	for _, r := range refs {
		pkg := NodeModulesPackage(r.File)
		unit := r.File
		if pkg != "" {
			unit = "pkg:" + pkg
		}
		key := [2]string{unit, r.API}
		if seen[key] {
			continue
		}
		seen[key] = true
		row, _ := ledger.Lookup(r.API)
		msg := r.API + " is not supported by the embedded " + kind + " runtime"
		if pkg != "" {
			msg = "dependency " + pkg + " uses " + r.API + ", which the embedded " + kind + " runtime does not support (it may only use it after a feature check)"
		}
		hint := row.Hint
		if option == CompatMiddlewareRuntime {
			hint = strings.ReplaceAll(hint, CompatAPIRuntime, CompatMiddlewareRuntime)
		}
		rel := r.File
		if root != "" {
			if p, err := filepath.Rel(root, r.File); err == nil && !strings.HasPrefix(p, "..") {
				rel = p
			}
		}
		if _, ok := lines[r.File]; !ok {
			if data, err := os.ReadFile(r.File); err == nil {
				lines[r.File] = strings.Split(string(data), "\n")
			} else {
				lines[r.File] = nil
			}
		}
		src := ""
		if ls := lines[r.File]; r.Line >= 1 && r.Line <= len(ls) {
			src = strings.TrimRight(ls[r.Line-1], "\r")
		}
		out = append(out, diag.Diagnostic{
			File:     filepath.ToSlash(rel),
			Line:     r.Line,
			Col:      r.Col,
			Message:  msg,
			Hint:     hint,
			Source:   src,
			Severity: diag.SeverityWarning,
			Code:     diag.CodeAPIRuntimeUnsupported,
		})
	}
	return out
}

// NodeModulesPackage returns the package name ("ws", "@scope/pkg") of a file
// under node_modules, using the innermost node_modules directory, or "".
func NodeModulesPackage(path string) string {
	p := filepath.ToSlash(path)
	const marker = "node_modules/"
	i := strings.LastIndex(p, "/"+marker)
	if i >= 0 {
		i++
	} else if strings.HasPrefix(p, marker) {
		i = 0
	} else {
		return ""
	}
	parts := strings.Split(p[i+len(marker):], "/")
	if len(parts) < 2 {
		return ""
	}
	if strings.HasPrefix(parts[0], "@") {
		if len(parts) < 3 {
			return ""
		}
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}
