package irtree

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// Renderer boundaries.
//
// A component marked `@renderer` (or with a "use renderer" / "@renderer"
// string at the top of its body) hands its children to a non-DOM renderer in
// the browser, such as a three.js scene graph. Its call-site children are never
// rendered to HTML and get no slots or bindings. Instead the component's props
// registration carries `children: () => [...]`, the children compiled in the
// reactive h() form of list rows and evaluated in the caller's hydration scope.
//
// In renderer children every non-event, non-ref function-valued prop of an
// intrinsic element is a thunk (`attach: () => ((parent, self) => ...)`), so a
// renderer can tell a reactive value from a function value.

// IsRendererFn reports whether a component is a renderer boundary.
func IsRendererFn(fn *ast.FnDecl) bool {
	if fn == nil {
		return false
	}
	return fn.Renderer || isRendererDirective(fn.Body)
}

// IsRendererDecl reports whether a const declaration is a renderer component
// (`@renderer const Canvas = (props) => ...`).
func IsRendererDecl(d *ast.VarDecl) bool {
	if d == nil {
		return false
	}
	if d.Renderer {
		return true
	}
	fn, ok := d.Init.(*ast.ArrowFn)
	return ok && isRendererDirective(fn.Body)
}

// isRendererDirective reports a `"use renderer"` or `"@renderer"` string in
// the directive prologue of a function body.
func isRendererDirective(body []ast.Stmt) bool {
	// Any string of the directive prologue counts.
	for _, stmt := range body {
		es, ok := stmt.(*ast.ExprStmt)
		if !ok {
			return false
		}
		lit, ok := es.Expression.(*ast.Literal)
		if !ok || lit.Kind != ast.StringLit {
			return false
		}
		val := strings.TrimSpace(strings.Trim(lit.Value, "\"'"))
		if val == "use renderer" || val == "@renderer" {
			return true
		}
	}
	return false
}

// reactifier lowers JSX to the reactive h() form. isRenderer names the
// renderer boundary components, whose children become a `children` thunk
// instead of h() arguments.
type reactifier struct {
	isRenderer func(name string) bool
}

// plainReactifier knows no renderer components.
var plainReactifier = &reactifier{}

func (r *reactifier) rendererComponent(name string) bool {
	return r != nil && r.isRenderer != nil && r.isRenderer(name)
}

// reactifier returns the builder's reactifier, which resolves renderer
// components against the bundled functions.
func (b *builder) reactifier() *reactifier {
	return &reactifier{isRenderer: func(name string) bool { return IsRendererFn(b.functions[name]) }}
}

// rendererChildren compiles renderer children to `() => [child, ...]`, or nil
// when there are none.
func (r *reactifier) rendererChildren(children []ast.JSXChild) ast.Expr {
	var elems []ast.Expr
	for _, child := range r.reactifyChildren(rendererJSXChildren(children)) {
		switch c := child.(type) {
		case *ast.JSXText:
			if strings.TrimSpace(c.Value) != "" {
				elems = append(elems, &ast.Literal{Kind: ast.StringLit, Value: c.Value})
			}
		case *ast.JSXExprContainer:
			if c.Expression != nil {
				elems = append(elems, c.Expression)
			}
		case *ast.JSXElementChild:
			elems = append(elems, c.Element)
		case *ast.JSXFragmentChild:
			elems = append(elems, c.Fragment)
		}
	}
	if len(elems) == 0 {
		return nil
	}
	return &ast.ArrowFn{Expression: true, Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.ArrayExpr{Elements: elems}}}}
}

// rendererModeFn returns a copy of a component whose returned JSX is renderer
// JSX (function-valued intrinsic props thunked).
func rendererModeFn(fn *ast.FnDecl) *ast.FnDecl {
	cp := *fn
	cp.Body = mapReturns(fn.Body, rendererJSX)
	return &cp
}

// mapReturns rewrites the values returned at top level of a function body.
func mapReturns(stmts []ast.Stmt, f func(ast.Expr) ast.Expr) []ast.Stmt {
	out := make([]ast.Stmt, len(stmts))
	for i, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.ReturnStmt:
			if s.Value != nil {
				out[i] = &ast.ReturnStmt{Position: s.Position, Value: f(s.Value)}
				continue
			}
		case *ast.IfStmt:
			cp := *s
			cp.Consequent = mapReturns(s.Consequent, f)
			cp.Alternate = mapReturns(s.Alternate, f)
			out[i] = &cp
			continue
		case *ast.BlockStmt:
			out[i] = &ast.BlockStmt{Position: s.Position, Body: mapReturns(s.Body, f)}
			continue
		case *ast.SwitchStmt:
			cp := *s
			cp.Cases = make([]*ast.CaseClause, len(s.Cases))
			for j, c := range s.Cases {
				if c != nil {
					cc := *c
					cc.Body = mapReturns(c.Body, f)
					cp.Cases[j] = &cc
				}
			}
			out[i] = &cp
			continue
		}
		out[i] = stmt
	}
	return out
}

// rendererJSX rewrites the JSX in an expression to renderer JSX.
func rendererJSX(e ast.Expr) ast.Expr {
	switch v := e.(type) {
	case *ast.JSXElement:
		return rendererElement(v)
	case *ast.JSXFragment:
		cp := *v
		cp.Children = rendererJSXChildren(v.Children)
		return &cp
	case *ast.ParenExpr:
		return &ast.ParenExpr{Position: v.Position, Expr: rendererJSX(v.Expr)}
	case *ast.TypeAssertion:
		return rendererJSX(v.Expr)
	case *ast.ConditionalExpr:
		return &ast.ConditionalExpr{Position: v.Position, Test: v.Test, Consequent: rendererJSX(v.Consequent), Alternate: rendererJSX(v.Alternate)}
	case *ast.BinaryExpr:
		if v.Op == "&&" || v.Op == "||" || v.Op == "??" {
			return &ast.BinaryExpr{Position: v.Position, Left: v.Left, Op: v.Op, Right: rendererJSX(v.Right)}
		}
	case *ast.ArrayExpr:
		out := make([]ast.Expr, len(v.Elements))
		for i, el := range v.Elements {
			if el != nil {
				out[i] = rendererJSX(el)
			}
		}
		return &ast.ArrayExpr{Position: v.Position, Elements: out}
	case *ast.CallExpr:
		// `.map(item => <mesh/>)` and similar callbacks render renderer JSX.
		cp := *v
		cp.Args = make([]ast.Expr, len(v.Args))
		for i, a := range v.Args {
			if fn, ok := a.(*ast.ArrowFn); ok && containsJSXReturn(fn) {
				fc := *fn
				fc.Body = mapReturns(fn.Body, rendererJSX)
				if fn.Expression && len(fn.Body) == 1 {
					if es, ok := fn.Body[0].(*ast.ExprStmt); ok {
						fc.Body = []ast.Stmt{&ast.ExprStmt{Expression: rendererJSX(es.Expression)}}
					}
				}
				cp.Args[i] = &fc
				continue
			}
			cp.Args[i] = a
		}
		return &cp
	}
	return e
}

func containsJSXReturn(fn *ast.ArrowFn) bool {
	if e := arrowBodyExpr(fn); e != nil && containsJSX(e) {
		return true
	}
	return fnReturnsJSX(fn.Body)
}

func rendererJSXChildren(children []ast.JSXChild) []ast.JSXChild {
	out := make([]ast.JSXChild, len(children))
	for i, child := range children {
		switch c := child.(type) {
		case *ast.JSXElementChild:
			out[i] = &ast.JSXElementChild{Element: rendererElement(c.Element)}
		case *ast.JSXFragmentChild:
			f := rendererJSX(c.Fragment).(*ast.JSXFragment)
			out[i] = &ast.JSXFragmentChild{Fragment: f}
		case *ast.JSXExprContainer:
			if c.Expression == nil {
				out[i] = c
				continue
			}
			out[i] = &ast.JSXExprContainer{Expression: rendererJSX(c.Expression)}
		default:
			out[i] = child
		}
	}
	return out
}

func rendererElement(el *ast.JSXElement) *ast.JSXElement {
	if el == nil || el.Opening == nil {
		return el
	}
	opening := *el.Opening
	if isSuspenseName(opening.Name) {
		// A renderer handles suspension itself: the boundary is a plain
		// descriptor, so the DOM Suspense runtime is neither referenced nor
		// shipped.
		opening.Name = RendererSuspenseTag
	}
	intrinsic := !isComponentName(opening.Name)
	opening.Attributes = make([]*ast.JSXAttr, len(el.Opening.Attributes))
	for i, attr := range el.Opening.Attributes {
		if attr == nil || attr.Value == nil {
			opening.Attributes[i] = attr
			continue
		}
		cp := *attr
		switch {
		case attr.Spread:
		case intrinsic && attr.Name == "ref" && isAssignableRef(attr.Value):
			cp.Value = adaptiveRef(unparen(attr.Value))
		case intrinsic && attr.Name != "ref" && attr.Name != "key" && !isOnEvent(attr.Name) && isFunctionExpr(attr.Value):
			cp.Value = thunk(&ast.ParenExpr{Expr: unparen(attr.Value)})
		default:
			cp.Value = rendererJSX(attr.Value)
		}
		opening.Attributes[i] = &cp
	}
	out := *el
	out.Opening = &opening
	out.Children = rendererJSXChildren(el.Children)
	return &out
}

// isAssignableRef reports a ref written as a variable or member
// (`ref={mesh}`), which may hold a callback, a {current} object, or be a plain
// Krate-style variable that receives the object.
func isAssignableRef(e ast.Expr) bool {
	switch unparen(e).(type) {
	case *ast.Identifier, *ast.MemberExpr:
		return true
	}
	return false
}

// adaptiveRef lowers `ref={x}` for a renderer, which only knows callback and
// {current} refs: a callback or {current} value passes through, anything else
// (an unassigned `let x`) becomes a callback that assigns x, as DOM refs do.
//
//	((r) => typeof r === "function" || (r !== null && typeof r === "object" && "current" in r) ? r : (v) => { x = v; })(x)
func adaptiveRef(target ast.Expr) ast.Expr {
	r := func() ast.Expr { return &ast.Identifier{Name: "__r"} }
	str := func(v string) ast.Expr { return &ast.Literal{Kind: ast.StringLit, Value: v} }
	typeOf := func(want string) ast.Expr {
		return &ast.BinaryExpr{Left: &ast.UnaryExpr{Op: "typeof", Arg: r()}, Op: "===", Right: str(want)}
	}
	isObject := &ast.BinaryExpr{
		Left: &ast.BinaryExpr{
			Left:  &ast.BinaryExpr{Left: r(), Op: "!==", Right: &ast.Literal{Kind: ast.NullLit, Value: "null"}},
			Op:    "&&",
			Right: typeOf("object"),
		},
		Op:    "&&",
		Right: &ast.BinaryExpr{Left: str("current"), Op: "in", Right: r()},
	}
	assign := &ast.ArrowFn{
		Params: []*ast.Param{{Name: "__v"}},
		Body:   []ast.Stmt{&ast.ExprStmt{Expression: &ast.BinaryExpr{Left: target, Op: "=", Right: &ast.Identifier{Name: "__v"}}}},
	}
	pick := &ast.ArrowFn{
		Params:     []*ast.Param{{Name: "__r"}},
		Expression: true,
		Body: []ast.Stmt{&ast.ReturnStmt{Value: &ast.ConditionalExpr{
			Test:       &ast.BinaryExpr{Left: typeOf("function"), Op: "||", Right: isObject},
			Consequent: r(),
			Alternate:  assign,
		}}},
	}
	return &ast.CallExpr{Callee: &ast.ParenExpr{Expr: pick}, Args: []ast.Expr{target}}
}

// RendererSuspenseTag is the intrinsic tag a <Suspense> boundary in renderer
// children compiles to: h('krate-suspense', {fallback}, ...children).
const RendererSuspenseTag = "krate-suspense"

func isSuspenseName(name string) bool {
	return name == "Suspense" || name == "React.Suspense"
}

func isFunctionExpr(e ast.Expr) bool {
	_, ok := unparen(e).(*ast.ArrowFn)
	return ok
}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.Expr
	}
}
