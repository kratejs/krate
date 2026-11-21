package bundler

import (
	"strconv"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// getterHooks are the React hooks whose lowered result is a single getter
// (useSyncExternalStore becomes createExternalStore, useMemo createMemoDeps).
// A declaration `const v = useSyncExternalStore(...)` makes v a getter whose
// reads become calls; a call written inline, `<span>{useSyncExternalStore(...)}</span>`,
// had no binding to read through, so the page got the getter itself: an empty
// server render and a TypeError in the browser.
var getterHooks = map[string]bool{"useSyncExternalStore": true, "useMemo": true}

// hoistInlineGetterHooks moves each call of a getter hook nested inside a
// top-level statement of a component body into a `const` declared
// just before that statement, and puts the binding in its place, so it lowers
// like a declared one. A hook runs once per render at the top level, so the
// move keeps its meaning; calls in a conditional branch, the right side of
// &&, || or ??, an optional chain or a nested function are left alone. A
// custom hook's `return useSyncExternalStore(...)` keeps the compat runtime's
// hook, which the build-time render can evaluate there.
func (c *reactCtx) hoistInlineGetterHooks(stmts []ast.Stmt) []ast.Stmt {
	var out []ast.Stmt
	for _, stmt := range stmts {
		var before []ast.Stmt
		switch s := stmt.(type) {
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if call, ok := d.Init.(*ast.CallExpr); ok && !d.IsDestructuring {
					if api, ok := c.callAPI(call.Callee); ok && getterHooks[api] {
						// Already declared: only its arguments may hold more.
						c.hoistArgs(call, &before)
						continue
					}
				}
				d.Init = c.hoistGetterHooks(d.Init, &before)
			}
		case *ast.ReturnStmt:
			s.Value = c.hoistGetterHooks(s.Value, &before)
		case *ast.ExprStmt:
			s.Expression = c.hoistGetterHooks(s.Expression, &before)
		}
		if before == nil && out == nil {
			continue
		}
		if out == nil {
			out = append([]ast.Stmt(nil), stmts[:indexOfStmt(stmts, stmt)]...)
		}
		out = append(out, before...)
		out = append(out, stmt)
	}
	if out == nil {
		return stmts
	}
	return out
}

func indexOfStmt(stmts []ast.Stmt, s ast.Stmt) int {
	for i, x := range stmts {
		if x == s {
			return i
		}
	}
	return len(stmts)
}

func (c *reactCtx) hoistArgs(call *ast.CallExpr, before *[]ast.Stmt) {
	for i, a := range call.Args {
		call.Args[i] = c.hoistGetterHooks(a, before)
	}
}

// hoistGetterHooks replaces the getter hook calls in e (see
// hoistInlineGetterHooks), appending their declarations to before in
// evaluation order.
func (c *reactCtx) hoistGetterHooks(e ast.Expr, before *[]ast.Stmt) ast.Expr {
	switch x := e.(type) {
	case nil:
		return nil
	case *ast.CallExpr:
		if x.Optional {
			return x
		}
		x.Callee = c.hoistGetterHooks(x.Callee, before)
		c.hoistArgs(x, before)
		if api, ok := c.callAPI(x.Callee); ok && getterHooks[api] {
			name := "__krate_hook" + strconv.Itoa(c.hoisted)
			c.hoisted++
			*before = append(*before, &ast.VarStmt{Position: x.Position, Kind: ast.VarConst, Decls: []*ast.VarDecl{{Name: name, Init: x}}})
			return &ast.Identifier{Position: x.Position, Name: name}
		}
	case *ast.MemberExpr:
		x.Object = c.hoistGetterHooks(x.Object, before)
		if x.Computed && !x.Optional {
			x.Property = c.hoistGetterHooks(x.Property, before)
		}
	case *ast.BinaryExpr:
		x.Left = c.hoistGetterHooks(x.Left, before)
		switch x.Op {
		case "&&", "||", "??", "&&=", "||=", "??=":
		default:
			x.Right = c.hoistGetterHooks(x.Right, before)
		}
	case *ast.UnaryExpr:
		x.Arg = c.hoistGetterHooks(x.Arg, before)
	case *ast.ConditionalExpr:
		x.Test = c.hoistGetterHooks(x.Test, before)
	case *ast.TypeAssertion:
		x.Expr = c.hoistGetterHooks(x.Expr, before)
	case *ast.ParenExpr:
		x.Expr = c.hoistGetterHooks(x.Expr, before)
	case *ast.AwaitExpr:
		x.Arg = c.hoistGetterHooks(x.Arg, before)
	case *ast.NewExpr:
		x.Callee = c.hoistGetterHooks(x.Callee, before)
		for i, a := range x.Args {
			x.Args[i] = c.hoistGetterHooks(a, before)
		}
	case *ast.ArrayExpr:
		for i, el := range x.Elements {
			x.Elements[i] = c.hoistGetterHooks(el, before)
		}
	case *ast.ObjectExpr:
		for _, p := range x.Properties {
			if p == nil || p.Method || p.Getter || p.Setter {
				continue
			}
			if p.Computed {
				p.KeyExpr = c.hoistGetterHooks(p.KeyExpr, before)
			}
			v := c.hoistGetterHooks(p.Value, before)
			if p.Shorthand && v != p.Value {
				p.Shorthand = false
			}
			p.Value = v
		}
	case *ast.TemplateExpr:
		for i, part := range x.Parts {
			x.Parts[i] = c.hoistGetterHooks(part, before)
		}
	case *ast.JSXElement:
		c.hoistJSXElement(x, before)
	case *ast.JSXFragment:
		x.Key = c.hoistGetterHooks(x.Key, before)
		c.hoistJSXChildren(x.Children, before)
	}
	return e
}

func (c *reactCtx) hoistJSXElement(el *ast.JSXElement, before *[]ast.Stmt) {
	if el == nil {
		return
	}
	if el.Opening != nil {
		for _, a := range el.Opening.Attributes {
			if a != nil {
				a.Value = c.hoistGetterHooks(a.Value, before)
			}
		}
	}
	c.hoistJSXChildren(el.Children, before)
}

func (c *reactCtx) hoistJSXChildren(children []ast.JSXChild, before *[]ast.Stmt) {
	for _, ch := range children {
		switch x := ch.(type) {
		case *ast.JSXExprContainer:
			x.Expression = c.hoistGetterHooks(x.Expression, before)
		case *ast.JSXElementChild:
			c.hoistJSXElement(x.Element, before)
		case *ast.JSXFragmentChild:
			if x.Fragment != nil {
				x.Fragment.Key = c.hoistGetterHooks(x.Fragment.Key, before)
				c.hoistJSXChildren(x.Fragment.Children, before)
			}
		}
	}
}
