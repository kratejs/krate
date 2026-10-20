package irtree

import (
	"strconv"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// foldStructuralReact folds React's structural helpers when their inputs are
// statically known, so SSR renders the real children instead of deferring to a
// client-only DOM shim:
//
//	Children.only(x) / toArray(x) / count(x)
//	isValidElement(<el/>)
//	cloneElement(<el/>, { ...props })
//
// Dynamic inputs (e.g. `props.children`) are left for the runtime shim.
func foldStructuralReact(expr ast.Expr) (ast.Expr, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, false
	}

	if mem, ok := call.Callee.(*ast.MemberExpr); ok && !mem.Computed {
		if obj, ok := mem.Object.(*ast.Identifier); ok && obj.Name == "Children" {
			if prop, ok := mem.Property.(*ast.Identifier); ok {
				if prop.Name == "map" && len(call.Args) == 2 {
					if frag, ok := foldChildrenMap(call.Args[0], call.Args[1]); ok {
						return frag, true
					}
					return nil, false
				}
				if len(call.Args) != 1 {
					return nil, false
				}
				arg := call.Args[0]
				switch prop.Name {
				case "only":
					return arg, true
				case "toArray":
					if arr, ok := arg.(*ast.ArrayExpr); ok {
						return arr, true
					}
					if isNullLiteral(arg) {
						return &ast.ArrayExpr{}, true
					}
					return &ast.ArrayExpr{Elements: []ast.Expr{arg}}, true
				case "count":
					if arr, ok := arg.(*ast.ArrayExpr); ok {
						return numberLit(len(arr.Elements)), true
					}
					if isNullLiteral(arg) {
						return numberLit(0), true
					}
					if _, ok := arg.(*ast.Literal); ok {
						return numberLit(1), true
					}
					if isJSXExpr(arg) {
						return numberLit(1), true
					}
				}
			}
		}
	}

	if id, ok := call.Callee.(*ast.Identifier); ok && len(call.Args) == 1 && id.Name == "isValidElement" {
		arg := call.Args[0]
		if isJSXExpr(arg) {
			return &ast.Literal{Kind: ast.BoolLit, Value: "true"}, true
		}
		if isNullLiteral(arg) {
			return &ast.Literal{Kind: ast.BoolLit, Value: "false"}, true
		}
		if _, ok := arg.(*ast.Literal); ok {
			return &ast.Literal{Kind: ast.BoolLit, Value: "false"}, true
		}
	}

	if id, ok := call.Callee.(*ast.Identifier); ok && id.Name == "cloneElement" && len(call.Args) >= 1 {
		el, ok := call.Args[0].(*ast.JSXElement)
		if !ok {
			return nil, false
		}
		if len(call.Args) >= 2 {
			if props, ok := call.Args[1].(*ast.ObjectExpr); ok {
				return mergeJSXProps(el, props), true
			}
			return nil, false
		}
		return el, true
	}
	return nil, false
}

func isJSXExpr(expr ast.Expr) bool {
	switch expr.(type) {
	case *ast.JSXElement, *ast.JSXFragment:
		return true
	}
	return false
}

func isNullLiteral(expr ast.Expr) bool {
	lit, ok := expr.(*ast.Literal)
	return ok && lit.Kind == ast.NullLit
}

func numberLit(n int) ast.Expr {
	return &ast.Literal{Kind: ast.NumberLit, Value: strconv.Itoa(n)}
}

// mergeJSXProps returns a copy of el with props merged onto it. The original
// element is not mutated (it may be shared through the module cache).
func mergeJSXProps(el *ast.JSXElement, props *ast.ObjectExpr) *ast.JSXElement {
	cloned := *el
	if el.Opening == nil {
		return &cloned
	}
	opening := *el.Opening
	attrs := make([]*ast.JSXAttr, 0, len(el.Opening.Attributes)+len(props.Properties))
	for _, a := range el.Opening.Attributes {
		if a == nil {
			continue
		}
		cp := *a
		attrs = append(attrs, &cp)
	}
	for _, p := range props.Properties {
		if p == nil {
			continue
		}
		if p.Spread {
			attrs = append(attrs, &ast.JSXAttr{Spread: true, Value: p.Value})
			continue
		}
		if p.Key == "" || p.Key == "children" {
			continue
		}
		replaced := false
		for _, a := range attrs {
			if a != nil && a.Name == p.Key {
				a.Value = p.Value
				replaced = true
				break
			}
		}
		if !replaced {
			attrs = append(attrs, &ast.JSXAttr{Name: p.Key, Value: p.Value})
		}
	}
	opening.Attributes = attrs
	cloned.Opening = &opening
	return &cloned
}

// foldChildrenMap evaluates Children.map(children, cb) when children is a
// literal list and cb is an inline arrow, producing a fragment of the mapped
// output. Dynamic children are left for the runtime shim.
func foldChildrenMap(children ast.Expr, cb ast.Expr) (ast.Expr, bool) {
	items, ok := staticChildrenList(children)
	if !ok {
		return nil, false
	}
	arrow, ok := cb.(*ast.ArrowFn)
	if !ok || len(arrow.Params) == 0 || arrow.Params[0].Name == "" || arrow.Params[0].Pattern != "" {
		return nil, false
	}
	body, ok := arrowReturnExpr(arrow)
	if !ok {
		return nil, false
	}
	frag := &ast.JSXFragment{}
	for i, item := range items {
		names := map[string]ast.Expr{arrow.Params[0].Name: transformExpr(item, nil, nil)}
		if len(arrow.Params) > 1 && arrow.Params[1].Name != "" && arrow.Params[1].Pattern == "" {
			names[arrow.Params[1].Name] = numberLit(i)
		}
		out := transformExpr(body, names, nil)
		switch v := out.(type) {
		case *ast.JSXElement:
			frag.Children = append(frag.Children, &ast.JSXElementChild{Element: v})
		case *ast.JSXFragment:
			frag.Children = append(frag.Children, &ast.JSXFragmentChild{Fragment: v})
		default:
			frag.Children = append(frag.Children, &ast.JSXExprContainer{Expression: v})
		}
	}
	return frag, true
}

func staticChildrenList(expr ast.Expr) ([]ast.Expr, bool) {
	switch v := expr.(type) {
	case *ast.ArrayExpr:
		return v.Elements, true
	case *ast.JSXElement, *ast.JSXFragment:
		return []ast.Expr{expr}, true
	case *ast.Literal:
		if v.Kind == ast.NullLit {
			return nil, true
		}
		return []ast.Expr{expr}, true
	}
	return nil, false
}

func arrowReturnExpr(fn *ast.ArrowFn) (ast.Expr, bool) {
	if len(fn.Body) == 1 {
		if ret, ok := fn.Body[0].(*ast.ReturnStmt); ok && ret.Value != nil {
			return ret.Value, true
		}
	}
	return nil, false
}

// transformExpr deep-copies expr, replacing identifiers named in names. Nested
// arrow parameters shadow outer substitutions. Unknown nodes are returned
// as-is; callers never mutate them.
func transformExpr(expr ast.Expr, names map[string]ast.Expr, shadowed map[string]bool) ast.Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		if !shadowed[e.Name] {
			if repl, ok := names[e.Name]; ok {
				return repl
			}
		}
		return &ast.Identifier{Position: e.Position, Name: e.Name}
	case *ast.Literal:
		cp := *e
		return &cp
	case *ast.CallExpr:
		args := make([]ast.Expr, len(e.Args))
		for i, a := range e.Args {
			args[i] = transformExpr(a, names, shadowed)
		}
		return &ast.CallExpr{Position: e.Position, Callee: transformExpr(e.Callee, names, shadowed), Args: args, Optional: e.Optional}
	case *ast.MemberExpr:
		return &ast.MemberExpr{
			Position: e.Position,
			Object:   transformExpr(e.Object, names, shadowed),
			Property: transformExpr(e.Property, names, shadowed),
			Computed: e.Computed,
			Optional: e.Optional,
		}
	case *ast.BinaryExpr:
		return &ast.BinaryExpr{Position: e.Position, Left: transformExpr(e.Left, names, shadowed), Op: e.Op, Right: transformExpr(e.Right, names, shadowed)}
	case *ast.UnaryExpr:
		return &ast.UnaryExpr{Position: e.Position, Op: e.Op, Arg: transformExpr(e.Arg, names, shadowed), Postfix: e.Postfix}
	case *ast.ConditionalExpr:
		return &ast.ConditionalExpr{
			Position:   e.Position,
			Test:       transformExpr(e.Test, names, shadowed),
			Consequent: transformExpr(e.Consequent, names, shadowed),
			Alternate:  transformExpr(e.Alternate, names, shadowed),
		}
	case *ast.TypeAssertion:
		return &ast.TypeAssertion{Position: e.Position, Expr: transformExpr(e.Expr, names, shadowed), TypeRef: e.TypeRef}
	case *ast.ObjectExpr:
		props := make([]*ast.ObjectProp, len(e.Properties))
		for i, p := range e.Properties {
			if p == nil {
				continue
			}
			cp := *p
			cp.Value = transformExpr(p.Value, names, shadowed)
			props[i] = &cp
		}
		return &ast.ObjectExpr{Position: e.Position, Properties: props}
	case *ast.ArrayExpr:
		els := make([]ast.Expr, len(e.Elements))
		for i, el := range e.Elements {
			els[i] = transformExpr(el, names, shadowed)
		}
		return &ast.ArrayExpr{Position: e.Position, Elements: els}
	case *ast.TemplateExpr:
		parts := make([]ast.Expr, len(e.Parts))
		for i, p := range e.Parts {
			parts[i] = transformExpr(p, names, shadowed)
		}
		return &ast.TemplateExpr{Position: e.Position, Parts: parts, Raw: append([]string(nil), e.Raw...)}
	case *ast.ArrowFn:
		next := make(map[string]bool, len(shadowed)+len(e.Params))
		for k := range shadowed {
			next[k] = true
		}
		params := make([]*ast.Param, len(e.Params))
		for i, p := range e.Params {
			cp := *p
			cp.Default = transformExpr(p.Default, names, shadowed)
			params[i] = &cp
			if p.Name != "" && p.Pattern == "" {
				next[p.Name] = true
			}
		}
		body := make([]ast.Stmt, len(e.Body))
		for i, s := range e.Body {
			body[i] = transformStmt(s, names, next)
		}
		return &ast.ArrowFn{Position: e.Position, Params: params, Body: body, Expression: e.Expression, Async: e.Async}
	case *ast.JSXElement:
		return transformJSXElement(e, names, shadowed)
	case *ast.JSXFragment:
		return &ast.JSXFragment{Position: e.Position, Children: transformJSXChildren(e.Children, names, shadowed)}
	}
	return expr
}

func transformStmt(stmt ast.Stmt, names map[string]ast.Expr, shadowed map[string]bool) ast.Stmt {
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		return &ast.ReturnStmt{Position: s.Position, Value: transformExpr(s.Value, names, shadowed)}
	case *ast.ExprStmt:
		return &ast.ExprStmt{Position: s.Position, Expression: transformExpr(s.Expression, names, shadowed)}
	case *ast.BlockStmt:
		body := make([]ast.Stmt, len(s.Body))
		for i, b := range s.Body {
			body[i] = transformStmt(b, names, shadowed)
		}
		return &ast.BlockStmt{Position: s.Position, Body: body}
	}
	return stmt
}

func transformJSXElement(el *ast.JSXElement, names map[string]ast.Expr, shadowed map[string]bool) *ast.JSXElement {
	out := &ast.JSXElement{Position: el.Position, Children: transformJSXChildren(el.Children, names, shadowed)}
	if el.Opening != nil {
		opening := *el.Opening
		opening.Attributes = make([]*ast.JSXAttr, len(el.Opening.Attributes))
		for i, a := range el.Opening.Attributes {
			if a == nil {
				continue
			}
			cp := *a
			cp.Value = transformExpr(a.Value, names, shadowed)
			opening.Attributes[i] = &cp
		}
		out.Opening = &opening
	}
	if el.Closing != nil {
		closing := *el.Closing
		out.Closing = &closing
	}
	return out
}

func transformJSXChildren(children []ast.JSXChild, names map[string]ast.Expr, shadowed map[string]bool) []ast.JSXChild {
	out := make([]ast.JSXChild, len(children))
	for i, child := range children {
		switch c := child.(type) {
		case *ast.JSXText:
			out[i] = &ast.JSXText{Value: c.Value}
		case *ast.JSXExprContainer:
			out[i] = &ast.JSXExprContainer{Expression: transformExpr(c.Expression, names, shadowed)}
		case *ast.JSXElementChild:
			out[i] = &ast.JSXElementChild{Element: transformJSXElement(c.Element, names, shadowed)}
		case *ast.JSXFragmentChild:
			out[i] = &ast.JSXFragmentChild{Fragment: &ast.JSXFragment{
				Position: c.Fragment.Position,
				Children: transformJSXChildren(c.Fragment.Children, names, shadowed),
			}}
		default:
			out[i] = child
		}
	}
	return out
}
