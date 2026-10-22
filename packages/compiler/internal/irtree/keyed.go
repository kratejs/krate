package irtree

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// Keyed lists and reactive client rendering.
//
// A dynamic `.map()` list binds at runtime through krateBindList (top level)
// or krateList (nested in rendered JSX). The map callback is compiled into a
// row render function whose item and index parameters are per-row accessors
// (`item()` / `index()`), and whose JSX reads are wrapped in thunks so a row
// updates in place when its item, index or any signal it reads changes. Rows
// are matched by the `key` of the element (or keyed Fragment) the callback
// returns. See packages/runtime/src/list.ts for the reconciliation.

// listPlan describes how a list slot binds at runtime.
type listPlan struct {
	// SourceJS is the array expression the list maps over.
	SourceJS string
	// KeyJS is `(item, index) => key` with plain values, or "" when unkeyed.
	KeyJS string
	// RenderJS is the row render function.
	RenderJS string
	// Accessor reports that RenderJS receives accessors instead of values.
	Accessor bool
	// KeyExpr is the key expression in terms of the callback parameters,
	// used to fold build-time keys for server-rendered rows.
	KeyExpr ast.Expr
	// ReturnsElement reports that the callback returns a JSX element, which
	// React expects to carry a key.
	ReturnsElement bool
	// KeyHoles reports that the callback returns elements on some paths that
	// lack a key while others have one.
	KeyHoles bool
}

// Runtime helper names emitted into hydration code (window globals).
const (
	runtimeListFn = "krateList"
	runtimeH      = "h"
)

// planMapCall analyzes `recv.map(callback)`. It returns nil when the call is
// not a list the runtime can bind (the caller keeps the whole-region
// re-render fallback).
func planMapCall(call *ast.CallExpr) *listPlan {
	if call == nil || len(call.Args) != 1 {
		return nil
	}
	mem, ok := call.Callee.(*ast.MemberExpr)
	if !ok || mem.Computed {
		return nil
	}
	if prop, ok := mem.Property.(*ast.Identifier); !ok || prop.Name != "map" {
		return nil
	}
	plan := &listPlan{SourceJS: generateExprJS(mem.Object, nil)}
	arrow, ok := call.Args[0].(*ast.ArrowFn)
	if !ok {
		// `.map(renderRow)`: plain values, rows re-render when their item changes.
		plan.RenderJS = generateExprJS(call.Args[0], nil)
		return plan
	}
	if arrow.Generator || arrow.Async || len(arrow.Params) > 3 {
		return nil
	}
	ret, prefix, ok := splitCallbackBody(arrow)
	if !ok {
		return nil
	}
	plan.ReturnsElement = returnsElement(ret)
	if key := keyExprOf(ret); key != nil {
		plan.KeyExpr = key
		plan.KeyJS = renderKeyFn(arrow, prefix, key)
	}
	plan.KeyHoles = hasKeyHole(ret)
	plan.Accessor = simpleCallbackParams(arrow)
	plan.RenderJS = renderRowFn(arrow, plan.Accessor)
	return plan
}

// splitCallbackBody returns the rendered expression of a map callback and the
// statements that precede its final return (nil for expression bodies). The
// callback must end in a return of a value.
func splitCallbackBody(arrow *ast.ArrowFn) (ast.Expr, []ast.Stmt, bool) {
	if arrow.Expression {
		e := arrowBodyExpr(arrow)
		return e, nil, e != nil
	}
	if len(arrow.Body) == 0 {
		return nil, nil, false
	}
	ret, ok := arrow.Body[len(arrow.Body)-1].(*ast.ReturnStmt)
	if !ok || ret.Value == nil {
		return nil, nil, false
	}
	return ret.Value, arrow.Body[:len(arrow.Body)-1], true
}

// simpleCallbackParams reports whether every callback parameter is a plain
// identifier (no destructuring, defaults or rest), so its uses can be
// rewritten to accessor calls.
func simpleCallbackParams(arrow *ast.ArrowFn) bool {
	if len(arrow.Params) > 2 {
		return false
	}
	for _, p := range arrow.Params {
		if p == nil || p.Name == "" || p.Pattern != "" || p.Target != nil || p.IsRest || p.Default != nil {
			return false
		}
		if strings.HasPrefix(p.Name, "{") || strings.HasPrefix(p.Name, "[") {
			return false
		}
	}
	return true
}

// keyExprOf returns the key of the JSX a map callback returns: the `key`
// attribute of an element, the key of a keyed Fragment, or a conditional
// over keyed branches. It returns nil when no key is present.
func keyExprOf(e ast.Expr) ast.Expr {
	switch v := e.(type) {
	case *ast.ParenExpr:
		return keyExprOf(v.Expr)
	case *ast.TypeAssertion:
		return keyExprOf(v.Expr)
	case *ast.JSXElement:
		return jsxKeyAttr(v)
	case *ast.JSXFragment:
		return v.Key
	case *ast.ConditionalExpr:
		c, a := keyExprOf(v.Consequent), keyExprOf(v.Alternate)
		if c == nil && a == nil {
			return nil
		}
		return &ast.ConditionalExpr{Position: v.Position, Test: v.Test, Consequent: orUndefined(c), Alternate: orUndefined(a)}
	case *ast.BinaryExpr:
		if v.Op == "&&" {
			if k := keyExprOf(v.Right); k != nil {
				return &ast.ConditionalExpr{Position: v.Position, Test: v.Left, Consequent: k, Alternate: undefinedLit()}
			}
		}
	}
	return nil
}

func orUndefined(e ast.Expr) ast.Expr {
	if e == nil {
		return undefinedLit()
	}
	return e
}

func undefinedLit() ast.Expr {
	return &ast.Literal{Kind: ast.NullLit, Value: "undefined"}
}

// jsxKeyAttr returns an element's `key` attribute value.
func jsxKeyAttr(el *ast.JSXElement) ast.Expr {
	if el == nil || el.Opening == nil {
		return nil
	}
	for _, attr := range el.Opening.Attributes {
		if attr != nil && !attr.Spread && attr.Name == "key" && attr.Value != nil {
			return attr.Value
		}
	}
	return nil
}

// returnsElement reports whether a callback result is (or may be) a JSX
// element or fragment, as opposed to text.
func returnsElement(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.ParenExpr:
		return returnsElement(v.Expr)
	case *ast.TypeAssertion:
		return returnsElement(v.Expr)
	case *ast.JSXElement, *ast.JSXFragment:
		return true
	case *ast.ConditionalExpr:
		return returnsElement(v.Consequent) || returnsElement(v.Alternate)
	case *ast.BinaryExpr:
		if v.Op == "&&" || v.Op == "||" || v.Op == "??" {
			return returnsElement(v.Right)
		}
	}
	return false
}

// hasKeyHole reports a callback result that keys some returned elements but
// not others (for example a ternary with a key on one branch only).
func hasKeyHole(e ast.Expr) bool {
	keyed, unkeyed := false, false
	var walk func(e ast.Expr)
	walk = func(e ast.Expr) {
		switch v := e.(type) {
		case *ast.ParenExpr:
			walk(v.Expr)
		case *ast.TypeAssertion:
			walk(v.Expr)
		case *ast.ConditionalExpr:
			walk(v.Consequent)
			walk(v.Alternate)
		case *ast.BinaryExpr:
			if v.Op == "&&" || v.Op == "||" || v.Op == "??" {
				walk(v.Right)
			}
		case *ast.JSXElement, *ast.JSXFragment:
			if hasAnyKey(v) {
				keyed = true
			} else {
				unkeyed = true
			}
		}
	}
	walk(e)
	return keyed && unkeyed
}

// hasAnyKey reports whether any returned JSX branch carries a key.
func hasAnyKey(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.ParenExpr:
		return hasAnyKey(v.Expr)
	case *ast.JSXElement:
		return jsxKeyAttr(v) != nil
	case *ast.JSXFragment:
		return v.Key != nil
	case *ast.ConditionalExpr:
		return hasAnyKey(v.Consequent) || hasAnyKey(v.Alternate)
	case *ast.BinaryExpr:
		return hasAnyKey(v.Right)
	}
	return false
}

// renderKeyFn renders `(item, index) => key` over plain values. A block
// callback keeps its leading statements so keys may read its locals.
func renderKeyFn(arrow *ast.ArrowFn, prefix []ast.Stmt, key ast.Expr) string {
	fn := &ast.ArrowFn{Position: arrow.Position, Params: arrow.Params}
	if len(prefix) == 0 {
		fn.Expression = true
		fn.Body = []ast.Stmt{&ast.ReturnStmt{Value: key}}
	} else {
		body := append([]ast.Stmt{}, prefix...)
		fn.Body = append(body, &ast.ReturnStmt{Value: key})
	}
	return renderArrowFn(fn, nil)
}

// renderRowFn renders the row render function. In accessor mode every use of
// the item/index parameters becomes a call of the row accessor.
func renderRowFn(arrow *ast.ArrowFn, accessor bool) string {
	fn := *arrow
	body := arrow.Body
	if accessor {
		for _, p := range arrow.Params {
			s := &listParamSubstituter{param: p.Name, elem: &ast.CallExpr{Callee: &ast.Identifier{Name: p.Name}}}
			body = s.subStmts(body)
		}
	}
	if ret, prefix, ok := splitBody(fn.Expression, body); ok {
		if accessor && len(prefix) > 0 {
			ret, prefix = inlineConstLocals(prefix, ret)
		}
		ret = reactifyRoot(ret)
		out := append([]ast.Stmt{}, prefix...)
		fn.Body = append(out, &ast.ReturnStmt{Value: ret})
		if fn.Expression && len(prefix) > 0 {
			fn.Expression = false
		}
	}
	if fn.Function {
		return renderFunctionExpr(&fn, nil)
	}
	return renderArrowFn(&fn, nil)
}

func splitBody(expression bool, body []ast.Stmt) (ast.Expr, []ast.Stmt, bool) {
	if len(body) == 0 {
		return nil, nil, false
	}
	switch last := body[len(body)-1].(type) {
	case *ast.ReturnStmt:
		if last.Value != nil {
			return last.Value, body[:len(body)-1], true
		}
	case *ast.ExprStmt:
		if expression {
			return last.Expression, body[:len(body)-1], true
		}
	}
	return nil, nil, false
}

// inlineConstLocals substitutes `const name = init` locals with a pure
// initializer into the returned expression, so values derived from the item
// stay live. Locals that cannot be inlined are kept (evaluated once when the
// row is created).
func inlineConstLocals(prefix []ast.Stmt, ret ast.Expr) (ast.Expr, []ast.Stmt) {
	type local struct {
		name string
		init ast.Expr
	}
	var locals []local
	var kept []ast.Stmt
	for _, stmt := range prefix {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok || vs.Kind != ast.VarConst || len(vs.Decls) != 1 {
			kept = append(kept, stmt)
			continue
		}
		d := vs.Decls[0]
		if d == nil || d.IsDestructuring || d.Name == "" || d.Init == nil || !isPureInit(d.Init) {
			kept = append(kept, stmt)
			continue
		}
		init := d.Init
		for _, prev := range locals {
			init = substituteListParam(init, prev.name, prev.init)
		}
		locals = append(locals, local{name: d.Name, init: init})
	}
	// A local still read by a kept statement must stay declared.
	for i := len(locals) - 1; i >= 0; i-- {
		used := false
		for _, stmt := range kept {
			if stmtMentions(stmt, locals[i].name) {
				used = true
				break
			}
		}
		if used {
			kept = append([]ast.Stmt{&ast.VarStmt{Kind: ast.VarConst, Decls: []*ast.VarDecl{{Name: locals[i].name, Init: locals[i].init}}}}, kept...)
			continue
		}
		ret = substituteListParam(ret, locals[i].name, &ast.ParenExpr{Expr: locals[i].init})
	}
	return ret, kept
}

func stmtMentions(stmt ast.Stmt, name string) bool {
	return codeContainsIdent(renderStmtJS(stmt, nil), name)
}

// isPureInit reports whether an initializer can be re-evaluated freely: no
// assignments, no allocation of mutable state that is later mutated in
// place, and no hook or factory calls.
func isPureInit(e ast.Expr) bool {
	switch v := e.(type) {
	case nil:
		return true
	case *ast.Identifier, *ast.Literal, *ast.ThisExpr, *ast.MetaProperty:
		return true
	case *ast.MemberExpr:
		return isPureInit(v.Object) && (!v.Computed || isPureInit(v.Property))
	case *ast.BinaryExpr:
		switch v.Op {
		case "=", "+=", "-=", "*=", "/=", "%=", "**=", "<<=", ">>=", ">>>=", "&=", "|=", "^=", "&&=", "||=", "??=":
			return false
		}
		return isPureInit(v.Left) && isPureInit(v.Right)
	case *ast.UnaryExpr:
		switch v.Op {
		case "++", "--", "delete", "...":
			return false
		}
		return isPureInit(v.Arg)
	case *ast.ConditionalExpr:
		return isPureInit(v.Test) && isPureInit(v.Consequent) && isPureInit(v.Alternate)
	case *ast.ParenExpr:
		return isPureInit(v.Expr)
	case *ast.TypeAssertion:
		return isPureInit(v.Expr)
	case *ast.TemplateExpr:
		for _, p := range v.Parts {
			if !isPureInit(p) {
				return false
			}
		}
		return true
	case *ast.CallExpr:
		switch callee := v.Callee.(type) {
		case *ast.Identifier:
			name := callee.Name
			if strings.HasPrefix(name, "use") || strings.HasPrefix(name, "create") {
				return false
			}
			switch name {
			case "String", "Number", "Boolean", "cn", "clsx", "cx", "twMerge", "encodeURIComponent":
			default:
				// A zero-argument call is a signal or memo read.
				if len(v.Args) != 0 {
					return false
				}
			}
		case *ast.MemberExpr:
			if !isPureMethod(callee) || !isPureInit(callee.Object) {
				return false
			}
		default:
			return false
		}
		for _, a := range v.Args {
			if _, isFn := a.(*ast.ArrowFn); isFn {
				continue
			}
			if !isPureInit(a) {
				return false
			}
		}
		return true
	}
	return false
}

func isPureMethod(m *ast.MemberExpr) bool {
	id, ok := m.Property.(*ast.Identifier)
	if !ok || m.Computed {
		return false
	}
	switch id.Name {
	case "toLowerCase", "toUpperCase", "trim", "trimStart", "trimEnd", "toFixed", "toString",
		"join", "includes", "startsWith", "endsWith", "slice", "substring", "indexOf",
		"filter", "map", "find", "some", "every", "padStart", "padEnd", "replace", "split",
		"concat", "at", "charAt", "toLocaleString", "localeCompare", "length":
		return true
	}
	return false
}

// Reactive JSX
// reactify rewrites rendered JSX so its dynamic parts are live: attribute
// values become thunks (h() binds a function prop as a reactive attribute),
// expression children become thunks (h() renders a function child as a
// reactive region), component props become getters, nested `.map()` calls
// become runtime lists and an element with a dynamic key is remounted when its
// key changes. Event handlers, refs and literals are left as they are.

func thunk(e ast.Expr) ast.Expr {
	return &ast.ArrowFn{Expression: true, Body: []ast.Stmt{&ast.ReturnStmt{Value: e}}}
}

// reactifyRoot rewrites the value a row or component returns. The root's own
// key belongs to the enclosing list and is dropped.
func reactifyRoot(e ast.Expr) ast.Expr {
	switch v := e.(type) {
	case *ast.ParenExpr:
		return &ast.ParenExpr{Position: v.Position, Expr: reactifyRoot(v.Expr)}
	case *ast.JSXElement:
		return reactifyElement(v, true)
	case *ast.JSXFragment:
		return reactifyFragment(v)
	case *ast.ConditionalExpr, *ast.BinaryExpr:
		if containsJSX(e) {
			// A branch between JSX values re-renders when its test changes.
			return thunk(reactifyExpr(e))
		}
	}
	if isStaticJSXValue(e) {
		return e
	}
	// Text rows (`items.map(t => t.label)`) and other values stay live too.
	return thunk(reactifyExpr(e))
}

// reactifyExpr rewrites JSX nested in an expression.
func reactifyExpr(e ast.Expr) ast.Expr {
	switch v := e.(type) {
	case *ast.JSXElement:
		return reactifyElement(v, false)
	case *ast.JSXFragment:
		return reactifyFragment(v)
	case *ast.ParenExpr:
		return &ast.ParenExpr{Position: v.Position, Expr: reactifyExpr(v.Expr)}
	case *ast.TypeAssertion:
		return reactifyExpr(v.Expr)
	case *ast.ConditionalExpr:
		return &ast.ConditionalExpr{Position: v.Position, Test: v.Test, Consequent: reactifyExpr(v.Consequent), Alternate: reactifyExpr(v.Alternate)}
	case *ast.BinaryExpr:
		if v.Op == "&&" || v.Op == "||" || v.Op == "??" {
			return &ast.BinaryExpr{Position: v.Position, Left: v.Left, Op: v.Op, Right: reactifyExpr(v.Right)}
		}
	case *ast.ArrayExpr:
		out := make([]ast.Expr, len(v.Elements))
		for i, el := range v.Elements {
			if el != nil {
				out[i] = reactifyExpr(el)
			}
		}
		return &ast.ArrayExpr{Position: v.Position, Elements: out}
	case *ast.CallExpr:
		if isMapCall(v) {
			if list := nestedListCall(v); list != nil {
				return list
			}
		}
	}
	return e
}

// nestedListCall lowers a `.map()` inside rendered JSX to krateList(...).
func nestedListCall(call *ast.CallExpr) ast.Expr {
	plan := planMapCall(call)
	if plan == nil {
		return nil
	}
	return listCallExpr(plan.SourceJS, plan.KeyJS, plan.RenderJS, plan.Accessor)
}

// listCallExpr builds `krateList(() => source, keyFn, render, mode)` from JS.
func listCallExpr(sourceJS, keyJS, renderJS string, accessor bool) ast.Expr {
	key := "null"
	if keyJS != "" {
		key = keyJS
	}
	mode := "0"
	if accessor {
		mode = "1"
	}
	return rawJSExpr(runtimeListFn + "(()=>(" + sourceJS + ")," + key + "," + renderJS + "," + mode + ")")
}

// rawJSExpr carries pre-rendered JS through the AST: generateExprJS prints an
// identifier's name verbatim. Only reactified output (which goes straight to
// codegen) contains these.
func rawJSExpr(js string) ast.Expr {
	return &ast.Identifier{Name: js}
}

func containsJSX(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.JSXElement, *ast.JSXFragment:
		return true
	case *ast.ParenExpr:
		return containsJSX(v.Expr)
	case *ast.ConditionalExpr:
		return containsJSX(v.Consequent) || containsJSX(v.Alternate)
	case *ast.BinaryExpr:
		return containsJSX(v.Left) || containsJSX(v.Right)
	case *ast.ArrayExpr:
		for _, el := range v.Elements {
			if el != nil && containsJSX(el) {
				return true
			}
		}
	case *ast.CallExpr:
		return isMapCall(v)
	}
	return false
}

// isStaticJSXValue reports values that never change after creation and need
// no thunk: literals, functions and static template strings.
func isStaticJSXValue(e ast.Expr) bool {
	switch v := e.(type) {
	case nil:
		return true
	case *ast.Literal:
		return true
	case *ast.ArrowFn:
		return true
	case *ast.TemplateExpr:
		return len(v.Parts) == 0
	case *ast.ParenExpr:
		return isStaticJSXValue(v.Expr)
	}
	return false
}

// linkLiteralAttrs are <Link> props the Link lowering inspects as literals.
var linkLiteralAttrs = map[string]bool{"prefetch": true, "replace": true, "external": true, "scroll": true}

func reactifyElement(el *ast.JSXElement, root bool) ast.Expr {
	if el == nil || el.Opening == nil {
		return el
	}
	name := el.Opening.Name
	if !root {
		if key := jsxKeyAttr(el); key != nil && !isStaticJSXValue(key) {
			// <Profile key={id} />: a new key remounts the element (cleanups,
			// then a fresh render with fresh state).
			body := renderArrowFn(&ast.ArrowFn{Expression: true, Body: []ast.Stmt{&ast.ReturnStmt{Value: reactifyElement(el, true)}}}, nil)
			return listCallExpr("["+generateExprJS(key, nil)+"]", "(k)=>k", body, true)
		}
	}
	if isComponentName(name) && name != "Link" {
		return reactifyComponent(el)
	}
	opening := &ast.JSXOpening{Name: name, SelfClosing: el.Opening.SelfClosing}
	for _, attr := range el.Opening.Attributes {
		if attr == nil {
			continue
		}
		if attr.Spread || attr.Value == nil {
			opening.Attributes = append(opening.Attributes, attr)
			continue
		}
		switch {
		case attr.Name == "key":
			continue
		case attr.Name == "ref", attr.Name == "dangerouslySetInnerHTML", isOnEvent(attr.Name),
			name == "Link" && linkLiteralAttrs[attr.Name], isStaticJSXValue(attr.Value):
			opening.Attributes = append(opening.Attributes, attr)
		default:
			opening.Attributes = append(opening.Attributes, &ast.JSXAttr{Position: attr.Position, Name: attr.Name, Value: thunk(attr.Value)})
		}
	}
	return &ast.JSXElement{Position: el.Position, Opening: opening, Children: reactifyChildren(el.Children), Closing: el.Closing}
}

func isComponentName(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

func reactifyFragment(f *ast.JSXFragment) ast.Expr {
	return &ast.JSXFragment{Position: f.Position, Children: reactifyChildren(f.Children)}
}

func reactifyChildren(children []ast.JSXChild) []ast.JSXChild {
	out := make([]ast.JSXChild, 0, len(children))
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXExprContainer:
			if c.Expression == nil {
				out = append(out, c)
				continue
			}
			out = append(out, &ast.JSXExprContainer{Expression: reactifyChildExpr(c.Expression)})
		case *ast.JSXElementChild:
			if r, ok := reactifyElement(c.Element, false).(*ast.JSXElement); ok {
				out = append(out, &ast.JSXElementChild{Element: r})
			} else {
				out = append(out, &ast.JSXExprContainer{Expression: reactifyElement(c.Element, false)})
			}
		case *ast.JSXFragmentChild:
			if r, ok := reactifyFragment(c.Fragment).(*ast.JSXFragment); ok {
				out = append(out, &ast.JSXFragmentChild{Fragment: r})
			}
		default:
			out = append(out, child)
		}
	}
	return out
}

// reactifyChildExpr rewrites a `{expr}` child: JSX and lists render directly,
// anything else becomes a reactive region.
func reactifyChildExpr(e ast.Expr) ast.Expr {
	switch v := e.(type) {
	case *ast.Literal:
		return v
	case *ast.JSXElement, *ast.JSXFragment:
		return reactifyExpr(v)
	case *ast.CallExpr:
		if isMapCall(v) {
			if list := nestedListCall(v); list != nil {
				return list
			}
		}
	}
	return thunk(reactifyExpr(e))
}

// reactifyComponent lowers <C a={x}>children</C> to h(C, {get a(){return x}},
// ...children) so the child reads live values through its props.
func reactifyComponent(el *ast.JSXElement) ast.Expr {
	props := &ast.ObjectExpr{Position: el.Position}
	for _, attr := range el.Opening.Attributes {
		if attr == nil {
			continue
		}
		if attr.Spread {
			props.Properties = append(props.Properties, &ast.ObjectProp{Spread: true, Value: attr.Value})
			continue
		}
		if attr.Name == "key" {
			continue
		}
		key := attr.Name
		if key == "className" {
			key = "class"
		}
		var value ast.Expr = &ast.Literal{Kind: ast.BoolLit, Value: "true"}
		if attr.Value != nil {
			value = attr.Value
		}
		switch value.(type) {
		case *ast.Literal, *ast.ArrowFn:
			props.Properties = append(props.Properties, &ast.ObjectProp{Key: key, Value: value, KeyQuoted: !isJSIdentifier(key)})
			continue
		case *ast.JSXElement, *ast.JSXFragment:
			props.Properties = append(props.Properties, &ast.ObjectProp{Key: key, Value: reactifyExpr(value), KeyQuoted: !isJSIdentifier(key)})
			continue
		}
		getter := &ast.ArrowFn{Function: true, Expression: true, Body: []ast.Stmt{&ast.ReturnStmt{Value: value}}}
		props.Properties = append(props.Properties, &ast.ObjectProp{Key: key, Value: getter, Method: true, Getter: true, KeyQuoted: !isJSIdentifier(key)})
	}
	args := []ast.Expr{&ast.Identifier{Name: el.Opening.Name}}
	if len(props.Properties) > 0 {
		args = append(args, props)
	} else {
		args = append(args, &ast.Literal{Kind: ast.NullLit, Value: "null"})
	}
	for _, child := range reactifyChildren(el.Children) {
		switch c := child.(type) {
		case *ast.JSXText:
			args = append(args, &ast.Literal{Kind: ast.StringLit, Value: c.Value})
		case *ast.JSXExprContainer:
			if c.Expression != nil {
				args = append(args, c.Expression)
			}
		case *ast.JSXElementChild:
			args = append(args, c.Element)
		case *ast.JSXFragmentChild:
			args = append(args, c.Fragment)
		}
	}
	return &ast.CallExpr{Position: el.Position, Callee: &ast.Identifier{Name: runtimeH}, Args: args}
}

// Reactive component functions
// Component functions rendered by the runtime (list rows, keyed remounts,
// components used inside them) are emitted with reactive JSX: their returned
// markup reads props through the getters their callers pass, so a prop change
// updates the component in place. Simple destructured props
// (`function Row({ item, onRemove })`) are rewritten to live props reads.

const reactivePropsParam = "__kp"

// ReactiveComponentFnJS renders a component function with reactive JSX, or
// returns "" when fn is not a component (an uppercase function that returns
// JSX).
func ReactiveComponentFnJS(fn *ast.FnDecl) string {
	if r := reactiveComponentFn(fn); r != nil {
		return renderStmtJS(r, nil)
	}
	return ""
}

// renderFnDeclJS renders a hoisted function declaration, using the reactive
// form for components.
func renderFnDeclJS(fn *ast.FnDecl, signals map[string]ast.Expr) string {
	if r := reactiveComponentFn(fn); r != nil {
		return renderStmtJS(r, signals)
	}
	return renderStmtJS(fn, signals)
}

func reactiveComponentFn(fn *ast.FnDecl) *ast.FnDecl {
	if fn == nil || !isComponentName(fn.Name) || fn.Generator || fn.Async || !fnReturnsJSX(fn.Body) {
		return nil
	}
	cp := *fn
	body := fn.Body
	if len(fn.Params) == 1 && fn.Params[0] != nil {
		if names, ok := simplePropsPattern(fn.Params[0]); ok && !assignsAny(body, names) {
			for _, n := range names {
				var repl ast.Expr = &ast.MemberExpr{Object: &ast.Identifier{Name: reactivePropsParam}, Property: &ast.Identifier{Name: n.key}}
				if n.def != nil {
					repl = &ast.ParenExpr{Expr: &ast.ConditionalExpr{
						Test:       &ast.BinaryExpr{Left: repl, Op: "===", Right: undefinedLit()},
						Consequent: n.def,
						Alternate:  repl,
					}}
				}
				s := &listParamSubstituter{param: n.local, elem: repl}
				body = s.subStmts(body)
			}
			cp.Params = []*ast.Param{{Position: fn.Params[0].Position, Name: reactivePropsParam}}
		}
	}
	cp.Body = reactifyReturns(inlineComponentLocals(body))
	return &cp
}

// fnReturnsJSX reports whether a function body returns JSX at top level.
func fnReturnsJSX(body []ast.Stmt) bool {
	found := false
	walkReturns(body, func(r *ast.ReturnStmt) {
		if r.Value != nil && containsJSX(r.Value) {
			found = true
		}
	})
	return found
}

// walkReturns visits the return statements of a function body, not entering
// nested functions.
func walkReturns(stmts []ast.Stmt, fn func(*ast.ReturnStmt)) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.ReturnStmt:
			fn(s)
		case *ast.IfStmt:
			walkReturns(s.Consequent, fn)
			walkReturns(s.Alternate, fn)
		case *ast.BlockStmt:
			walkReturns(s.Body, fn)
		case *ast.SwitchStmt:
			for _, c := range s.Cases {
				if c != nil {
					walkReturns(c.Body, fn)
				}
			}
		case *ast.TryStmt:
			walkReturns(s.Body, fn)
			if s.Catch != nil {
				walkReturns(s.Catch.Body, fn)
			}
			walkReturns(s.Finally, fn)
		}
	}
}

// reactifyReturns rewrites the JSX returned at top level of a function body.
func reactifyReturns(stmts []ast.Stmt) []ast.Stmt {
	out := make([]ast.Stmt, len(stmts))
	for i, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.ReturnStmt:
			if s.Value != nil && containsJSX(s.Value) {
				out[i] = &ast.ReturnStmt{Position: s.Position, Value: reactifyRoot(s.Value)}
				continue
			}
		case *ast.IfStmt:
			cp := *s
			cp.Consequent = reactifyReturns(s.Consequent)
			cp.Alternate = reactifyReturns(s.Alternate)
			out[i] = &cp
			continue
		case *ast.BlockStmt:
			out[i] = &ast.BlockStmt{Position: s.Position, Body: reactifyReturns(s.Body)}
			continue
		case *ast.SwitchStmt:
			cp := *s
			cp.Cases = make([]*ast.CaseClause, len(s.Cases))
			for j, c := range s.Cases {
				if c != nil {
					cc := *c
					cc.Body = reactifyReturns(c.Body)
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

// inlineComponentLocals inlines pure top-level const locals into the
// returned JSX (see inlineConstLocals) when every other statement leaves them
// alone, so values derived from props stay live.
func inlineComponentLocals(body []ast.Stmt) []ast.Stmt {
	if len(body) == 0 {
		return body
	}
	last, ok := body[len(body)-1].(*ast.ReturnStmt)
	if !ok || last.Value == nil {
		return body
	}
	ret, kept := inlineConstLocals(body[:len(body)-1], last.Value)
	return append(kept, &ast.ReturnStmt{Position: last.Position, Value: ret})
}

type propsBinding struct {
	key   string
	local string
	def   ast.Expr
}

// simplePropsPattern returns the bindings of a `{ a, b: c, d = 1 }` props
// parameter. Rest elements, nested patterns and computed keys are rejected.
func simplePropsPattern(p *ast.Param) ([]propsBinding, bool) {
	obj, ok := p.Target.(*ast.ObjectExpr)
	if !ok || p.IsRest {
		return nil, false
	}
	var out []propsBinding
	for _, prop := range obj.Properties {
		if prop == nil || prop.Spread || prop.Computed || prop.Method || prop.Key == "" {
			return nil, false
		}
		switch v := prop.Value.(type) {
		case *ast.Identifier:
			out = append(out, propsBinding{key: prop.Key, local: v.Name})
		case *ast.BinaryExpr:
			id, ok := v.Left.(*ast.Identifier)
			if v.Op != "=" || !ok {
				return nil, false
			}
			out = append(out, propsBinding{key: prop.Key, local: id.Name, def: v.Right})
		default:
			return nil, false
		}
	}
	return out, len(out) > 0
}

// assignsAny reports whether the body may reassign any destructured local.
func assignsAny(body []ast.Stmt, names []propsBinding) bool {
	js := ""
	for _, s := range body {
		js += renderStmtJS(s, nil) + "\n"
	}
	for _, n := range names {
		if assignsIdent(js, n.local) {
			return true
		}
	}
	return false
}

// assignsIdent is a conservative token scan for `name =`, `name +=`, `name++`
// and `++name` in generated JS (comparisons and arrows are not assignments).
func assignsIdent(js, name string) bool {
	for i := 0; i < len(js); {
		j := strings.Index(js[i:], name)
		if j < 0 {
			return false
		}
		start := i + j
		end := start + len(name)
		i = end
		if start > 0 && (isIdentChar(js[start-1]) || js[start-1] == '.') {
			continue
		}
		if end < len(js) && isIdentChar(js[end]) {
			continue
		}
		if start >= 2 && (js[start-2:start] == "++" || js[start-2:start] == "--") {
			return true
		}
		k := end
		for k < len(js) && js[k] == ' ' {
			k++
		}
		rest := js[k:]
		if strings.HasPrefix(rest, "++") || strings.HasPrefix(rest, "--") {
			return true
		}
		if strings.HasPrefix(rest, "==") || strings.HasPrefix(rest, "=>") {
			continue
		}
		if strings.HasPrefix(rest, "=") {
			return true
		}
		if len(rest) >= 2 && rest[1] == '=' && strings.ContainsRune("+-*/%&|^", rune(rest[0])) {
			return true
		}
	}
	return false
}
