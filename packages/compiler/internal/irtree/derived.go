package irtree

import (
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// Derived render values.
//
// The bundler lowers a React component's render-dependent body bindings to
// `const x = krateDerived(() => expr)` and custom hook results to
// `const h = krateHook(useThing())` (see bundler/react_derived.go). Both are
// getters: every read site already calls them. The builder registers them in
// the component's signal table so bindings that read them are reactive and
// their SSR initial value is the getter's build-time value. A derived context
// read (`krateDerived(() => Ctx.useContext())`) reads the provider's value
// through the synthetic context prop at hydration, or the context default
// when no dynamic provider is active.

// derivedCallee returns "krateDerived" or "krateHook" when expr is a call to
// one of them, or "".
func derivedCallee(expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return ""
	}
	id, ok := call.Callee.(*ast.Identifier)
	if !ok {
		return ""
	}
	switch id.Name {
	case "krateDerived", "krateHook":
		return id.Name
	}
	return ""
}

// derivedContextTarget returns the context name of a derived context read,
// `krateDerived(() => Ctx.useContext())`.
func derivedContextTarget(expr ast.Expr) (string, bool) {
	if derivedCallee(expr) != "krateDerived" {
		return "", false
	}
	arrow, ok := expr.(*ast.CallExpr).Args[0].(*ast.ArrowFn)
	if !ok || !arrow.Expression {
		return "", false
	}
	body := arrowBodyExpr(arrow)
	if body == nil || !isContextRead(body) {
		return "", false
	}
	return contextReadTarget(body)
}

// collectDerivedLocals returns the derived bindings declared at the top level
// of a component body, mapped to their SSR initial expression (the getter's
// value: `krateDerived(fn)()`, or the active provider's value expression for a
// derived context read).
func (b *builder) collectDerivedLocals(body []ast.Stmt) map[string]ast.Expr {
	out := map[string]ast.Expr{}
	for _, stmt := range body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, d := range vs.Decls {
			if d == nil || d.IsDestructuring || d.Name == "" || derivedCallee(d.Init) == "" {
				continue
			}
			if ctx, ok := derivedContextTarget(d.Init); ok {
				if v, active := b.contextValues[ctx]; active && v != nil {
					// Resolved now: the consumer's binding may reuse a
					// provider-side name (`const theme = useContext(Theme)`).
					initial, known := b.contextInitials[ctx]
					if !known || initial.ContainsUnknown() {
						initial = evalConstValue(v, b.localSignals, b.localProps)
					}
					out[d.Name] = exprFromValue(initial)
					continue
				}
			}
			out[d.Name] = &ast.CallExpr{Callee: d.Init}
		}
	}
	return out
}

// exprFromValue turns a build-time value back into an expression for the
// signal table; an unknown value becomes an unresolvable identifier, which
// evaluates as dynamic.
func exprFromValue(v jsvalue.Value) ast.Expr {
	if !v.ContainsUnknown() {
		if src, ok := jsvalue.JSSource(v); ok {
			if prog := parseExprAsProgram("(" + src + ")"); prog != nil && len(prog.Body) == 1 {
				if es, ok := prog.Body[0].(*ast.ExprStmt); ok && es.Expression != nil {
					return es.Expression
				}
			}
		}
	}
	return &ast.Identifier{Name: "__krate_dynamic_context__"}
}

// derivedDeclInit returns the initializer to emit for a derived binding's
// hydration declaration. A derived context read becomes a read of the
// provider's getter prop, or of the context default.
func (b *builder) derivedDeclInit(init ast.Expr) ast.Expr {
	ctx, ok := derivedContextTarget(init)
	if !ok {
		return init
	}
	var value ast.Expr
	if v, active := b.contextValues[ctx]; active && v != nil {
		value = &ast.CallExpr{Callee: &ast.MemberExpr{
			Object:   &ast.Identifier{Name: "props"},
			Property: &ast.Identifier{Name: contextPropPrefix + ctx},
		}}
	} else {
		def := jsvalue.Undefined()
		if legacy, found := b.contextDefaults[ctx]; found {
			def = jsvalue.FromLegacy(legacy)
		}
		// The literal source is emitted verbatim.
		value = &ast.Identifier{Name: "(" + valueJSLiteral(def) + ")"}
	}
	return &ast.CallExpr{
		Callee: &ast.Identifier{Name: "krateDerived"},
		Args:   []ast.Expr{&ast.ArrowFn{Expression: true, Body: []ast.Stmt{&ast.ReturnStmt{Value: value}}}},
	}
}

// readsActiveDerivedContext reports whether a body declares a derived read of
// a context that has a dynamic provider above it.
func (b *builder) readsActiveDerivedContext(body []ast.Stmt) bool {
	if len(b.contextValues) == 0 {
		return false
	}
	for _, stmt := range body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, d := range vs.Decls {
			if d == nil {
				continue
			}
			if ctx, ok := derivedContextTarget(d.Init); ok {
				if _, active := b.contextValues[ctx]; active {
					return true
				}
			}
		}
	}
	return false
}
