package annotator

import (
	"regexp"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// Copy-on-write AST rewriting for class lowering. The class declaration is
// never mutated: classes are inspected more than once (diagnostics, module
// merging), so every rewrite returns new nodes.

// exprFn rewrites one expression. It may return a replacement directly, or
// call recurse(e) to rebuild e with its children rewritten by the same exprFn.
type exprFn func(e ast.Expr, recurse func(ast.Expr) ast.Expr) ast.Expr

func walkExpr(e ast.Expr, f exprFn) ast.Expr {
	if e == nil {
		return nil
	}
	return f(e, func(x ast.Expr) ast.Expr { return mapExprChildren(x, f) })
}

func walkExprs(es []ast.Expr, f exprFn) []ast.Expr {
	if es == nil {
		return nil
	}
	out := make([]ast.Expr, len(es))
	for i, e := range es {
		out[i] = walkExpr(e, f)
	}
	return out
}

// mapExprChildren rebuilds e with every child expression and statement
// rewritten by f.
func mapExprChildren(e ast.Expr, f exprFn) ast.Expr {
	switch x := e.(type) {
	case *ast.CallExpr:
		c := *x
		c.Callee = walkExpr(x.Callee, f)
		c.Args = walkExprs(x.Args, f)
		return &c
	case *ast.MemberExpr:
		c := *x
		c.Object = walkExpr(x.Object, f)
		if x.Computed {
			c.Property = walkExpr(x.Property, f)
		}
		return &c
	case *ast.BinaryExpr:
		c := *x
		c.Left = walkExpr(x.Left, f)
		c.Right = walkExpr(x.Right, f)
		return &c
	case *ast.UnaryExpr:
		c := *x
		c.Arg = walkExpr(x.Arg, f)
		return &c
	case *ast.ConditionalExpr:
		c := *x
		c.Test = walkExpr(x.Test, f)
		c.Consequent = walkExpr(x.Consequent, f)
		c.Alternate = walkExpr(x.Alternate, f)
		return &c
	case *ast.TypeAssertion:
		c := *x
		c.Expr = walkExpr(x.Expr, f)
		return &c
	case *ast.ParenExpr:
		c := *x
		c.Expr = walkExpr(x.Expr, f)
		return &c
	case *ast.AwaitExpr:
		c := *x
		c.Arg = walkExpr(x.Arg, f)
		return &c
	case *ast.NewExpr:
		c := *x
		c.Callee = walkExpr(x.Callee, f)
		c.Args = walkExprs(x.Args, f)
		return &c
	case *ast.DynamicImport:
		c := *x
		c.Arg = walkExpr(x.Arg, f)
		c.Options = walkExpr(x.Options, f)
		return &c
	case *ast.ArrowFn:
		c := *x
		c.Params = walkParams(x.Params, f)
		c.Body = walkStmts(x.Body, f)
		return &c
	case *ast.ObjectExpr:
		c := *x
		c.Properties = make([]*ast.ObjectProp, len(x.Properties))
		for i, p := range x.Properties {
			if p == nil {
				continue
			}
			pc := *p
			pc.Value = walkExpr(p.Value, f)
			if p.Computed {
				pc.KeyExpr = walkExpr(p.KeyExpr, f)
			}
			// A shorthand `{ count }` whose value was replaced is no longer
			// shorthand.
			if p.Shorthand {
				if id, ok := pc.Value.(*ast.Identifier); !ok || id.Name != p.Key {
					pc.Shorthand = false
				}
			}
			c.Properties[i] = &pc
		}
		return &c
	case *ast.ArrayExpr:
		c := *x
		c.Elements = walkExprs(x.Elements, f)
		return &c
	case *ast.TemplateExpr:
		c := *x
		c.Parts = walkExprs(x.Parts, f)
		return &c
	case *ast.TaggedTemplateExpr:
		c := *x
		c.Tag = walkExpr(x.Tag, f)
		if x.Quasi != nil {
			q := *x.Quasi
			q.Parts = walkExprs(x.Quasi.Parts, f)
			c.Quasi = &q
		}
		return &c
	case *ast.JSXElement:
		c := *x
		if x.Opening != nil {
			o := *x.Opening
			o.Attributes = make([]*ast.JSXAttr, len(x.Opening.Attributes))
			for i, a := range x.Opening.Attributes {
				if a == nil {
					continue
				}
				ac := *a
				ac.Value = walkExpr(a.Value, f)
				o.Attributes[i] = &ac
			}
			c.Opening = &o
		}
		c.Children = walkJSXChildren(x.Children, f)
		return &c
	case *ast.JSXFragment:
		c := *x
		c.Children = walkJSXChildren(x.Children, f)
		return &c
	}
	// Leaves (identifiers, literals, this, import.meta, class expressions).
	return e
}

func walkJSXChildren(children []ast.JSXChild, f exprFn) []ast.JSXChild {
	if children == nil {
		return nil
	}
	out := make([]ast.JSXChild, len(children))
	for i, ch := range children {
		switch c := ch.(type) {
		case *ast.JSXExprContainer:
			out[i] = &ast.JSXExprContainer{Expression: walkExpr(c.Expression, f)}
		case *ast.JSXElementChild:
			out[i] = jsxChildOf(walkExpr(c.Element, f))
		case *ast.JSXFragmentChild:
			out[i] = jsxChildOf(walkExpr(c.Fragment, f))
		default:
			out[i] = ch
		}
	}
	return out
}

// jsxChildOf wraps a rewritten child expression back into the right JSX child
// node.
func jsxChildOf(e ast.Expr) ast.JSXChild {
	switch x := e.(type) {
	case *ast.JSXElement:
		return &ast.JSXElementChild{Element: x}
	case *ast.JSXFragment:
		return &ast.JSXFragmentChild{Fragment: x}
	}
	return &ast.JSXExprContainer{Expression: e}
}

func walkParams(params []*ast.Param, f exprFn) []*ast.Param {
	if params == nil {
		return nil
	}
	out := make([]*ast.Param, len(params))
	for i, p := range params {
		if p == nil {
			continue
		}
		c := *p
		c.Default = walkExpr(p.Default, f)
		out[i] = &c
	}
	return out
}

func walkStmts(stmts []ast.Stmt, f exprFn) []ast.Stmt {
	if stmts == nil {
		return nil
	}
	out := make([]ast.Stmt, 0, len(stmts))
	for _, s := range stmts {
		out = append(out, walkStmt(s, f))
	}
	return out
}

func walkStmt(stmt ast.Stmt, f exprFn) ast.Stmt {
	switch s := stmt.(type) {
	case nil:
		return nil
	case *ast.ExprStmt:
		c := *s
		c.Expression = walkExpr(s.Expression, f)
		return &c
	case *ast.ReturnStmt:
		c := *s
		c.Value = walkExpr(s.Value, f)
		return &c
	case *ast.ThrowStmt:
		c := *s
		c.Value = walkExpr(s.Value, f)
		return &c
	case *ast.VarStmt:
		c := *s
		c.Decls = make([]*ast.VarDecl, len(s.Decls))
		for i, d := range s.Decls {
			if d == nil {
				continue
			}
			dc := *d
			dc.Init = walkExpr(d.Init, f)
			c.Decls[i] = &dc
		}
		return &c
	case *ast.IfStmt:
		c := *s
		c.Test = walkExpr(s.Test, f)
		c.Consequent = walkStmts(s.Consequent, f)
		c.Alternate = walkStmts(s.Alternate, f)
		return &c
	case *ast.BlockStmt:
		c := *s
		c.Body = walkStmts(s.Body, f)
		return &c
	case *ast.ForStmt:
		c := *s
		c.Init = walkStmt(s.Init, f)
		c.Test = walkExpr(s.Test, f)
		c.Update = walkExpr(s.Update, f)
		c.Body = walkStmts(s.Body, f)
		return &c
	case *ast.ForInStmt:
		c := *s
		c.Left = walkExpr(s.Left, f)
		c.Right = walkExpr(s.Right, f)
		c.Body = walkStmts(s.Body, f)
		return &c
	case *ast.WhileStmt:
		c := *s
		c.Test = walkExpr(s.Test, f)
		c.Body = walkStmts(s.Body, f)
		return &c
	case *ast.DoWhileStmt:
		c := *s
		c.Body = walkStmts(s.Body, f)
		c.Test = walkExpr(s.Test, f)
		return &c
	case *ast.SwitchStmt:
		c := *s
		c.Discriminant = walkExpr(s.Discriminant, f)
		c.Cases = make([]*ast.CaseClause, len(s.Cases))
		for i, cl := range s.Cases {
			if cl == nil {
				continue
			}
			cc := *cl
			cc.Test = walkExpr(cl.Test, f)
			cc.Body = walkStmts(cl.Body, f)
			c.Cases[i] = &cc
		}
		return &c
	case *ast.TryStmt:
		c := *s
		c.Body = walkStmts(s.Body, f)
		if s.Catch != nil {
			cc := *s.Catch
			cc.Body = walkStmts(s.Catch.Body, f)
			c.Catch = &cc
		}
		c.Finally = walkStmts(s.Finally, f)
		return &c
	case *ast.LabeledStmt:
		c := *s
		c.Body = walkStmt(s.Body, f)
		return &c
	case *ast.WithStmt:
		c := *s
		c.Object = walkExpr(s.Object, f)
		c.Body = walkStmts(s.Body, f)
		return &c
	case *ast.FnDecl:
		c := *s
		c.Params = walkParams(s.Params, f)
		c.Body = walkStmts(s.Body, f)
		return &c
	}
	return stmt
}

// inspectExpr calls visit for every expression under e (pre-order). Returning
// false from visit skips that node's children.
func inspectExpr(e ast.Expr, visit func(ast.Expr) bool) {
	walkExpr(e, func(x ast.Expr, recurse func(ast.Expr) ast.Expr) ast.Expr {
		if visit(x) {
			recurse(x)
		}
		return x
	})
}

func inspectStmts(stmts []ast.Stmt, visit func(ast.Expr) bool) {
	walkStmts(stmts, func(x ast.Expr, recurse func(ast.Expr) ast.Expr) ast.Expr {
		if visit(x) {
			recurse(x)
		}
		return x
	})
}

// thisMember returns the property name of a non-computed `this.<name>`.
func thisMember(e ast.Expr) (string, *ast.MemberExpr, bool) {
	m, ok := e.(*ast.MemberExpr)
	if !ok || m.Computed {
		return "", nil, false
	}
	if _, ok := m.Object.(*ast.ThisExpr); !ok {
		return "", nil, false
	}
	p, ok := m.Property.(*ast.Identifier)
	if !ok {
		return "", nil, false
	}
	return p.Name, m, true
}

// isOwnThisFunction reports whether e is a function that binds its own
// `this` (a function expression or method, not an arrow).
func isOwnThisFunction(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.ArrowFn:
		return x.Function
	case *ast.ClassExpr:
		return true
	}
	return false
}

var identRe = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*`)

// boundNames collects every name bound anywhere inside stmts (declarations,
// function and arrow parameters, catch parameters). It over-approximates
// destructuring patterns by taking every identifier in their source.
func boundNames(stmts []ast.Stmt) map[string]bool {
	out := map[string]bool{}
	addParams := func(params []*ast.Param) {
		for _, p := range params {
			if p == nil {
				continue
			}
			if p.Pattern != "" {
				for _, n := range identRe.FindAllString(p.Pattern, -1) {
					out[n] = true
				}
			} else if p.Name != "" {
				out[p.Name] = true
			}
		}
	}
	var stmtsFn func([]ast.Stmt)
	stmtsFn = func(list []ast.Stmt) {
		for _, s := range list {
			switch v := s.(type) {
			case *ast.VarStmt:
				for _, d := range v.Decls {
					if d == nil {
						continue
					}
					if d.Name != "" {
						out[d.Name] = true
					}
					for _, n := range d.Names {
						out[n] = true
					}
					if d.Pattern != "" {
						for _, n := range identRe.FindAllString(d.Pattern, -1) {
							out[n] = true
						}
					}
				}
			case *ast.FnDecl:
				out[v.Name] = true
				addParams(v.Params)
				stmtsFn(v.Body)
			case *ast.IfStmt:
				stmtsFn(v.Consequent)
				stmtsFn(v.Alternate)
			case *ast.BlockStmt:
				stmtsFn(v.Body)
			case *ast.ForStmt:
				if v.Init != nil {
					stmtsFn([]ast.Stmt{v.Init})
				}
				stmtsFn(v.Body)
			case *ast.ForInStmt:
				inspectExpr(v.Left, func(e ast.Expr) bool {
					if id, ok := e.(*ast.Identifier); ok {
						out[id.Name] = true
					}
					return true
				})
				stmtsFn(v.Body)
			case *ast.WhileStmt:
				stmtsFn(v.Body)
			case *ast.DoWhileStmt:
				stmtsFn(v.Body)
			case *ast.SwitchStmt:
				for _, c := range v.Cases {
					if c != nil {
						stmtsFn(c.Body)
					}
				}
			case *ast.TryStmt:
				stmtsFn(v.Body)
				if v.Catch != nil {
					for _, n := range identRe.FindAllString(v.Catch.Param, -1) {
						out[n] = true
					}
					stmtsFn(v.Catch.Body)
				}
				stmtsFn(v.Finally)
			case *ast.LabeledStmt:
				stmtsFn([]ast.Stmt{v.Body})
			}
		}
	}
	stmtsFn(stmts)
	inspectStmts(stmts, func(e ast.Expr) bool {
		if fn, ok := e.(*ast.ArrowFn); ok {
			addParams(fn.Params)
			stmtsFn(fn.Body)
		}
		return true
	})
	return out
}
