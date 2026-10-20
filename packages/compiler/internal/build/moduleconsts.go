package build

import (
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// mergeModuleConstants builds a module-constant map for the IR builder from the
// entry page and its imported modules. Imported modules are visited first so the
// entry module wins on name collisions, and every module's
// `export { local as remote }` aliases are added (covering re-export barrels).
func mergeModuleConstants(entry *ast.Program, extra []annotator.ModuleSource) (map[string]string, map[string]ast.Expr) {
	consts := make(map[string]string)
	inits := make(map[string]ast.Expr)
	add := func(prog *ast.Program) {
		if prog == nil {
			return
		}
		c, in := irtree.CollectModuleConstants(prog)
		for k, v := range c {
			if _, ok := consts[k]; !ok {
				consts[k] = v
			}
		}
		for k, v := range in {
			if _, ok := inits[k]; !ok {
				inits[k] = v
			}
		}
		for _, stmt := range prog.Body {
			exp, ok := stmt.(*ast.ExportStmt)
			if !ok {
				continue
			}
			for _, n := range exp.Named {
				if n.Local == "" || n.Remote == "" {
					continue
				}
				if v, ok := consts[n.Local]; ok {
					consts[n.Remote] = v
				}
				if e, ok := inits[n.Local]; ok {
					inits[n.Remote] = e
				}
			}
		}
	}
	for _, m := range extra {
		if isExternalModulePath(m.Path) {
			continue
		}
		add(m.Program)
	}
	add(entry)

	// Entry import aliases (`import { upper as shout }`) bind the local name to
	// the exported constant's value.
	if entry == nil {
		return consts, inits
	}
	for _, stmt := range entry.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok {
			continue
		}
		for _, n := range imp.Named {
			local := n.Local
			if local == "" {
				local = n.Remote
			}
			if local == n.Remote {
				continue
			}
			if v, ok := consts[n.Remote]; ok {
				consts[local] = v
			}
			if e, ok := inits[n.Remote]; ok {
				inits[local] = e
			}
		}
	}
	return consts, inits
}

// isExternalModulePath reports whether a module is a compiled dependency under
// node_modules. Such packages are bundled into islands; their module constants
// must not leak into the page hydration scope. Workspace @krate packages are
// Krate source and remain eligible.
func isExternalModulePath(p string) bool {
	norm := "/" + strings.TrimPrefix(filepath.ToSlash(p), "./")
	return strings.Contains(norm, "/node_modules/") && !strings.Contains(norm, "/node_modules/@krate/")
}
