package bundler

import "github.com/kratejs/krate/packages/compiler/ast"

// astVisitor is called for every statement and expression node of a walk.
// inFn reports whether the node is inside a function nested in the walk
// root. Returning false skips the node's children.
type astVisitor func(n any, inFn bool) bool

func visitStmts(stmts []ast.Stmt, inFn bool, fn astVisitor) {
	for _, s := range stmts {
		visitStmt(s, inFn, fn)
	}
}

func visitStmt(stmt ast.Stmt, inFn bool, fn astVisitor) {
	if stmt == nil || !fn(stmt, inFn) {
		return
	}
	switch s := stmt.(type) {
	case *ast.VarStmt:
		for _, d := range s.Decls {
			if d != nil {
				visitExpr(d.Target, inFn, fn)
				visitExpr(d.Init, inFn, fn)
			}
		}
	case *ast.ExprStmt:
		visitExpr(s.Expression, inFn, fn)
	case *ast.ReturnStmt:
		visitExpr(s.Value, inFn, fn)
	case *ast.ThrowStmt:
		visitExpr(s.Value, inFn, fn)
	case *ast.FnDecl:
		visitParams(s.Params, fn)
		visitStmts(s.Body, true, fn)
	case *ast.ClassDecl:
		visitClass(s, fn, inFn)
	case *ast.ExportStmt:
		visitStmt(s.Declaration, inFn, fn)
	case *ast.IfStmt:
		visitExpr(s.Test, inFn, fn)
		visitStmts(s.Consequent, inFn, fn)
		visitStmts(s.Alternate, inFn, fn)
	case *ast.BlockStmt:
		visitStmts(s.Body, inFn, fn)
	case *ast.ForStmt:
		visitStmt(s.Init, inFn, fn)
		visitExpr(s.Test, inFn, fn)
		visitExpr(s.Update, inFn, fn)
		visitStmts(s.Body, inFn, fn)
	case *ast.ForInStmt:
		visitExpr(s.Left, inFn, fn)
		visitExpr(s.Right, inFn, fn)
		visitStmts(s.Body, inFn, fn)
	case *ast.WhileStmt:
		visitExpr(s.Test, inFn, fn)
		visitStmts(s.Body, inFn, fn)
	case *ast.DoWhileStmt:
		visitStmts(s.Body, inFn, fn)
		visitExpr(s.Test, inFn, fn)
	case *ast.SwitchStmt:
		visitExpr(s.Discriminant, inFn, fn)
		for _, c := range s.Cases {
			if c != nil {
				visitExpr(c.Test, inFn, fn)
				visitStmts(c.Body, inFn, fn)
			}
		}
	case *ast.TryStmt:
		visitStmts(s.Body, inFn, fn)
		if s.Catch != nil {
			visitStmts(s.Catch.Body, inFn, fn)
		}
		visitStmts(s.Finally, inFn, fn)
	case *ast.LabeledStmt:
		visitStmt(s.Body, inFn, fn)
	case *ast.WithStmt:
		visitExpr(s.Object, inFn, fn)
		visitStmts(s.Body, inFn, fn)
	}
}

func visitParams(params []*ast.Param, fn astVisitor) {
	for _, p := range params {
		if p != nil {
			visitExpr(p.Default, true, fn)
		}
	}
}

func visitClass(c *ast.ClassDecl, fn astVisitor, inFn bool) {
	if c == nil {
		return
	}
	visitExpr(c.SuperClass, inFn, fn)
	for _, m := range c.Members {
		if m == nil {
			continue
		}
		visitExpr(m.KeyExpr, inFn, fn)
		visitParams(m.Params, fn)
		visitStmts(m.Body, true, fn)
		visitExpr(m.Init, true, fn)
	}
}

func visitExpr(expr ast.Expr, inFn bool, fn astVisitor) {
	if expr == nil || !fn(expr, inFn) {
		return
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		visitExpr(e.Callee, inFn, fn)
		for _, a := range e.Args {
			visitExpr(a, inFn, fn)
		}
	case *ast.NewExpr:
		visitExpr(e.Callee, inFn, fn)
		for _, a := range e.Args {
			visitExpr(a, inFn, fn)
		}
	case *ast.MemberExpr:
		visitExpr(e.Object, inFn, fn)
		if e.Computed {
			visitExpr(e.Property, inFn, fn)
		}
	case *ast.BinaryExpr:
		visitExpr(e.Left, inFn, fn)
		visitExpr(e.Right, inFn, fn)
	case *ast.UnaryExpr:
		visitExpr(e.Arg, inFn, fn)
	case *ast.ConditionalExpr:
		visitExpr(e.Test, inFn, fn)
		visitExpr(e.Consequent, inFn, fn)
		visitExpr(e.Alternate, inFn, fn)
	case *ast.TypeAssertion:
		visitExpr(e.Expr, inFn, fn)
	case *ast.ParenExpr:
		visitExpr(e.Expr, inFn, fn)
	case *ast.AwaitExpr:
		visitExpr(e.Arg, inFn, fn)
	case *ast.DynamicImport:
		visitExpr(e.Arg, inFn, fn)
		visitExpr(e.Options, inFn, fn)
	case *ast.ArrowFn:
		visitParams(e.Params, fn)
		visitStmts(e.Body, true, fn)
	case *ast.ObjectExpr:
		for _, p := range e.Properties {
			if p == nil {
				continue
			}
			if p.Computed {
				visitExpr(p.KeyExpr, inFn, fn)
			}
			visitExpr(p.Value, inFn, fn)
		}
	case *ast.ArrayExpr:
		for _, el := range e.Elements {
			visitExpr(el, inFn, fn)
		}
	case *ast.TemplateExpr:
		for _, p := range e.Parts {
			visitExpr(p, inFn, fn)
		}
	case *ast.TaggedTemplateExpr:
		visitExpr(e.Tag, inFn, fn)
		if e.Quasi != nil {
			visitExpr(e.Quasi, inFn, fn)
		}
	case *ast.ClassExpr:
		visitClass(e.Class, fn, inFn)
	case *ast.JSXElement:
		visitJSXElement(e, inFn, fn)
	case *ast.JSXFragment:
		visitJSXChildren(e.Children, inFn, fn)
	}
}

func visitJSXElement(el *ast.JSXElement, inFn bool, fn astVisitor) {
	if el == nil {
		return
	}
	if el.Opening != nil {
		for _, a := range el.Opening.Attributes {
			if a != nil {
				visitExpr(a.Value, inFn, fn)
			}
		}
	}
	visitJSXChildren(el.Children, inFn, fn)
}

func visitJSXChildren(children []ast.JSXChild, inFn bool, fn astVisitor) {
	for _, ch := range children {
		switch c := ch.(type) {
		case *ast.JSXExprContainer:
			visitExpr(c.Expression, inFn, fn)
		case *ast.JSXElementChild:
			if c.Element != nil {
				visitExpr(c.Element, inFn, fn)
			}
		case *ast.JSXFragmentChild:
			if c.Fragment != nil {
				visitExpr(c.Fragment, inFn, fn)
			}
		}
	}
}
