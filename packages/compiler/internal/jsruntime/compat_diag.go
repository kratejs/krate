package jsruntime

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	native "github.com/kratejs/krate/packages/native/jsruntime"
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
	ledger := native.Compat()
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
		rel := relToRoot(root, r.File)
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

// relToRoot returns path relative to root, or path unchanged when it is
// outside root. esbuild reports symlink-resolved paths (macOS /var is
// /private/var), so when the plain comparison fails it retries with the
// symlinks of either side resolved.
func relToRoot(root, path string) string {
	if root == "" {
		return path
	}
	inside := func(r, p string) (string, bool) {
		rel, err := filepath.Rel(r, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", false
		}
		return rel, true
	}
	if rel, ok := inside(root, path); ok {
		return rel
	}
	realRoot, errR := filepath.EvalSymlinks(root)
	realPath, errP := filepath.EvalSymlinks(path)
	if errR != nil {
		realRoot = root
	}
	if errP != nil {
		realPath = path
	}
	for _, c := range [][2]string{{realRoot, path}, {root, realPath}, {realRoot, realPath}} {
		if rel, ok := inside(c[0], c[1]); ok {
			return rel
		}
	}
	return path
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
