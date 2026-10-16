package csssignals

import "github.com/kratejs/krate/packages/compiler/ast"

// walkStmts visits every statement in a body (recursively through control flow
// and nested function bodies) and calls visitCall for each CallExpr found in
// expressions and JSX.
func walkStmts(stmts []ast.Stmt, visitCall func(*ast.CallExpr)) {
	for _, stmt := range stmts {
		walkStmt(stmt, visitCall)
	}
}

func walkStmt(stmt ast.Stmt, visitCall func(*ast.CallExpr)) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		walkExpr(s.Expression, visitCall)
	case *ast.ReturnStmt:
		walkExpr(s.Value, visitCall)
	case *ast.VarStmt:
		for _, d := range s.Decls {
			walkExpr(d.Init, visitCall)
		}
	case *ast.IfStmt:
		walkExpr(s.Test, visitCall)
		walkStmts(s.Consequent, visitCall)
		walkStmts(s.Alternate, visitCall)
	case *ast.BlockStmt:
		walkStmts(s.Body, visitCall)
	case *ast.ForStmt:
		walkStmt(s.Init, visitCall)
		walkExpr(s.Test, visitCall)
		walkExpr(s.Update, visitCall)
		walkStmts(s.Body, visitCall)
	case *ast.ForInStmt:
		walkExpr(s.Left, visitCall)
		walkExpr(s.Right, visitCall)
		walkStmts(s.Body, visitCall)
	case *ast.WhileStmt:
		walkExpr(s.Test, visitCall)
		walkStmts(s.Body, visitCall)
	case *ast.DoWhileStmt:
		walkStmts(s.Body, visitCall)
		walkExpr(s.Test, visitCall)
	case *ast.SwitchStmt:
		walkExpr(s.Discriminant, visitCall)
		for _, c := range s.Cases {
			walkExpr(c.Test, visitCall)
			walkStmts(c.Body, visitCall)
		}
	case *ast.TryStmt:
		walkStmts(s.Body, visitCall)
		if s.Catch != nil {
			walkStmts(s.Catch.Body, visitCall)
		}
		walkStmts(s.Finally, visitCall)
	case *ast.FnDecl:
		walkStmts(s.Body, visitCall)
	case *ast.ThrowStmt:
		walkExpr(s.Value, visitCall)
	}
}

func walkExpr(expr ast.Expr, visitCall func(*ast.CallExpr)) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		visitCall(e)
		walkExpr(e.Callee, visitCall)
		for _, a := range e.Args {
			walkExpr(a, visitCall)
		}
	case *ast.MemberExpr:
		walkExpr(e.Object, visitCall)
		walkExpr(e.Property, visitCall)
	case *ast.BinaryExpr:
		walkExpr(e.Left, visitCall)
		walkExpr(e.Right, visitCall)
	case *ast.UnaryExpr:
		walkExpr(e.Arg, visitCall)
	case *ast.ConditionalExpr:
		walkExpr(e.Test, visitCall)
		walkExpr(e.Consequent, visitCall)
		walkExpr(e.Alternate, visitCall)
	case *ast.ArrayExpr:
		for _, el := range e.Elements {
			walkExpr(el, visitCall)
		}
	case *ast.ObjectExpr:
		for _, p := range e.Properties {
			walkExpr(p.Value, visitCall)
		}
	case *ast.TemplateExpr:
		for _, p := range e.Parts {
			walkExpr(p, visitCall)
		}
	case *ast.ArrowFn:
		walkStmts(e.Body, visitCall)
	case *ast.AwaitExpr:
		walkExpr(e.Arg, visitCall)
	case *ast.NewExpr:
		walkExpr(e.Callee, visitCall)
		for _, a := range e.Args {
			walkExpr(a, visitCall)
		}
	case *ast.TypeAssertion:
		walkExpr(e.Expr, visitCall)
	case *ast.JSXElement:
		if e.Opening != nil {
			for _, attr := range e.Opening.Attributes {
				walkExpr(attr.Value, visitCall)
			}
		}
		for _, c := range e.Children {
			walkJSXChild(c, visitCall)
		}
	case *ast.JSXFragment:
		for _, c := range e.Children {
			walkJSXChild(c, visitCall)
		}
	}
}

func walkJSXChild(child ast.JSXChild, visitCall func(*ast.CallExpr)) {
	switch c := child.(type) {
	case *ast.JSXExprContainer:
		walkExpr(c.Expression, visitCall)
	case *ast.JSXElementChild:
		walkExpr(c.Element, visitCall)
	case *ast.JSXFragmentChild:
		walkExpr(c.Fragment, visitCall)
	}
}

// referencesCSS reports whether an expression references any of the given
// choice identifiers (getters or setters). Used by validation.
func referencesCSS(expr ast.Expr, names map[string]bool) bool {
	if expr == nil || len(names) == 0 {
		return false
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		return names[e.Name]
	case *ast.CallExpr:
		if id, ok := e.Callee.(*ast.Identifier); ok && names[id.Name] {
			return true
		}
		if referencesCSS(e.Callee, names) {
			return true
		}
		for _, a := range e.Args {
			if referencesCSS(a, names) {
				return true
			}
		}
		return false
	case *ast.MemberExpr:
		return referencesCSS(e.Object, names)
	case *ast.BinaryExpr:
		return referencesCSS(e.Left, names) || referencesCSS(e.Right, names)
	case *ast.UnaryExpr:
		return referencesCSS(e.Arg, names)
	case *ast.ConditionalExpr:
		return referencesCSS(e.Test, names) || referencesCSS(e.Consequent, names) || referencesCSS(e.Alternate, names)
	case *ast.ArrayExpr:
		for _, el := range e.Elements {
			if referencesCSS(el, names) {
				return true
			}
		}
		return false
	case *ast.ObjectExpr:
		for _, p := range e.Properties {
			if referencesCSS(p.Value, names) {
				return true
			}
		}
		return false
	case *ast.ArrowFn:
		return stmtsReferenceCSS(e.Body, names)
	case *ast.TemplateExpr:
		for _, p := range e.Parts {
			if referencesCSS(p, names) {
				return true
			}
		}
		return false
	case *ast.NewExpr:
		if referencesCSS(e.Callee, names) {
			return true
		}
		for _, a := range e.Args {
			if referencesCSS(a, names) {
				return true
			}
		}
		return false
	case *ast.AwaitExpr:
		return referencesCSS(e.Arg, names)
	case *ast.TypeAssertion:
		return referencesCSS(e.Expr, names)
	case *ast.JSXElement:
		if e.Opening != nil {
			for _, attr := range e.Opening.Attributes {
				if referencesCSS(attr.Value, names) {
					return true
				}
			}
		}
		for _, c := range e.Children {
			if jsxChildReferencesCSS(c, names) {
				return true
			}
		}
		return false
	case *ast.JSXFragment:
		for _, c := range e.Children {
			if jsxChildReferencesCSS(c, names) {
				return true
			}
		}
		return false
	}
	return false
}

func jsxChildReferencesCSS(child ast.JSXChild, names map[string]bool) bool {
	switch c := child.(type) {
	case *ast.JSXExprContainer:
		return referencesCSS(c.Expression, names)
	case *ast.JSXElementChild:
		return referencesCSS(c.Element, names)
	case *ast.JSXFragmentChild:
		return referencesCSS(c.Fragment, names)
	}
	return false
}

func stmtsReferenceCSS(stmts []ast.Stmt, names map[string]bool) bool {
	for _, s := range stmts {
		if stmtReferenceCSS(s, names) {
			return true
		}
	}
	return false
}

func stmtReferenceCSS(stmt ast.Stmt, names map[string]bool) bool {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		return referencesCSS(s.Expression, names)
	case *ast.ReturnStmt:
		return referencesCSS(s.Value, names)
	case *ast.VarStmt:
		for _, d := range s.Decls {
			if referencesCSS(d.Init, names) {
				return true
			}
		}
	case *ast.IfStmt:
		return referencesCSS(s.Test, names) || stmtsReferenceCSS(s.Consequent, names) || stmtsReferenceCSS(s.Alternate, names)
	case *ast.BlockStmt:
		return stmtsReferenceCSS(s.Body, names)
	case *ast.ForStmt:
		return referencesCSS(s.Test, names) || referencesCSS(s.Update, names) || stmtsReferenceCSS(s.Body, names)
	case *ast.ForInStmt:
		return referencesCSS(s.Right, names) || stmtsReferenceCSS(s.Body, names)
	case *ast.WhileStmt:
		return referencesCSS(s.Test, names) || stmtsReferenceCSS(s.Body, names)
	case *ast.DoWhileStmt:
		return stmtsReferenceCSS(s.Body, names)
	case *ast.SwitchStmt:
		if referencesCSS(s.Discriminant, names) {
			return true
		}
		for _, c := range s.Cases {
			if stmtsReferenceCSS(c.Body, names) {
				return true
			}
		}
	case *ast.TryStmt:
		return stmtsReferenceCSS(s.Body, names)
	case *ast.FnDecl:
		return stmtsReferenceCSS(s.Body, names)
	}
	return false
}
