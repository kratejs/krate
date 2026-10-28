package build

import "github.com/kratejs/krate/packages/compiler/ast"

// walkProgramExprs calls visit for every expression reachable from the
// program's statements, including function bodies, JSX attributes and
// children. visit returns false to stop the whole walk.
func walkProgramExprs(prog *ast.Program, visit func(ast.Expr) bool) {
	if prog == nil {
		return
	}
	stopped := false

	var walkStmt func([]ast.Stmt)
	var walkExpr func(ast.Expr)
	var walkJSXChild func(ast.JSXChild)

	walkExpr = func(e ast.Expr) {
		if stopped || e == nil {
			return
		}
		if !visit(e) {
			stopped = true
			return
		}
		switch v := e.(type) {
		case *ast.CallExpr:
			walkExpr(v.Callee)
			for _, a := range v.Args {
				walkExpr(a)
			}
		case *ast.MemberExpr:
			walkExpr(v.Object)
			walkExpr(v.Property)
		case *ast.BinaryExpr:
			walkExpr(v.Left)
			walkExpr(v.Right)
		case *ast.UnaryExpr:
			walkExpr(v.Arg)
		case *ast.ConditionalExpr:
			walkExpr(v.Test)
			walkExpr(v.Consequent)
			walkExpr(v.Alternate)
		case *ast.TemplateExpr:
			for _, p := range v.Parts {
				walkExpr(p)
			}
		case *ast.ArrowFn:
			walkStmt(v.Body)
		case *ast.AwaitExpr:
			walkExpr(v.Arg)
		case *ast.DynamicImport:
			walkExpr(v.Arg)
		case *ast.ParenExpr:
			walkExpr(v.Expr)
		case *ast.TypeAssertion:
			walkExpr(v.Expr)
		case *ast.NewExpr:
			walkExpr(v.Callee)
			for _, a := range v.Args {
				walkExpr(a)
			}
		case *ast.ObjectExpr:
			for _, p := range v.Properties {
				if p.Value != nil {
					walkExpr(p.Value)
				}
			}
		case *ast.ArrayExpr:
			for _, el := range v.Elements {
				walkExpr(el)
			}
		case *ast.JSXElement:
			if v.Opening != nil {
				for _, attr := range v.Opening.Attributes {
					if attr.Value != nil {
						walkExpr(attr.Value)
					}
				}
			}
			for _, c := range v.Children {
				walkJSXChild(c)
			}
		case *ast.JSXFragment:
			for _, c := range v.Children {
				walkJSXChild(c)
			}
		}
	}

	walkJSXChild = func(c ast.JSXChild) {
		if stopped {
			return
		}
		switch ch := c.(type) {
		case *ast.JSXExprContainer:
			walkExpr(ch.Expression)
		case *ast.JSXElementChild:
			walkExpr(ch.Element)
		case *ast.JSXFragmentChild:
			walkExpr(ch.Fragment)
		}
	}

	walkStmt = func(stmts []ast.Stmt) {
		for _, stmt := range stmts {
			if stopped {
				return
			}
			switch s := stmt.(type) {
			case *ast.ExprStmt:
				walkExpr(s.Expression)
			case *ast.VarStmt:
				for _, d := range s.Decls {
					if d.Init != nil {
						walkExpr(d.Init)
					}
				}
			case *ast.FnDecl:
				walkStmt(s.Body)
			case *ast.ReturnStmt:
				if s.Value != nil {
					walkExpr(s.Value)
				}
			case *ast.BlockStmt:
				walkStmt(s.Body)
			case *ast.IfStmt:
				walkExpr(s.Test)
				walkStmt(s.Consequent)
				walkStmt(s.Alternate)
			case *ast.ForStmt:
				if s.Init != nil {
					walkStmt([]ast.Stmt{s.Init})
				}
				if s.Test != nil {
					walkExpr(s.Test)
				}
				walkStmt(s.Body)
			case *ast.ForInStmt:
				walkExpr(s.Right)
				walkStmt(s.Body)
			case *ast.WhileStmt:
				walkExpr(s.Test)
				walkStmt(s.Body)
			case *ast.DoWhileStmt:
				walkStmt(s.Body)
				walkExpr(s.Test)
			case *ast.SwitchStmt:
				walkExpr(s.Discriminant)
				for _, c := range s.Cases {
					if c.Test != nil {
						walkExpr(c.Test)
					}
					walkStmt(c.Body)
				}
			case *ast.TryStmt:
				walkStmt(s.Body)
				if s.Catch != nil {
					walkStmt(s.Catch.Body)
				}
				walkStmt(s.Finally)
			case *ast.ThrowStmt:
				walkExpr(s.Value)
			case *ast.ExportStmt:
				if s.Declaration != nil {
					walkStmt([]ast.Stmt{s.Declaration})
				}
			}
		}
	}

	walkStmt(prog.Body)
}
