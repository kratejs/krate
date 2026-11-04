package irtree

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// React.Children over call-site children.
//
// `Children.map(props.children, (c, i) => cloneElement(c, { active }))` in a
// stateful component (Tabs, RadioGroup) iterates the elements its caller wrote.
// Those are known at build time, so the call is unrolled over them: the helper
// receives the literal element list and folds like any static Children call
// (see foldStructuralReact). Call-site elements whose props read the caller's
// state cannot move into this component's scope and are reported.

// childrenItems lists the call-site children as the values React.Children
// iterates: elements, fragments and non-blank text.
func childrenItems(children []ast.JSXChild) ([]ast.Expr, bool) {
	var items []ast.Expr
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXText:
			if strings.TrimSpace(c.Value) != "" {
				items = append(items, &ast.Literal{Kind: ast.StringLit, Value: strings.TrimSpace(c.Value)})
			}
		case *ast.JSXElementChild:
			items = append(items, c.Element)
		case *ast.JSXFragmentChild:
			items = append(items, c.Fragment)
		case *ast.JSXExprContainer:
			if c.Expression == nil {
				continue
			}
			if !isJSXExpr(c.Expression) {
				return nil, false
			}
			items = append(items, c.Expression)
		}
	}
	return items, true
}

// resolveChildrenArg rewrites `Children.<fn>(props.children, ...)` to take the
// literal call-site children, so it folds statically.
func (b *builder) resolveChildrenArg(expr ast.Expr) (ast.Expr, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) == 0 || !isChildrenPlaceholder(call.Args[0]) {
		return nil, false
	}
	mem, ok := call.Callee.(*ast.MemberExpr)
	if !ok || mem.Computed {
		return nil, false
	}
	if obj, ok := mem.Object.(*ast.Identifier); !ok || obj.Name != "Children" {
		return nil, false
	}
	if len(b.callSiteChildren) == 0 {
		return nil, false
	}
	items, ok := childrenItems(b.callSiteChildren)
	if !ok {
		return nil, false
	}
	for _, item := range items {
		if el, ok := item.(*ast.JSXElement); ok && b.elementReadsCallerState(el) {
			if b.locateOnly == 0 {
				b.warnAt(call.Position, "Children."+mem.Property.(*ast.Identifier).Name+" over children whose props read the caller's state cannot be unrolled at build time",
					"pass the shared value through context or as a prop of the parent, or render the children with a function-as-children prop")
			}
			return nil, false
		}
	}
	args := append([]ast.Expr{&ast.ArrayExpr{Elements: items}}, call.Args[1:]...)
	return &ast.CallExpr{Position: call.Position, Callee: call.Callee, Args: args}, true
}

// elementReadsCallerState reports whether an element's attributes or children
// read a signal of the scope the call-site children belong to.
func (b *builder) elementReadsCallerState(el *ast.JSXElement) bool {
	if el == nil || el.Opening == nil {
		return false
	}
	for _, a := range el.Opening.Attributes {
		if a != nil && a.Value != nil && b.callerReferencesSignal(a.Value) {
			return true
		}
	}
	return false
}

// callerReferencesSignal reports whether expr reads a signal declared by the
// component that wrote the call-site children (the enclosing context on the
// build stack).
func (b *builder) callerReferencesSignal(expr ast.Expr) bool {
	if len(b.ctxStack) == 0 {
		return false
	}
	caller := b.ctxStack[len(b.ctxStack)-1].localSignals
	if len(caller) == 0 {
		return false
	}
	found := false
	scanJSIdents(generateExprJS(expr, nil), func(name string) {
		if _, ok := caller[name]; ok {
			found = true
		}
	})
	return found
}

// functionChild returns the arrow function that is the only child of a
// component call (`<Toggle>{(on) => ...}</Toggle>`).
func functionChild(children []ast.JSXChild) ast.Expr {
	var fn ast.Expr
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXText:
			if strings.TrimSpace(c.Value) != "" {
				return nil
			}
		case *ast.JSXExprContainer:
			if fn != nil || c.Expression == nil {
				return nil
			}
			e := c.Expression
			for {
				p, ok := e.(*ast.ParenExpr)
				if !ok {
					break
				}
				e = p.Expr
			}
			if _, ok := e.(*ast.ArrowFn); !ok {
				return nil
			}
			fn = e
		default:
			return nil
		}
	}
	return fn
}
