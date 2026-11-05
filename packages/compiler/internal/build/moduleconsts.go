package build

import (
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
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
	return resolver.IsCompiledDependency("/" + strings.TrimPrefix(filepath.ToSlash(p), "./"))
}

// applyModuleBindings fills the module-level binding options of the IR build:
// constants and classes, namespace-merged vars, the imported modules' top-level
// statements, and `import * as ns` objects.
func applyModuleBindings(opts *irtree.BuildOptions, entryPath string, entry *ast.Program, extra []annotator.ModuleSource) {
	opts.ModuleConsts, opts.ModuleConstInits = mergeModuleConstants(entry, extra)
	declOnly := map[string]bool{}
	var stmts []ast.Stmt
	for i := len(extra) - 1; i >= 0; i-- {
		m := extra[i]
		if isExternalModulePath(m.Path) || m.Program == nil {
			continue
		}
		for k := range irtree.CollectModuleDeclOnly(m.Program) {
			declOnly[k] = true
		}
		stmts = append(stmts, irtree.CollectModuleStmts(m.Program)...)
	}
	if len(declOnly) > 0 {
		opts.ModuleDeclOnly = declOnly
	}
	opts.ModuleStmts = stmts
	opts.ModuleNamespaces = moduleNamespaces(annotator.ResolverOf(extra), entryPath, entry, extra)
}

// moduleNamespaces maps each namespace-import binding (`import * as ns from`,
// `export * as ns from`) to the export names of the module it names.
func moduleNamespaces(res *bundler.ImportResolver, entryPath string, entry *ast.Program, extra []annotator.ModuleSource) map[string]map[string]string {
	byPath := map[string]*ast.Program{}
	for _, m := range extra {
		if m.Program != nil && !isExternalModulePath(m.Path) {
			byPath[filepath.Clean(m.Path)] = m.Program
		}
	}
	out := map[string]map[string]string{}
	visit := func(path string, prog *ast.Program) {
		if prog == nil {
			return
		}
		for _, stmt := range prog.Body {
			var ns, src string
			switch s := stmt.(type) {
			case *ast.ImportStmt:
				ns, src = s.Namespace, s.Source
			case *ast.ExportStmt:
				ns, src = s.Namespace, s.ReexportSource
			}
			if ns == "" || src == "" {
				continue
			}
			target := res.Resolve(path, strings.Trim(src, "\"'"))
			tp := byPath[filepath.Clean(target)]
			if tp == nil {
				continue
			}
			if _, dup := out[ns]; dup {
				continue
			}
			out[ns] = exportBindings(res, filepath.Clean(target), byPath, map[string]bool{})
		}
	}
	visit(entryPath, entry)
	for _, m := range extra {
		if !isExternalModulePath(m.Path) {
			visit(m.Path, m.Program)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// exportBindings lists a module's exports as exported name -> flattened
// binding name, following `export *` through bundled modules.
func exportBindings(res *bundler.ImportResolver, path string, byPath map[string]*ast.Program, seen map[string]bool) map[string]string {
	out := map[string]string{}
	prog := byPath[path]
	if prog == nil || seen[path] {
		return out
	}
	seen[path] = true
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok {
			continue
		}
		switch d := exp.Declaration.(type) {
		case *ast.FnDecl:
			if !exp.Default && d.Name != "" {
				out[d.Name] = d.Name
			}
		case *ast.ClassDecl:
			if !exp.Default && d.Name != "" {
				out[d.Name] = d.Name
			}
		case *ast.VarStmt:
			for _, decl := range d.Decls {
				if decl != nil && decl.Name != "" {
					out[decl.Name] = decl.Name
				}
			}
		}
		for _, n := range exp.Named {
			if n.Local != "" && n.Remote != "" && n.Remote != "default" {
				out[n.Remote] = n.Local
			}
		}
		if exp.ReexportSource != "" {
			target := filepath.Clean(res.Resolve(path, strings.Trim(exp.ReexportSource, "\"'")))
			switch {
			case exp.StarReexport:
				for k, v := range exportBindings(res, target, byPath, seen) {
					if _, dup := out[k]; !dup {
						out[k] = v
					}
				}
			case exp.Namespace != "":
				out[exp.Namespace] = exp.Namespace
			default:
				for _, n := range exp.ReexportNamed {
					if n.Local != "" && n.Remote != "" && n.Remote != "default" {
						out[n.Remote] = n.Local
					}
				}
			}
		}
	}
	return out
}
