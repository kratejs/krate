package irtree

import "github.com/kratejs/krate/packages/compiler/ast"

// Render-time throw guards.
//
// React re-runs render, so `if (count === 5) throw new Error(...)` in a
// component body throws as soon as the state reaches 5, and the nearest error
// boundary catches it. A Krate component body runs once; replaying the guard
// at hydration only checks the initial state. A guard that reads signals is
// therefore wrapped in an effect, which re-checks it whenever those signals
// change and reports the error through the owner tree (runtime boundary.ts).

// isThrowGuard reports whether an if statement throws in one of its branches
// (outside nested functions) and never returns.
func isThrowGuard(s *ast.IfStmt) bool {
	return stmtsThrow(s.Consequent) || stmtsThrow(s.Alternate)
}

func stmtsThrow(stmts []ast.Stmt) bool {
	for _, st := range stmts {
		switch v := st.(type) {
		case *ast.ThrowStmt:
			return true
		case *ast.BlockStmt:
			if stmtsThrow(v.Body) {
				return true
			}
		case *ast.IfStmt:
			if stmtsThrow(v.Consequent) || stmtsThrow(v.Alternate) {
				return true
			}
		}
	}
	return false
}

// throwGuardJS wraps a replayed guard in an effect.
func throwGuardJS(js string) string {
	return "createEffect(()=>{" + js + "})"
}
