package irtree

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

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

// throwGuardWithDeps prefixes the guard with the top-level locals (declared
// before it, at index idx of body) it reads, transitively, so the effect
// recomputes them: `const max = Math.max(...props.points); if (!isFinite(max))
// throw ...`. live reports whether any of them reads props or signals.
func (b *builder) throwGuardWithDeps(body []ast.Stmt, idx int, guard string) (string, bool) {
	type decl struct {
		name string
		init ast.Expr
	}
	var decls []decl
	for _, st := range body[:idx] {
		vs, ok := st.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, d := range vs.Decls {
			if d != nil && !d.IsDestructuring && d.Name != "" && d.Init != nil {
				decls = append(decls, decl{d.Name, d.Init})
			}
		}
	}
	need := map[string]bool{}
	scanJSIdents(guard, func(n string) { need[n] = true })
	var picked []string
	live := false
	for i := len(decls) - 1; i >= 0; i-- {
		d := decls[i]
		if !need[d.name] {
			continue
		}
		js := generateExprJS(d.init, b.sigMap())
		scanJSIdents(js, func(n string) { need[n] = true })
		if referencesProps(d.init) || b.referencesSignal(d.init) {
			live = true
		}
		picked = append(picked, "var "+d.name+"="+js+";")
	}
	var sb strings.Builder
	for i := len(picked) - 1; i >= 0; i-- {
		sb.WriteString(picked[i])
	}
	sb.WriteString(guard)
	return sb.String(), live
}
