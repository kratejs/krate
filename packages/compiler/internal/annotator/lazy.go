package annotator

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	irtree "github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// collectLazyComponents records the lazy() components of a module from the
// `const __krate_lazy__<Name> = "<spec>"` markers the React lowering leaves
// in place of each module-level lazy() declaration.
func collectLazyComponents(body []ast.Stmt, ann *irtree.Annotations, file, raw string) {
	for _, stmt := range body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, decl := range vs.Decls {
			name, ok := strings.CutPrefix(decl.Name, bundler.LazyMarkerPrefix)
			if !ok || name == "" {
				continue
			}
			lit, ok := decl.Init.(*ast.Literal)
			if !ok || lit.Kind != ast.StringLit {
				continue
			}
			if ann.LazyComponents == nil {
				ann.LazyComponents = make(map[string]irtree.LazyComponent)
			}
			if _, exists := ann.LazyComponents[name]; exists {
				continue
			}
			lc := irtree.LazyComponent{
				Spec:   lit.Value,
				File:   file,
				Line:   vs.Position.Line,
				Col:    vs.Position.Col,
				Source: lineOf(raw, vs.Position.Line),
			}
			if impl, ok := strings.CutPrefix(lit.Value, bundler.LazyLocalSpec); ok {
				lc.Spec, lc.Impl, lc.Loader = "", impl, bundler.LazyLoaderPrefix+name
				aliasLazyImpl(ann, name, impl)
			}
			ann.LazyComponents[name] = lc
		}
	}
}

// lowerContextUses turns `const v = __krateUse(() => __krateRead(X))` back
// into the context read `createMemo(() => X.useContext())` when X is bound to createContext()
// in any bundled module (the React lowering only sees its own module, so a
// context imported from elsewhere was lowered as a possible promise).
func lowerContextUses(ann *irtree.Annotations, modules []ModuleSource) {
	contexts := map[string]bool{}
	for _, m := range modules {
		if m.Program == nil {
			continue
		}
		for _, stmt := range m.Program.Body {
			vs, ok := stmt.(*ast.VarStmt)
			if !ok {
				if exp, isExp := stmt.(*ast.ExportStmt); isExp {
					vs, ok = exp.Declaration.(*ast.VarStmt)
				}
			}
			if !ok || vs == nil {
				continue
			}
			for _, d := range vs.Decls {
				if call, ok := d.Init.(*ast.CallExpr); ok && d.Name != "" {
					if id, ok := call.Callee.(*ast.Identifier); ok && (id.Name == "createContext" || id.Name == "createScope") {
						contexts[d.Name] = true
					}
				}
			}
		}
	}
	if len(contexts) == 0 {
		return
	}
	// Renamed imports of a context are contexts too.
	for _, m := range modules {
		if m.Program == nil {
			continue
		}
		for _, stmt := range m.Program.Body {
			if imp, ok := stmt.(*ast.ImportStmt); ok {
				for _, n := range imp.Named {
					if n.Local != "" && contexts[n.Remote] {
						contexts[n.Local] = true
					}
				}
			}
		}
	}
	seen := map[*ast.FnDecl]bool{}
	for _, fn := range ann.Functions {
		if fn == nil || seen[fn] {
			continue
		}
		seen[fn] = true
		for _, stmt := range fn.Body {
			vs, ok := stmt.(*ast.VarStmt)
			if !ok {
				continue
			}
			for _, d := range vs.Decls {
				if name := usedContextArg(d.Init); name != "" && contexts[name] {
					// Reads of the binding were already lowered to getter
					// calls, so it stays a getter: a memo over the context read
					// (which also folds to the context default at build time).
					read := &ast.CallExpr{Callee: &ast.MemberExpr{
						Object:   &ast.Identifier{Name: name},
						Property: &ast.Identifier{Name: "useContext"},
					}}
					d.Init = &ast.CallExpr{
						Callee: &ast.Identifier{Name: "createMemo"},
						Args:   []ast.Expr{&ast.ArrowFn{Expression: true, Body: []ast.Stmt{&ast.ReturnStmt{Value: read}}}},
					}
				}
			}
		}
	}
}

// usedContextArg returns X for `__krateUse(() => __krateRead(X))`.
func usedContextArg(init ast.Expr) string {
	call, ok := init.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return ""
	}
	if id, ok := call.Callee.(*ast.Identifier); !ok || id.Name != "__krateUse" {
		return ""
	}
	arrow, ok := call.Args[0].(*ast.ArrowFn)
	if !ok || len(arrow.Body) != 1 {
		return ""
	}
	ret, ok := arrow.Body[0].(*ast.ReturnStmt)
	if !ok {
		return ""
	}
	read, ok := ret.Value.(*ast.CallExpr)
	if !ok || len(read.Args) != 1 {
		return ""
	}
	if id, ok := read.Callee.(*ast.Identifier); !ok || id.Name != "__krateRead" {
		return ""
	}
	if arg, ok := read.Args[0].(*ast.Identifier); ok {
		return arg.Name
	}
	return ""
}

// aliasLazyImpl makes the lazy binding resolve to its local component.
func aliasLazyImpl(ann *irtree.Annotations, name, impl string) {
	fn := ann.Functions[impl]
	if fn == nil {
		return
	}
	if _, exists := ann.Functions[name]; !exists {
		ann.Functions[name] = fn
		ann.ComponentSources[name] = ann.ComponentSources[impl]
		ann.ComponentRaw[name] = ann.ComponentRaw[impl]
	}
}

// markLazyDefaults flags lazy components whose module has no default export
// when the lazy loader used the default export. The builder reports them.
func markLazyDefaults(ann *irtree.Annotations, modules []ModuleSource) {
	if len(ann.LazyComponents) == 0 {
		return
	}
	byPath := make(map[string]*ast.Program, len(modules))
	for _, m := range modules {
		if m.Program != nil && m.Path != "" {
			byPath[m.Path] = m.Program
		}
	}
	for name, lc := range ann.LazyComponents {
		if lc.Impl != "" {
			aliasLazyImpl(ann, name, lc.Impl)
			continue
		}
		owner := byPath[lc.File]
		if owner == nil || !importsDefaultAs(owner.Body, name) {
			continue
		}
		target := byPath[bundler.ResolveImport(lc.File, lc.Spec)]
		if target != nil && FindDefaultExport(target.Body) == "" {
			lc.NoDefaultExport = true
			ann.LazyComponents[name] = lc
		}
	}
}

func importsDefaultAs(body []ast.Stmt, name string) bool {
	for _, stmt := range body {
		if imp, ok := stmt.(*ast.ImportStmt); ok && imp.Default == name {
			return true
		}
	}
	return false
}
