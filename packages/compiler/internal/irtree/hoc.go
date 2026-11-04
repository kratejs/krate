package irtree

import (
	"github.com/kratejs/krate/packages/compiler/ast"
)

// Higher-order components.
//
// `const CounterButton = withCounter(Button)` where
//
//	function withCounter(Wrapped) {
//	  return function WithCounter(props) { ...; return <Wrapped {...props} /> };
//	}
//
// is lowered to a component function named after the binding whose body is the
// returned function with the factory's parameters replaced by the call
// arguments (`<Wrapped>` becomes `<Button>`), so the wrapped component is
// compiled and hydrated like any hand-written one.

// HOCFactory is a function that returns a component.
type HOCFactory struct {
	Params []*ast.Param
	Body   []ast.Stmt
}

// InstantiateHOC returns the component function for `const name =
// factory(args...)`, or nil when the factory is not a simple wrapper (it
// returns something other than a function, has statements besides the inner
// component, or takes patterns/rest parameters).
func InstantiateHOC(name string, factory HOCFactory, args []ast.Expr) *ast.FnDecl {
	if name == "" || len(args) > len(factory.Params) {
		return nil
	}
	for _, p := range factory.Params {
		if !plainParamNode(p) {
			return nil
		}
	}
	inner := hocInnerFunction(factory.Body)
	if inner == nil {
		return nil
	}
	body := inner.Body
	for i, arg := range args {
		if arg == nil {
			return nil
		}
		s := &listParamSubstituter{param: factory.Params[i].Name, elem: arg}
		if id, ok := arg.(*ast.Identifier); ok {
			s.tagTo = id.Name
		}
		body = s.subStmts(body)
	}
	fn := *inner
	fn.Name = name
	fn.Body = body
	fn.Export, fn.Default = false, false
	return &fn
}

func plainParamNode(p *ast.Param) bool {
	return p != nil && p.Name != "" && p.Name != "{...}" && p.Name != "[...]" && p.Pattern == "" && !p.IsRest
}

// hocInnerFunction finds the function a factory returns when the factory body
// holds nothing else but that function's declaration.
func hocInnerFunction(body []ast.Stmt) *ast.FnDecl {
	if len(body) == 0 {
		return nil
	}
	ret, ok := body[len(body)-1].(*ast.ReturnStmt)
	if !ok || ret.Value == nil {
		return nil
	}
	rest := body[:len(body)-1]
	switch v := ret.Value.(type) {
	case *ast.ArrowFn:
		if len(rest) != 0 {
			return nil
		}
		return arrowToFnDecl(v)
	case *ast.Identifier:
		if len(rest) != 1 {
			return nil
		}
		switch d := rest[0].(type) {
		case *ast.FnDecl:
			if d.Name == v.Name {
				return d
			}
		case *ast.VarStmt:
			if len(d.Decls) == 1 && d.Decls[0] != nil && d.Decls[0].Name == v.Name {
				if arrow, ok := d.Decls[0].Init.(*ast.ArrowFn); ok {
					return arrowToFnDecl(arrow)
				}
			}
		}
	}
	return nil
}

func arrowToFnDecl(a *ast.ArrowFn) *ast.FnDecl {
	if a == nil || a.Async || a.Generator {
		return nil
	}
	body := a.Body
	if a.Expression && len(body) == 1 {
		if es, ok := body[0].(*ast.ExprStmt); ok {
			body = []ast.Stmt{&ast.ReturnStmt{Position: es.Position, Value: es.Expression}}
		}
	}
	return &ast.FnDecl{Position: a.Position, Name: a.Name, Params: a.Params, Body: body}
}
