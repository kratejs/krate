package build

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

// clientModuleStmts lists the top-level setup statements (calls, `new`,
// member assignments rooted in a binding imported from a package) of the
// Krate-source modules that run in the browser
// with entry, in module dependency order: a module's imports come before it.
// Server, static and runtime modules, "use server" modules and modules only
// reached through them are left out, as are npm and krate.vendor code, which
// esbuild bundles with its own side effects.
func clientModuleStmts(bundle *bundler.Bundle, entry *bundler.Module, root string) []irtree.ClientModuleStmt {
	if bundle == nil || entry == nil || entry.Program == nil {
		return nil
	}
	byPath := make(map[string]*bundler.Module, len(bundle.Modules))
	for _, m := range bundle.Modules {
		if m != nil && m.Program != nil && !m.IsExternal && !m.IsCSS {
			byPath[filepath.Clean(m.Path)] = m
		}
	}
	var out []irtree.ClientModuleStmt
	state := map[string]int{} // 1: visited off the client, 2: visited on it
	var visit func(m *bundler.Module, client bool)
	visit = func(m *bundler.Module, client bool) {
		if resolver.IsCompiledDependency(m.Path) {
			return
		}
		serverish := serverOnlyModule(m)
		client = !serverish && (client || bundler.HasUseClientDirective(m.SourceCode) || declaresComponent(m.Program))
		want := 1
		if client {
			want = 2
		}
		key := filepath.Clean(m.Path)
		if state[key] >= want {
			return
		}
		state[key] = want
		for _, spec := range m.Imports {
			resolved := bundle.Resolver.Resolve(m.Path, spec)
			if dep := byPath[filepath.Clean(resolved)]; dep != nil && dep != m {
				visit(dep, client)
			}
		}
		if !client {
			return
		}
		bindings := moduleBindingNames(m.Program)
		packages := packageImportNames(bundle, m, root)
		rel, err := filepath.Rel(root, m.Path)
		if err != nil {
			rel = m.Path
		}
		sum := sha256.Sum256([]byte(filepath.ToSlash(rel)))
		tag := hex.EncodeToString(sum[:4])
		for _, stmt := range m.Program.Body {
			// Only setup code of packages: compound components
			// (`Dialog.Trigger = ...`), decorators and local mutations are
			// handled where they are used.
			if r := irtree.SideEffectRoot(stmt); r == "" || !packages[r] {
				continue
			}
			pos := stmt.Pos()
			if pos.Line == 0 {
				// Added by the compiler, not written in the module.
				continue
			}
			out = append(out, irtree.ClientModuleStmt{
				Stmt:     stmt,
				Key:      "#s:" + tag + ":" + strconv.Itoa(pos.Line) + ":" + strconv.Itoa(pos.Col),
				File:     m.Path,
				Bindings: bindings,
			})
		}
	}
	visit(entry, true)
	return out
}

// serverOnlyModule reports a module whose code never runs in the browser.
func serverOnlyModule(m *bundler.Module) bool {
	switch m.ComponentClass {
	case bundler.ComponentClassServer, bundler.ComponentClassStatic, bundler.ComponentClassRuntime:
		return true
	}
	return useServerModule(m.Program)
}

func useServerModule(prog *ast.Program) bool {
	for _, stmt := range prog.Body {
		es, ok := stmt.(*ast.ExprStmt)
		if !ok {
			return false
		}
		lit, ok := es.Expression.(*ast.Literal)
		if !ok || lit.Kind != ast.StringLit {
			return false
		}
		if strings.Trim(strings.TrimSpace(lit.Value), `"'`) == "use server" {
			return true
		}
	}
	return false
}

// declaresComponent reports a module that declares a component (an
// uppercase function), which makes it client code by default.
func declaresComponent(prog *ast.Program) bool {
	for _, stmt := range prog.Body {
		if exp, ok := stmt.(*ast.ExportStmt); ok && exp.Declaration != nil {
			stmt = exp.Declaration
		}
		switch s := stmt.(type) {
		case *ast.FnDecl:
			if s.Name != "" && s.Name[0] >= 'A' && s.Name[0] <= 'Z' {
				return true
			}
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if d == nil || d.Name == "" || d.Name[0] < 'A' || d.Name[0] > 'Z' {
					continue
				}
				if _, ok := d.Init.(*ast.ArrowFn); ok {
					return true
				}
			}
		}
	}
	return false
}

// moduleBindingNames returns the names a module imports from other modules
// or declares at top level. Imports of the client runtime are globals in the
// page script and are left out.
func moduleBindingNames(prog *ast.Program) map[string]bool {
	out := map[string]bool{}
	for _, stmt := range prog.Body {
		switch s := stmt.(type) {
		case *ast.ImportStmt:
			src := strings.Trim(s.Source, "\"'`")
			if runtimeSpecifier(src) {
				continue
			}
			if s.Default != "" {
				out[s.Default] = true
			}
			if s.Namespace != "" {
				out[s.Namespace] = true
			}
			for _, n := range s.Named {
				local := n.Local
				if local == "" {
					local = n.Remote
				}
				out[local] = true
			}
		default:
			if exp, ok := stmt.(*ast.ExportStmt); ok && exp.Declaration != nil {
				stmt = exp.Declaration
			}
			switch d := stmt.(type) {
			case *ast.FnDecl:
				out[d.Name] = true
			case *ast.ClassDecl:
				out[d.Name] = true
			case *ast.VarStmt:
				for _, v := range d.Decls {
					if v != nil && v.Name != "" {
						out[v.Name] = true
					}
				}
			}
		}
	}
	return out
}

// vendorLocalNames returns the bindings the page script takes from the
// vendor bundle.
func vendorLocalNames(v []vendorImport) map[string]bool {
	out := make(map[string]bool, len(v))
	for _, u := range v {
		if !strings.HasPrefix(u.Source, "node:") {
			out[u.Local] = true
		}
	}
	return out
}

// applyClientModuleStmts hands the IR build the module setup statements to
// replay in the page script, with the bindings the vendor bundle provides.
func (b *Builder) applyClientModuleStmts(opts *irtree.BuildOptions, bundle *bundler.Bundle, entry *bundler.Module) {
	stmts := clientModuleStmts(bundle, entry, b.Root)
	if len(stmts) == 0 {
		return
	}
	opts.ClientModuleStmts = stmts
	opts.VendorLocals = vendorLocalNames(collectVendorImports(bundle))
}

// packageImportNames returns the bindings m imports from packages (bare
// specifiers resolving into node_modules or outside the project), leaving out
// the client runtime, whose exports are page globals.
func packageImportNames(bundle *bundler.Bundle, m *bundler.Module, root string) map[string]bool {
	out := map[string]bool{}
	rootSlash := filepath.ToSlash(filepath.Clean(root)) + "/"
	for _, stmt := range m.Program.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok {
			continue
		}
		src := strings.Trim(imp.Source, "\"'`")
		if src == "" || strings.HasPrefix(src, ".") || strings.HasPrefix(src, "/") || strings.HasPrefix(src, "#") || runtimeSpecifier(src) {
			continue
		}
		resolved := filepath.ToSlash(bundle.Resolver.Resolve(m.Path, src))
		if resolved == "" || (strings.HasPrefix(resolved, rootSlash) && !strings.Contains(resolved, "/node_modules/")) {
			continue
		}
		if imp.Default != "" {
			out[imp.Default] = true
		}
		if imp.Namespace != "" {
			out[imp.Namespace] = true
		}
		for _, n := range imp.Named {
			local := n.Local
			if local == "" {
				local = n.Remote
			}
			out[local] = true
		}
	}
	return out
}

func runtimeSpecifier(src string) bool {
	return src == "@krate/runtime" || strings.HasPrefix(src, "@krate/runtime/") || src == "react" || src == "react-dom" ||
		strings.HasPrefix(src, "react/") || strings.HasPrefix(src, "react-dom/")
}
