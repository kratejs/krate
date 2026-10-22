package irtree

import (
	"github.com/kratejs/krate/packages/compiler/ast"
)

// Early-return lowering.
//
// A component body may choose what to render with guard statements:
//
//	if (loading()) return <Spinner/>;
//	else if (!data) return null;
//	return <Main/>;
//
// The slot builder renders a single expression, so the chain is lowered into
// nested conditionals, `loading() ? <Spinner/> : !data ? null : <Main/>`,
// which buildSlotNodes folds at build time when the test is static or turns
// into a reactive ConditionalSlot when it reads signals.
//
// Statements at the component top level that are not guards (locals,
// effects, handlers) keep running in source order through the normal local
// and hydration passes, so they are skipped here. Statements inside a guard
// branch, before its return, would only run on that path; nothing else
// executes them, so such a branch cannot be lowered and is reported instead.
// The same applies to returns nested in loops, try blocks or labeled
// statements, and to switch cases that do more than return.

// returnIssue describes a render-return shape that cannot be lowered.
type returnIssue struct {
	pos  ast.Pos
	msg  string
	hint string
}

// RenderReturn is findReturnStmt for other packages: the SSR evaluator uses it
// so prop-driven components resolve the same lowered return (including
// switch statements) as client components.
func RenderReturn(body []ast.Stmt) *ast.ReturnStmt { return findReturnStmt(body) }

// findReturnStmt returns the statement whose value a component renders. A
// body whose render is chosen by early-return guards yields a synthesized
// return of the lowered conditional chain; otherwise (and when the guards
// cannot be lowered) it is the first top-level return.
func findReturnStmt(body []ast.Stmt) *ast.ReturnStmt {
	ret, _ := lowerRenderReturn(body)
	return ret
}

// lowerRenderReturn lowers a component body's render returns. It returns the
// render return (possibly synthesized) and, when a guard shape cannot be
// lowered, the issue; the return is then the first top-level return so the
// caller still has something to render.
func lowerRenderReturn(body []ast.Stmt) (*ast.ReturnStmt, *returnIssue) {
	var first *ast.ReturnStmt
	guards := false
	for _, stmt := range body {
		if ret, ok := stmt.(*ast.ReturnStmt); ok {
			first = ret
			break
		}
		if stmt != nil && stmtHasRenderReturn(stmt) {
			guards = true
		}
	}
	if !guards {
		return first, nil
	}
	l := &returnLowerer{}
	expr, falls := l.seq(body, true)
	if l.issue != nil {
		return first, l.issue
	}
	if falls {
		// Some path reaches the end of the body without returning: React
		// renders nothing for it.
		expr = l.fill(expr, nullExpr())
	}
	pos := ast.Pos{}
	for _, stmt := range body {
		if stmt != nil && stmtHasRenderReturn(stmt) {
			pos = stmt.Pos()
			break
		}
	}
	return &ast.ReturnStmt{Position: pos, Value: expr}, nil
}

// fallthroughExpr marks a path that reaches the end of a statement list
// without returning; the enclosing sequence fills it with what follows.
type fallthroughExpr struct{ ast.Identifier }

func nullExpr() ast.Expr { return &ast.Literal{Kind: ast.NullLit, Value: "null"} }

type returnLowerer struct {
	issue *returnIssue
	// near is the closest enclosing statement position, used when a node
	// carries no position of its own.
	near ast.Pos
}

func (l *returnLowerer) fail(pos ast.Pos, msg, hint string) {
	if pos.Line == 0 {
		pos = l.near
	}
	if l.issue == nil {
		l.issue = &returnIssue{pos: pos, msg: msg, hint: hint}
	}
}

// seq lowers a statement list to the expression it returns. falls reports
// whether some path reaches the end of the list, in which case the result
// contains fallthrough markers (or is nil when every path falls through).
// top is true for the component's own body, where non-guard statements run
// through the regular passes and are skipped.
func (l *returnLowerer) seq(stmts []ast.Stmt, top bool) (ast.Expr, bool) {
	var result ast.Expr // nil: nothing returned yet on the pending path
	pending := true     // the path being built is still open (no return yet)
	for _, stmt := range stmts {
		if l.issue != nil || !pending {
			break
		}
		if stmt == nil {
			continue
		}
		var expr ast.Expr
		var falls bool
		switch s := stmt.(type) {
		case *ast.ReturnStmt:
			expr, falls = s.Value, false
			if expr == nil {
				expr = nullExpr()
			}
		case *ast.IfStmt:
			if !stmtHasRenderReturn(s) {
				if !top {
					l.branchStmt(s)
				}
				continue
			}
			saved := l.near
			if s.Position.Line > 0 {
				l.near = s.Position
			}
			c, cf := l.seq(s.Consequent, false)
			a, af := l.seq(s.Alternate, false)
			l.near = saved
			falls = cf || af
			if c == nil {
				c = &fallthroughExpr{}
			}
			if a == nil {
				a = &fallthroughExpr{}
			}
			expr = &ast.ConditionalExpr{Position: s.Position, Test: s.Test, Consequent: c, Alternate: a}
		case *ast.BlockStmt:
			if !stmtHasRenderReturn(s) {
				if !top {
					l.branchStmt(s)
				}
				continue
			}
			expr, falls = l.seq(s.Body, top)
			if expr == nil {
				continue
			}
		case *ast.SwitchStmt:
			if !stmtHasRenderReturn(s) {
				if !top {
					l.branchStmt(s)
				}
				continue
			}
			expr, falls = l.switchStmt(s)
		case *ast.FnDecl, *ast.ClassDecl:
			// Declarations are hoisted; their bodies are not render paths.
			continue
		default:
			if stmtHasRenderReturn(stmt) {
				l.fail(stmt.Pos(), "a component render return inside a loop, try block or labeled statement cannot be lowered",
					"move the return out of the loop: compute the value first (for example with .find() or a variable), then return JSX from the component body")
				return nil, false
			}
			if !top {
				l.branchStmt(stmt)
			}
			continue
		}
		if result == nil {
			result = expr
		} else {
			result = l.fill(result, expr)
		}
		pending = falls
	}
	if result == nil {
		return nil, true
	}
	return result, pending
}

// branchStmt reports a statement that sits in a guard branch before its
// return: only that path would run it, and the lowered conditional cannot.
func (l *returnLowerer) branchStmt(stmt ast.Stmt) {
	l.fail(stmt.Pos(), "a statement inside an early-return branch cannot be lowered: only the render value of a guard is compiled, so the statement would never run",
		"move the statement above the if (or into an effect/handler), so the branch only contains `return <JSX/>`, e.g. `if (cond) return <Spinner />;`")
}

// switchStmt lowers `switch (d) { case a: return A; case b: case c: return B;
// default: return D; }` into `d === a ? A : (d === b || d === c) ? B : D`.
func (l *returnLowerer) switchStmt(s *ast.SwitchStmt) (ast.Expr, bool) {
	type arm struct {
		test ast.Expr
		val  ast.Expr // nil: falls out of the switch (break)
	}
	var arms []arm
	var defaultVal ast.Expr // nil: no default, or `default: break;`
	var pendingTests []ast.Expr
	for _, c := range s.Cases {
		if c == nil {
			continue
		}
		body := c.Body
		if len(body) == 1 {
			if blk, ok := body[0].(*ast.BlockStmt); ok {
				body = blk.Body
			}
		}
		if c.Test != nil {
			pendingTests = append(pendingTests, &ast.BinaryExpr{Position: c.Position, Left: s.Discriminant, Op: "===", Right: c.Test})
		}
		if len(body) == 0 {
			// Empty case body: falls through into the next case.
			if c.Test == nil {
				l.fail(c.Position, "a `default:` case that falls through into another case cannot be lowered into a render expression",
					"move `default:` to the end of the switch and give it its own `return <JSX/>;`")
				return nil, false
			}
			continue
		}
		var val ast.Expr
		breaks := false
		switch b := body[0].(type) {
		case *ast.ReturnStmt:
			val = b.Value
			if val == nil {
				val = nullExpr()
			}
		case *ast.BreakStmt:
			if b.Label == "" {
				breaks = true
			}
		}
		if len(body) != 1 || (val == nil && !breaks) {
			l.fail(c.Position, "a switch case that does more than return JSX cannot be lowered into a render expression",
				"make every case a single `return <JSX/>;` (or `break;`), and compute anything else above the switch")
			return nil, false
		}
		if c.Test == nil {
			defaultVal = val
			// Tests merged into the default arm (`case x: default:`) are
			// covered by the default value.
			pendingTests = nil
			continue
		}
		test := pendingTests[0]
		for _, t := range pendingTests[1:] {
			test = &ast.BinaryExpr{Position: c.Position, Left: test, Op: "||", Right: t}
		}
		pendingTests = nil
		arms = append(arms, arm{test: test, val: val})
	}
	falls := false
	tail := defaultVal
	if tail == nil {
		tail = &fallthroughExpr{}
		falls = true
	}
	for i := len(arms) - 1; i >= 0; i-- {
		val := arms[i].val
		if val == nil {
			val = &fallthroughExpr{}
			falls = true
		}
		tail = &ast.ConditionalExpr{Position: s.Position, Test: arms[i].test, Consequent: val, Alternate: tail}
	}
	return tail, falls
}

// fill replaces every fallthrough marker in e with next.
func (l *returnLowerer) fill(e, next ast.Expr) ast.Expr {
	switch x := e.(type) {
	case *fallthroughExpr:
		return next
	case *ast.ConditionalExpr:
		if !containsFallthrough(x) {
			return x
		}
		return &ast.ConditionalExpr{Position: x.Position, Test: x.Test, Consequent: l.fill(x.Consequent, next), Alternate: l.fill(x.Alternate, next)}
	}
	return e
}

func containsFallthrough(e ast.Expr) bool {
	switch x := e.(type) {
	case *fallthroughExpr:
		return true
	case *ast.ConditionalExpr:
		return containsFallthrough(x.Consequent) || containsFallthrough(x.Alternate)
	}
	return false
}

// stmtHasRenderReturn reports whether stmt contains a return that belongs to
// the enclosing function (returns inside nested functions do not count).
func stmtHasRenderReturn(stmt ast.Stmt) bool {
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BlockStmt:
		return stmtsHaveRenderReturn(s.Body)
	case *ast.IfStmt:
		return stmtsHaveRenderReturn(s.Consequent) || stmtsHaveRenderReturn(s.Alternate)
	case *ast.ForStmt:
		return stmtsHaveRenderReturn(s.Body)
	case *ast.ForInStmt:
		return stmtsHaveRenderReturn(s.Body)
	case *ast.WhileStmt:
		return stmtsHaveRenderReturn(s.Body)
	case *ast.DoWhileStmt:
		return stmtsHaveRenderReturn(s.Body)
	case *ast.SwitchStmt:
		for _, c := range s.Cases {
			if c != nil && stmtsHaveRenderReturn(c.Body) {
				return true
			}
		}
	case *ast.TryStmt:
		if stmtsHaveRenderReturn(s.Body) || stmtsHaveRenderReturn(s.Finally) {
			return true
		}
		if s.Catch != nil && stmtsHaveRenderReturn(s.Catch.Body) {
			return true
		}
	}
	return false
}

func stmtsHaveRenderReturn(stmts []ast.Stmt) bool {
	for _, s := range stmts {
		if s != nil && stmtHasRenderReturn(s) {
			return true
		}
	}
	return false
}
