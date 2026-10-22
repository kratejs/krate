package annotator

import "github.com/kratejs/krate/packages/compiler/ast"

// Render-local inlining.
//
// React re-runs render() on every update, so a local such as
// `const { count } = this.state` or `const label = 'n=' + this.state.n` is
// recomputed each time. A lowered component body runs once: the build folds
// such a local to its initial value and the hydrated bindings never see it
// change. inlineRenderLocals substitutes these instance-derived consts into
// their uses, so every binding re-evaluates the expression from the state
// signal and the props getters.
func inlineRenderLocals(body []ast.Stmt) []ast.Stmt {
	nested := nestedBindings(body)
	tags := jsxTagNames(body)
	subst := map[string]ast.Expr{}
	out := make([]ast.Stmt, 0, len(body))
	for _, stmt := range body {
		stmt = substituteIdents(stmt, subst)
		vs, ok := stmt.(*ast.VarStmt)
		if !ok || vs.Kind != ast.VarConst {
			out = append(out, stmt)
			continue
		}
		pending := map[string]ast.Expr{}
		inlinable := true
		for _, d := range vs.Decls {
			if d == nil || d.Init == nil || !readsInstance(d.Init, subst) {
				inlinable = false
				break
			}
			binds, ok := declSubstitutions(d)
			if !ok {
				inlinable = false
				break
			}
			for name, e := range binds {
				if nested[name] || tags[name] {
					inlinable = false
				}
				pending[name] = e
			}
		}
		if !inlinable {
			out = append(out, stmt)
			continue
		}
		for name, e := range pending {
			subst[name] = e
		}
	}
	return out
}

// declSubstitutions returns, for `const x = init` or a flat object pattern
// `const { a, b: c, d = 1 } = init`, the expression each bound name stands for.
func declSubstitutions(d *ast.VarDecl) (map[string]ast.Expr, bool) {
	init := d.Init
	if !d.IsDestructuring {
		if d.Name == "" {
			return nil, false
		}
		return map[string]ast.Expr{d.Name: init}, true
	}
	obj, ok := d.Target.(*ast.ObjectExpr)
	if !ok || d.RestName != "" {
		return nil, false
	}
	out := map[string]ast.Expr{}
	for _, p := range obj.Properties {
		if p == nil || p.Spread || p.Computed || p.Key == "" {
			return nil, false
		}
		read := &ast.MemberExpr{Object: init, Property: &ast.Identifier{Name: p.Key}}
		switch v := p.Value.(type) {
		case *ast.Identifier:
			out[v.Name] = read
		case *ast.BinaryExpr:
			target, ok := v.Left.(*ast.Identifier)
			if !ok || v.Op != "=" {
				return nil, false
			}
			out[target.Name] = &ast.ConditionalExpr{
				Test:       &ast.BinaryExpr{Left: read, Op: "===", Right: &ast.Identifier{Name: "undefined"}},
				Consequent: v.Right,
				Alternate:  read,
			}
		default:
			return nil, false
		}
	}
	return out, len(out) > 0
}

// readsInstance reports whether e reads `this` (state, props, context, a
// member) or a local already inlined from it.
func readsInstance(e ast.Expr, subst map[string]ast.Expr) bool {
	found := false
	inspectExpr(e, func(x ast.Expr) bool {
		if found || isOwnThisFunction(x) {
			return false
		}
		switch v := x.(type) {
		case *ast.ThisExpr:
			found = true
		case *ast.Identifier:
			if _, ok := subst[v.Name]; ok {
				found = true
			}
		case *ast.ArrowFn:
			// A function value is created once; inlining it would change its
			// identity on every read.
			return false
		}
		return !found
	})
	return found
}

// substituteIdents replaces identifiers named in subst within stmt.
func substituteIdents(stmt ast.Stmt, subst map[string]ast.Expr) ast.Stmt {
	if len(subst) == 0 {
		return stmt
	}
	return walkStmt(stmt, func(e ast.Expr, recurse func(ast.Expr) ast.Expr) ast.Expr {
		if id, ok := e.(*ast.Identifier); ok {
			if repl, ok := subst[id.Name]; ok {
				return repl
			}
			return e
		}
		return recurse(e)
	})
}

// nestedBindings returns the names bound anywhere below the top level of body
// (parameters, inner declarations), which an inlined name must not collide
// with.
func nestedBindings(body []ast.Stmt) map[string]bool {
	stripped := make([]ast.Stmt, 0, len(body))
	for _, stmt := range body {
		if vs, ok := stmt.(*ast.VarStmt); ok {
			for _, d := range vs.Decls {
				if d != nil && d.Init != nil {
					stripped = append(stripped, &ast.ExprStmt{Expression: d.Init})
				}
			}
			continue
		}
		stripped = append(stripped, stmt)
	}
	return boundNames(stripped)
}

// jsxTagNames collects the JSX element names used in body (a local used as a
// tag, `<Tag/>`, cannot be substituted).
func jsxTagNames(body []ast.Stmt) map[string]bool {
	out := map[string]bool{}
	inspectStmts(body, func(e ast.Expr) bool {
		if el, ok := e.(*ast.JSXElement); ok && el.Opening != nil {
			out[el.Opening.Name] = true
		}
		return true
	})
	return out
}
