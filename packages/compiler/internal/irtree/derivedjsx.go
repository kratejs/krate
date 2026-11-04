package irtree

import (
	"github.com/kratejs/krate/packages/compiler/ast"
)

// JSX held by a derived render value.
//
// `const badge = on ? <b/> : <i/>` and `let content; switch (tab) { case "a":
// content = <A/>; ... }` are lowered by the bundler to krateDerived getters.
// A `{badge}` child of such a value is lowered here to the equivalent
// conditional expression so it renders and updates like an inline ternary
// (server markup included) instead of as a text read.

// derivedJSXExpr returns the single expression a derived getter evaluates to,
// folding if/switch statements that assign a local then return it into
// conditionals. ok is false when the body is not of that shape.
func derivedJSXExpr(arrow *ast.ArrowFn) (ast.Expr, bool) {
	if arrow == nil || len(arrow.Params) != 0 {
		return nil, false
	}
	env := map[string]ast.Expr{}
	return foldReturnBlock(arrow.Body, env, 0)
}

const maxFoldDepth = 16

func cloneEnv(env map[string]ast.Expr) map[string]ast.Expr {
	out := make(map[string]ast.Expr, len(env))
	for k, v := range env {
		out[k] = v
	}
	return out
}

func substEnv(e ast.Expr, env map[string]ast.Expr) ast.Expr {
	for name, val := range env {
		s := &listParamSubstituter{param: name, elem: val}
		e = s.sub(e)
	}
	return e
}

// foldReturnBlock symbolically runs stmts and returns the expression of the
// value the block returns.
func foldReturnBlock(stmts []ast.Stmt, env map[string]ast.Expr, depth int) (ast.Expr, bool) {
	if depth > maxFoldDepth {
		return nil, false
	}
	for i, st := range stmts {
		rest := stmts[i+1:]
		switch s := st.(type) {
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if d == nil || d.IsDestructuring || d.Name == "" {
					return nil, false
				}
				if d.Init == nil {
					env[d.Name] = undefinedLit()
				} else {
					env[d.Name] = substEnv(d.Init, env)
				}
			}
		case *ast.ExprStmt:
			be, ok := s.Expression.(*ast.BinaryExpr)
			if !ok || be.Op != "=" {
				return nil, false
			}
			id, ok := be.Left.(*ast.Identifier)
			if !ok {
				return nil, false
			}
			if _, known := env[id.Name]; !known {
				return nil, false
			}
			env[id.Name] = substEnv(be.Right, env)
		case *ast.ReturnStmt:
			if s.Value == nil {
				return undefinedLit(), true
			}
			return substEnv(s.Value, env), true
		case *ast.BlockStmt:
			joined := append(append([]ast.Stmt{}, s.Body...), rest...)
			return foldReturnBlock(joined, env, depth+1)
		case *ast.IfStmt:
			thenStmts := append(append([]ast.Stmt{}, s.Consequent...), rest...)
			elseStmts := append(append([]ast.Stmt{}, s.Alternate...), rest...)
			cons, ok := foldReturnBlock(thenStmts, cloneEnv(env), depth+1)
			if !ok {
				return nil, false
			}
			alt, ok := foldReturnBlock(elseStmts, cloneEnv(env), depth+1)
			if !ok {
				return nil, false
			}
			return &ast.ConditionalExpr{Test: substEnv(s.Test, env), Consequent: cons, Alternate: alt}, true
		case *ast.SwitchStmt:
			return foldSwitch(s, rest, env, depth)
		default:
			return nil, false
		}
	}
	return undefinedLit(), true
}

// foldSwitch lowers a switch whose cases end in break/return (empty cases fall
// through to the next) to a conditional chain.
func foldSwitch(s *ast.SwitchStmt, rest []ast.Stmt, env map[string]ast.Expr, depth int) (ast.Expr, bool) {
	disc := substEnv(s.Discriminant, env)
	type arm struct {
		tests []ast.Expr
		body  []ast.Stmt
		dflt  bool
	}
	var arms []arm
	var pending []ast.Expr
	pendingDefault := false
	for ci, c := range s.Cases {
		if c == nil {
			return nil, false
		}
		if c.Test == nil {
			pendingDefault = true
		} else {
			pending = append(pending, c.Test)
		}
		if len(c.Body) == 0 {
			continue
		}
		body := c.Body
		if br, ok := body[len(body)-1].(*ast.BreakStmt); ok && br.Label == "" {
			body = body[:len(body)-1]
		} else if _, ok := body[len(body)-1].(*ast.ReturnStmt); !ok && ci != len(s.Cases)-1 {
			// Falls through into the next case with statements.
			return nil, false
		}
		arms = append(arms, arm{tests: pending, body: body, dflt: pendingDefault})
		pending, pendingDefault = nil, false
	}
	if len(pending) > 0 || pendingDefault {
		arms = append(arms, arm{tests: pending, dflt: pendingDefault})
	}
	// The default arm is the final else; with none, the statements after the
	// switch run.
	var result ast.Expr
	var defaultArm *arm
	for i := range arms {
		if arms[i].dflt {
			defaultArm = &arms[i]
		}
	}
	tail := func(body []ast.Stmt) (ast.Expr, bool) {
		joined := append(append([]ast.Stmt{}, body...), rest...)
		return foldReturnBlock(joined, cloneEnv(env), depth+1)
	}
	if defaultArm != nil {
		r, ok := tail(defaultArm.body)
		if !ok {
			return nil, false
		}
		result = r
	} else {
		r, ok := tail(nil)
		if !ok {
			return nil, false
		}
		result = r
	}
	for i := len(arms) - 1; i >= 0; i-- {
		a := arms[i]
		if a.dflt || len(a.tests) == 0 {
			continue
		}
		var test ast.Expr
		for _, t := range a.tests {
			eq := &ast.BinaryExpr{Left: disc, Op: "===", Right: substEnv(t, env)}
			if test == nil {
				test = eq
			} else {
				test = &ast.BinaryExpr{Left: test, Op: "||", Right: eq}
			}
		}
		body, ok := tail(a.body)
		if !ok {
			return nil, false
		}
		result = &ast.ConditionalExpr{Test: test, Consequent: body, Alternate: result}
	}
	return result, true
}

// derivedJSXChild returns the inlined expression for a `{name}` child that
// reads a derived value holding JSX.
func (b *builder) derivedJSXChild(name string) (ast.Expr, bool) {
	if !b.signalHoldsJSX(name) {
		return nil, false
	}
	call := b.sigMap()[name].(*ast.CallExpr)
	inner, ok := call.Callee.(*ast.CallExpr)
	if !ok || len(inner.Args) < 1 {
		return nil, false
	}
	arrow, ok := inner.Args[0].(*ast.ArrowFn)
	if !ok {
		return nil, false
	}
	return derivedJSXExpr(arrow)
}
