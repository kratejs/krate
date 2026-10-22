package irtree

import (
	"sort"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Suspense-related IR support.
//
// The React lowering (bundler/react_suspense.go) turns use(promise),
// useDeferredValue and useSyncExternalStore declarations into calls of
// runtime factories that return getters. The builder treats such a binding
// like a resource getter: it is reactive (JSX reads become bindings), its
// declaration is replayed verbatim in the hydration scope, and a component
// that declares one always hydrates.

// reactiveFactories maps a factory callee to whether its build-time value is
// unknown (true) or computable from an argument (false).
var reactiveFactories = map[string]bool{
	"__krateUse":          true,
	"createDeferredValue": false,
	"createExternalStore": false,
}

// SuspendingFactory reports whether hydration code calling name can suspend a
// Suspense boundary (use() values and resources).
func SuspendingFactory(name string) bool {
	return name == "__krateUse" || name == "createResource"
}

// reactiveFactoryCall returns the factory name when init calls one.
func reactiveFactoryCall(init ast.Expr) (*ast.CallExpr, string) {
	call, ok := init.(*ast.CallExpr)
	if !ok {
		return nil, ""
	}
	id, ok := call.Callee.(*ast.Identifier)
	if !ok {
		return nil, ""
	}
	if _, ok := reactiveFactories[id.Name]; !ok {
		return nil, ""
	}
	return call, id.Name
}

// collectReactiveFactoryJS returns the factory declarations of a component
// body for the hydration scope, in source order.
func (b *builder) collectReactiveFactoryJS(body []ast.Stmt, order *bodyOrder) []bodyJS {
	var out []bodyJS
	for _, stmt := range body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, decl := range vs.Decls {
			if decl.Name == "" || decl.IsDestructuring {
				continue
			}
			if call, _ := reactiveFactoryCall(decl.Init); call == nil {
				continue
			}
			// A `var` declaration, so the folded-locals pass sees the name as
			// declared and does not emit it a second time.
			out = append(out, bodyJS{
				ord: order.of(decl),
				js:  "var " + decl.Name + "=" + generateExprJS(decl.Init, b.sigMap()),
			})
		}
	}
	return out
}

// registerFactoryLocals makes factory bindings reactive getters. A use()
// value is unknown at build time (like a resource); a deferred value starts
// from its initialValue (else its value); an external store from its server
// snapshot (else its client snapshot) when that is an inline arrow.
func (b *builder) registerFactoryLocals(body []ast.Stmt) {
	for _, stmt := range body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, decl := range vs.Decls {
			if decl.Name == "" || decl.IsDestructuring {
				continue
			}
			call, name := reactiveFactoryCall(decl.Init)
			if call == nil {
				continue
			}
			b.localSignals[decl.Name] = factoryInitial(name, call)
		}
	}
}

func factoryInitial(name string, call *ast.CallExpr) ast.Expr {
	switch name {
	case "createDeferredValue":
		if len(call.Args) >= 2 && call.Args[1] != nil {
			return call.Args[1]
		}
		if len(call.Args) >= 1 {
			if arrow, ok := call.Args[0].(*ast.ArrowFn); ok {
				if e := arrowBodyExpr(arrow); e != nil {
					return e
				}
			}
		}
	case "createExternalStore":
		for _, i := range []int{2, 1} {
			if len(call.Args) > i {
				if arrow, ok := call.Args[i].(*ast.ArrowFn); ok && len(arrow.Params) == 0 {
					if e := arrowBodyExpr(arrow); e != nil {
						return e
					}
				}
			}
		}
	}
	return resourceSentinel
}

// bodyUsesReactiveFactory reports whether a component body declares a
// use()/deferred/external-store value or a resource; such a component must
// hydrate even without signals of its own.
func bodyUsesReactiveFactory(body []ast.Stmt) bool {
	for _, stmt := range body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, decl := range vs.Decls {
			if call, _ := reactiveFactoryCall(decl.Init); call != nil {
				return true
			}
			if call, ok := decl.Init.(*ast.CallExpr); ok {
				if id, ok := call.Callee.(*ast.Identifier); ok && id.Name == "createResource" {
					return true
				}
			}
		}
	}
	return false
}

// LazyComponent records a component declared with React.lazy(); its
// hydration code is emitted into a separately loaded chunk.
type LazyComponent struct {
	// Spec is the module specifier passed to import().
	Spec string
	// File, Line and Col locate the lazy() declaration; Source is its line.
	File   string
	Line   int
	Col    int
	Source string
	// NoDefaultExport is set when the loader used the module's default
	// export and the module has none.
	NoDefaultExport bool
	// Impl and Loader are set for a lazy() whose loader is not a static
	// import: Impl is the local component it resolves to, Loader the
	// module-level binding holding the loader function.
	Impl   string
	Loader string
}

// isLazyComponent reports whether name was declared with lazy().
func (b *builder) isLazyComponent(name string) bool {
	_, ok := b.ann.LazyComponents[name]
	return ok
}

// lazyLoaderGate makes a promise-loader lazy instance suspend until its
// loader resolved (the loader starts when the instance first hydrates).
func (b *builder) lazyLoaderGate(node *ComponentNode, lc LazyComponent) {
	init := b.moduleConstInits[lc.Loader]
	if init == nil {
		return
	}
	js := generateExprJS(init, nil)
	if js == "" {
		return
	}
	node.LazyLoader = lc.Loader
	// ExtraVars, so helpers the loader calls are hoisted into the scope.
	node.ExtraVars = append([]string{
		"var " + lc.Loader + "=" + js,
		"__krateRead(__krateLazyPayload(" + lc.Loader + "))",
	}, node.ExtraVars...)
}

// checkLazyComponents reports lazy components whose module has no default
// component export (the alias never resolved to a function).
func (b *builder) checkLazyComponents() {
	names := make([]string, 0, len(b.ann.LazyComponents))
	for name := range b.ann.LazyComponents {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		lc := b.ann.LazyComponents[name]
		if b.ann.Functions[name] != nil && !lc.NoDefaultExport {
			continue
		}
		what := "module " + lc.Spec + " has no default export that is a component"
		if lc.Impl != "" {
			what = "its loader resolves to " + lc.Impl + ", which is not a component"
		}
		d := diag.New(lc.File, lc.Line, lc.Col,
			"lazy() component "+name+": "+what,
			"export the component with `export default function "+name+"()`, or map a named export with .then((m) => ({ default: m.Name }))",
			lc.Source).WithCode(diag.CodeLazyDefaultExport)
		b.buildErrs = append(b.buildErrs, d)
	}
}

// warnSuspendOutsideBoundary warns when a component reading use(promise) is
// rendered outside any <Suspense> boundary.
func (b *builder) warnSuspendOutsideBoundary(fn *ast.FnDecl, pos ast.Pos) {
	if b.suspenseDepth > 0 || fn == nil || !bodyCallsUse(fn.Body) {
		return
	}
	d := b.diagAt(pos, diag.SeverityWarning,
		"<"+fn.Name+"> calls use(promise) but is not inside a <Suspense> boundary",
		"wrap it in <Suspense fallback={...}> so a fallback shows while the promise is pending")
	b.warnings = append(b.warnings, d.WithCode(diag.CodeSuspendNoBoundary))
}

func bodyCallsUse(body []ast.Stmt) bool {
	for _, stmt := range body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, decl := range vs.Decls {
			if _, name := reactiveFactoryCall(decl.Init); name == "__krateUse" {
				return true
			}
		}
	}
	return false
}
