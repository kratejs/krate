package irtree

import (
	"sort"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// mapCallParts returns the `.map(callback)` call and its receiver.
func mapCallParts(expr ast.Expr) (*ast.CallExpr, *ast.MemberExpr, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil, nil, false
	}
	mem, ok := call.Callee.(*ast.MemberExpr)
	if !ok || mem.Computed {
		return nil, nil, false
	}
	prop, ok := mem.Property.(*ast.Identifier)
	if !ok || prop.Name != "map" {
		return nil, nil, false
	}
	return call, mem, true
}

// isFlatMapCall reports a `.flatMap(callback)` expression.
func isFlatMapCall(expr ast.Expr) bool {
	_, _, ok := flatMapCallParts(expr)
	return ok
}

// flatMapCallParts returns the `.flatMap(callback)` call and its receiver.
func flatMapCallParts(expr ast.Expr) (*ast.CallExpr, *ast.MemberExpr, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil, nil, false
	}
	mem, ok := call.Callee.(*ast.MemberExpr)
	if !ok || mem.Computed {
		return nil, nil, false
	}
	prop, ok := mem.Property.(*ast.Identifier)
	if !ok || prop.Name != "flatMap" {
		return nil, nil, false
	}
	return call, mem, true
}

// resolveMapCallback resolves the callback passed to .map() to an inline arrow
// body. Named local functions and local const-arrows are supported so a known
// list renders on the server exactly as an inline callback would.
func (b *builder) resolveMapCallback(expr ast.Expr) *ast.ArrowFn {
	switch e := expr.(type) {
	case *ast.ArrowFn:
		return e
	case *ast.Identifier:
		if b.localFnBody != nil {
			if fn := findLocalFunction(e.Name, b.localFnBody); fn != nil {
				return &ast.ArrowFn{
					Position: fn.Position,
					Params:   fn.Params,
					Body:     fn.Body,
					Async:    fn.Async,
					Function: true,
				}
			}
			if arrow := findLocalConstFn(e.Name, b.localFnBody); arrow != nil {
				return arrow
			}
		}
	}
	return nil
}

// mapCallbackBindings binds the callback parameters to one known array item.
// Object/array destructuring, defaults, rest parameters, the index, and the
// source array are all represented as AST expressions, ready for substitution.
func mapCallbackBindings(arrow *ast.ArrowFn, elem ast.Expr, index int, arr *ast.ArrayExpr) map[string]ast.Expr {
	bindings := make(map[string]ast.Expr, len(arrow.Params)+2)
	if elem == nil {
		elem = undefinedExpr()
	}
	args := []ast.Expr{
		elem,
		&ast.Literal{Kind: ast.NumberLit, Value: itoa(index)},
		arr,
	}
	for i, p := range arrow.Params {
		if p == nil {
			continue
		}
		val := undefinedExpr()
		if i < len(args) && args[i] != nil {
			val = cloneExpr(args[i])
		}
		if p.IsRest {
			if p.Name != "" {
				bindings[p.Name] = &ast.ArrayExpr{Elements: cloneExprList(args[i:])}
			}
			continue
		}
		if p.Target != nil {
			bindPatternTarget(p.Target, val, bindings)
			continue
		}
		if p.Pattern != "" {
			bindPatternString(p.Pattern, val, bindings)
			continue
		}
		if p.Name == "" {
			continue
		}
		if p.Default != nil && isUndefinedValueExpr(val) {
			val = substituteBindings(p.Default, bindings)
		}
		bindings[p.Name] = val
	}
	return bindings
}

// bindPatternTarget binds a parser cover-grammar target (ObjectExpr/ArrayExpr,
// with `=` defaults and `...` rests) to an expression value.
func bindPatternTarget(target, value ast.Expr, out map[string]ast.Expr) {
	if target == nil {
		return
	}
	switch t := target.(type) {
	case *ast.Identifier:
		if t.Name != "" {
			out[t.Name] = cloneExpr(value)
		}
	case *ast.BinaryExpr:
		if t.Op == "=" {
			if isUndefinedValueExpr(value) {
				value = substituteBindings(t.Right, out)
			}
			bindPatternTarget(t.Left, value, out)
		}
	case *ast.UnaryExpr:
		if t.Op == "..." {
			bindPatternTarget(t.Arg, value, out)
		}
	case *ast.ParenExpr:
		bindPatternTarget(t.Expr, value, out)
	case *ast.TypeAssertion:
		bindPatternTarget(t.Expr, value, out)
	case *ast.ObjectExpr:
		used := map[string]bool{}
		var rest ast.Expr
		for _, p := range t.Properties {
			if p == nil {
				continue
			}
			if p.Spread {
				rest = p.Value
				continue
			}
			key := p.Key
			if key == "" {
				if id, ok := p.Value.(*ast.Identifier); ok {
					key = id.Name
				}
			}
			if key == "" {
				continue
			}
			used[key] = true
			child := memberASTOf(value, &ast.Identifier{Name: key})
			if child == nil {
				child = undefinedExpr()
			}
			bindPatternTarget(p.Value, child, out)
		}
		if rest != nil {
			bindPatternTarget(rest, objectRest(value, used), out)
		}
	case *ast.ArrayExpr:
		for i, el := range t.Elements {
			if el == nil {
				continue
			}
			if u, ok := el.(*ast.UnaryExpr); ok && u.Op == "..." {
				rest := arrayRest(value, i)
				bindPatternTarget(u.Arg, rest, out)
				continue
			}
			child := arrayElement(value, i)
			bindPatternTarget(el, child, out)
		}
	}
}

// bindPatternString binds a VarDecl pattern source string. The AST target is
// preferred; this is a conservative fallback that still binds the common
// `{ a, b }` and `[a, b]` forms.
func bindPatternString(pattern string, value ast.Expr, out map[string]ast.Expr) {
	pattern = trimPattern(pattern)
	if pattern == "" {
		return
	}
	names := destructuredParamNames(pattern)
	if len(names) == 0 {
		return
	}
	if pattern[0] == '[' {
		for i, name := range names {
			if name == "" {
				continue
			}
			out[name] = arrayElement(value, i)
		}
		return
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		child := memberASTOf(value, &ast.Identifier{Name: name})
		if child == nil {
			child = undefinedExpr()
		}
		out[name] = child
	}
}

func trimPattern(pattern string) string {
	for len(pattern) > 0 && (pattern[0] == ' ' || pattern[0] == '\t' || pattern[0] == '\n' || pattern[0] == '\r') {
		pattern = pattern[1:]
	}
	for len(pattern) > 0 {
		last := pattern[len(pattern)-1]
		if last != ' ' && last != '\t' && last != '\n' && last != '\r' {
			break
		}
		pattern = pattern[:len(pattern)-1]
	}
	return pattern
}

func arrayElement(value ast.Expr, index int) ast.Expr {
	if arr, ok := value.(*ast.ArrayExpr); ok && index >= 0 && index < len(arr.Elements) && arr.Elements[index] != nil {
		return cloneExpr(arr.Elements[index])
	}
	return undefinedExpr()
}

func arrayRest(value ast.Expr, from int) ast.Expr {
	if arr, ok := value.(*ast.ArrayExpr); ok && from >= 0 && from < len(arr.Elements) {
		return &ast.ArrayExpr{Elements: cloneExprList(arr.Elements[from:])}
	}
	return &ast.ArrayExpr{}
}

func objectRest(value ast.Expr, used map[string]bool) ast.Expr {
	obj, ok := value.(*ast.ObjectExpr)
	if !ok {
		return &ast.ObjectExpr{}
	}
	out := &ast.ObjectExpr{Position: obj.Position}
	for _, p := range obj.Properties {
		if p == nil || p.Spread || used[p.Key] {
			continue
		}
		out.Properties = append(out.Properties, &ast.ObjectProp{
			Key:       p.Key,
			Value:     cloneExpr(p.Value),
			Shorthand: p.Shorthand,
			Method:    p.Method,
		})
	}
	return out
}

func undefinedExpr() ast.Expr {
	return &ast.Literal{Kind: ast.NullLit, Value: "undefined"}
}

func isUndefinedValueExpr(expr ast.Expr) bool {
	if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == "void" {
		return true
	}
	lit, ok := expr.(*ast.Literal)
	if !ok {
		return false
	}
	return lit.Kind == ast.NullLit && lit.Value == "undefined"
}

func cloneExprList(exprs []ast.Expr) []ast.Expr {
	out := make([]ast.Expr, len(exprs))
	for i, e := range exprs {
		if e != nil {
			out[i] = cloneExpr(e)
		}
	}
	return out
}

// substituteBindings applies parameter/local bindings to an AST expression.
// Root bindings such as `item` are applied last so destructured aliases are not
// accidentally rewritten inside their own replacements.
// staticMapCallbackBody accepts only the callback shapes the static list path
// can represent faithfully: an expression body or a block made of const
// declarations followed by a single final return. Early returns, let/var
// mutation, loops, and async callbacks stay on the runtime list path.
func staticMapCallbackBody(arrow *ast.ArrowFn) (ast.Expr, bool) {
	if arrow == nil || arrow.Async {
		return nil, false
	}
	if arrow.Expression {
		if len(arrow.Body) != 1 {
			return nil, false
		}
		switch s := arrow.Body[0].(type) {
		case *ast.ReturnStmt:
			return s.Value, s.Value != nil
		case *ast.ExprStmt:
			return s.Expression, true
		}
		return nil, false
	}
	var body ast.Expr
	for i, stmt := range arrow.Body {
		switch s := stmt.(type) {
		case *ast.VarStmt:
			if s.Kind != ast.VarConst {
				return nil, false
			}
			for _, d := range s.Decls {
				if d == nil || d.Init == nil {
					return nil, false
				}
			}
		case *ast.ReturnStmt:
			if i != len(arrow.Body)-1 || s.Value == nil {
				return nil, false
			}
			body = s.Value
		default:
			return nil, false
		}
	}
	return body, body != nil
}

func substituteBindings(expr ast.Expr, bindings map[string]ast.Expr) ast.Expr {
	if expr == nil || len(bindings) == 0 {
		return expr
	}
	names := make([]string, 0, len(bindings))
	for name := range bindings {
		names = append(names, name)
	}
	sort.Strings(names)
	// Apply ordinary bindings first and the root callback values last. A root
	// replacement can contain object literals whose properties are themselves
	// destructured locals; keeping it last avoids rewriting those values.
	roots := map[string]bool{"item": true, "index": true, "array": true}
	sort.SliceStable(names, func(i, j int) bool {
		return !roots[names[i]] && roots[names[j]]
	})
	out := expr
	for _, name := range names {
		if name == "" || name == "props" || name == "children" {
			continue
		}
		out = substituteListParam(out, name, bindings[name])
	}
	return out
}

// substituteCallbackLocalsBound folds simple const locals declared before the
// callback's final return, including destructuring, then substitutes all
// callback parameters and locals into the returned expression.
func (b *builder) substituteCallbackLocalsBound(arrow *ast.ArrowFn, body ast.Expr, bindings map[string]ast.Expr, requireStatic bool) (ast.Expr, bool) {
	if arrow == nil || arrow.Expression {
		return substituteBindings(body, bindings), true
	}
	for _, stmt := range arrow.Body {
		if _, ok := stmt.(*ast.ReturnStmt); ok {
			break
		}
		vs, ok := stmt.(*ast.VarStmt)
		if !ok || vs.Kind != ast.VarConst {
			continue
		}
		for _, d := range vs.Decls {
			if d == nil || d.Init == nil {
				continue
			}
			init := substituteBindings(d.Init, bindings)
			if requireStatic && !b.isStaticResolvable(init) {
				return nil, false
			}
			if d.IsDestructuring {
				bindPatternString(d.Pattern, init, bindings)
				continue
			}
			if d.Name != "" {
				bindings[d.Name] = init
			}
		}
	}
	return substituteBindings(body, bindings), true
}

// buildStaticFlatMapSlots folds flatMap over a known source when the callback
// returns JSX or arrays of JSX. flatMap has no runtime list plan, so only the
// fully known case is handled here.
func (b *builder) buildStaticFlatMapSlots(expr ast.Expr, parentID string) ([]SlotNode, bool) {
	call, mem, ok := flatMapCallParts(expr)
	if !ok || b.referencesSignal(mem.Object) || b.dynamicListRows > 0 {
		return nil, false
	}
	arr := b.resolveArrayLiteralExpr(expr)
	if arr == nil {
		return nil, false
	}
	arrow := b.resolveMapCallback(call.Args[0])
	if arrow == nil {
		return nil, false
	}
	body, ok := staticMapCallbackBody(arrow)
	if !ok {
		return nil, false
	}
	id := b.assignSlotID(b.nextDynamicSlot(parentID, "list"))
	var nodes []SlotNode
	for i, elem := range arr.Elements {
		if elem == nil {
			elem = undefinedExpr()
		}
		bindings := mapCallbackBindings(arrow, elem, i, arr)
		concrete, ok := b.substituteCallbackLocalsBound(arrow, body, bindings, true)
		if !ok {
			return nil, false
		}
		nodes = append(nodes, b.exprChild(concrete, string(joinSlotIDKey(id, itoa(i))), true)...)
	}
	if len(nodes) == 0 {
		nodes = []SlotNode{&StaticHTML{HTML: ""}}
	}
	return nodes, true
}
