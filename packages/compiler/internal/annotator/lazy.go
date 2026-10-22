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
