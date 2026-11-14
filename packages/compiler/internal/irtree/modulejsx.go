package irtree

import (
	"strconv"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// Module-level constants that hold JSX (`const sections = [{ body: <p/> }]`)
// have no folded value: JSX is markup, not a jsvalue. A component that reads
// one would render its slot empty until hydration. These helpers hand the
// constant's own AST back so the slot pipeline builds the markup (components,
// nested lists and all) the way it does for JSX written inline.

// containsJSXNode reports whether node holds a JSX element or fragment.
func containsJSXNode(node any) bool {
	found := false
	WalkAST(node, func(n any) bool {
		switch n.(type) {
		case *ast.JSXElement, *ast.JSXFragment:
			found = true
		}
		return !found
	})
	return found
}

// unwrapTypeWrappers strips parentheses and TS assertions (`as const`).
func unwrapTypeWrappers(e ast.Expr) ast.Expr {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.Expr
		case *ast.TypeAssertion:
			e = x.Expr
		default:
			return e
		}
	}
}

// moduleJSXInit returns the initializer of the module constant name when it
// holds JSX and name really refers to it here: not a component local, a
// parameter, a loop binding, a signal or a binding top-level code changed.
func (b *builder) moduleJSXInit(name string) (ast.Expr, bool) {
	return b.moduleInit(name, true)
}

func (b *builder) moduleInit(name string, needJSX bool) (ast.Expr, bool) {
	init, ok := b.moduleConstInits[name]
	if !ok || init == nil || b.isShadowed(name) || b.isSignalName(name) {
		return nil, false
	}
	if b.moduleRun != nil {
		if _, changed := b.moduleRun.Values[name]; changed {
			return nil, false
		}
		if _, blocked := b.moduleRun.Blocked[name]; blocked {
			return nil, false
		}
	}
	if b.declaredInComponent(name) || (needJSX && !b.holdsJSX(init, 0)) {
		return nil, false
	}
	return unwrapTypeWrappers(init), true
}

// declaredInComponent reports whether the component being built declares name
// as a parameter or a local, which hides a module constant of the same name.
func (b *builder) declaredInComponent(name string) bool {
	if b.currentNode != nil && b.currentNode.Fn != nil {
		for _, p := range extractParamNames(b.currentNode.Fn) {
			if p == name {
				return true
			}
		}
	}
	found := false
	WalkAST(b.localFnBody, func(n any) bool {
		if d, ok := n.(*ast.VarDecl); ok {
			if d.Name == name {
				found = true
			}
			for _, nm := range d.Names {
				if nm == name {
					found = true
				}
			}
		}
		if fn, ok := n.(*ast.FnDecl); ok && fn.Name == name {
			found = true
		}
		return !found
	})
	return found
}

// resolveModuleJSX resolves an identifier or a literal member path
// (`sections`, `cfg.node`, `rows[0].cell`) rooted at a JSX-holding module
// constant to the expression it stands for, or nil.
func (b *builder) resolveModuleJSX(expr ast.Expr, depth int) ast.Expr {
	return b.resolveModuleJSXPath(expr, depth)
}

// capturedAtUse reports whether a free identifier in node (or in a module
// constant it references) is bound differently where the node is rendered, so
// splicing the node in would read the wrong binding. Every identifier counts,
// which over-approximates and only costs a fallback to hydration.
//
// A component-local constant lives in the component's own scope, so its names
// resolve the same where it is read unless a nested binding (a map callback
// parameter) hides them; only module names must also avoid the component's
// own bindings.
func (b *builder) capturedAtUse(node any, seen map[string]bool, local bool) bool {
	captured := false
	WalkAST(node, func(n any) bool {
		id, ok := n.(*ast.Identifier)
		if !ok || captured {
			return !captured
		}
		if b.isShadowed(id.Name) {
			captured = true
			return false
		}
		if local {
			if b.declaredInComponent(id.Name) {
				return true
			}
		} else if b.isSignalName(id.Name) || b.declaredInComponent(id.Name) {
			captured = true
			return false
		}
		if init, ok := b.moduleConstInits[id.Name]; ok && init != nil && !seen[id.Name] {
			seen[id.Name] = true
			if b.capturedAtUse(init, seen, false) {
				captured = true
			}
		}
		return !captured
	})
	return captured
}

func (b *builder) resolveModuleJSXPath(expr ast.Expr, depth int) ast.Expr {
	if depth > 8 {
		return nil
	}
	switch e := unwrapTypeWrappers(expr).(type) {
	case *ast.Identifier:
		init, ok := b.localJSXInit(e.Name)
		if ok {
			if b.capturedAtUse(init, map[string]bool{}, true) {
				return nil
			}
		} else if init, ok = b.moduleJSXInit(e.Name); !ok || b.capturedAtUse(init, map[string]bool{}, false) {
			return nil
		}
		if inner := b.resolveModuleJSXPath(init, depth+1); inner != nil {
			return inner
		}
		return init
	case *ast.MemberExpr:
		if e.Optional {
			return nil
		}
		obj := b.resolveModuleJSXPath(e.Object, depth+1)
		if obj == nil {
			return nil
		}
		obj = unwrapTypeWrappers(obj)
		if arr, ok := obj.(*ast.ArrayExpr); ok && e.Computed {
			lit, ok := e.Property.(*ast.Literal)
			if !ok || lit.Kind != ast.NumberLit || arrayHasSpread(arr) {
				return nil
			}
			i, err := strconv.Atoi(lit.Value)
			if err != nil || i < 0 || i >= len(arr.Elements) {
				return nil
			}
			return arr.Elements[i]
		}
		if _, ok := obj.(*ast.ObjectExpr); ok {
			if v := memberASTOf(obj, e.Property); v != nil {
				return unwrapTypeWrappers(v)
			}
		}
	}
	return nil
}

// moduleJSXChild renders a `{name}` or `{name.path}` child that reads a
// module constant holding JSX, as if the JSX were written in place.
func (b *builder) moduleJSXChild(expr ast.Expr, parentID string, doEscape bool) ([]SlotNode, bool) {
	switch unwrapTypeWrappers(expr).(type) {
	case *ast.Identifier, *ast.MemberExpr:
	default:
		return nil, false
	}
	resolved := b.resolveModuleJSX(expr, 0)
	switch resolved.(type) {
	case *ast.JSXElement, *ast.JSXFragment, *ast.ArrayExpr, *ast.CallExpr, *ast.ConditionalExpr:
		return b.exprChild(resolved, parentID, doEscape), true
	}
	return nil, false
}

// jsxPropRead returns the call-site JSX behind a `props.X` read or a
// destructured `X` read of a prop the caller passed as JSX, or nil.
func (b *builder) jsxPropRead(expr ast.Expr) (jsxProp, bool) {
	if len(b.jsxProps) == 0 {
		return jsxProp{}, false
	}
	p, ok := b.jsxPropLookup(expr)
	return p, ok
}

func (b *builder) jsxPropLookup(expr ast.Expr) (jsxProp, bool) {
	switch e := unwrapTypeWrappers(expr).(type) {
	case *ast.Identifier:
		if b.isShadowed(e.Name) {
			return jsxProp{}, false
		}
		p, ok := b.jsxProps[e.Name]
		return p, ok
	case *ast.MemberExpr:
		obj, ok := e.Object.(*ast.Identifier)
		prop, pok := e.Property.(*ast.Identifier)
		if !ok || !pok || e.Computed || e.Optional || obj.Name != "props" || b.isShadowed("props") {
			return jsxProp{}, false
		}
		p, ok := b.jsxProps[prop.Name]
		return p, ok
	}
	return jsxProp{}, false
}

// jsxProp is a call-site prop holding JSX with the caller values it reads.
type jsxProp struct {
	expr  ast.Expr
	scope map[string]jsvalue.Value
}

// inlineJSXConstAttrs replaces attributes that read a constant holding JSX
// (`body={intro}`, `sections={sections}`) with the constant's own JSX, so they
// are handled like JSX written in the attribute.
func (b *builder) inlineJSXConstAttrs(attrs map[string]ast.Expr) {
	for name, expr := range attrs {
		switch unwrapTypeWrappers(expr).(type) {
		case *ast.Identifier, *ast.MemberExpr:
		default:
			continue
		}
		if b.isFuncReference(expr) {
			continue
		}
		if resolved := b.resolveModuleJSX(expr, 0); resolved != nil && containsJSXNode(resolved) {
			attrs[name] = resolved
		}
	}
}

// jsxAttrExprs returns the attribute expressions that hold JSX.
func jsxAttrExprs(attrs map[string]ast.Expr) []ast.Expr {
	var out []ast.Expr
	for _, e := range attrs {
		if e != nil && containsJSXNode(e) {
			out = append(out, e)
		}
	}
	return out
}

// callerScope collects the build-time values of the caller's names that exprs
// mention. JSX written in the caller's scope is rendered in the callee's, so
// these travel with it. Every identifier counts, which only adds names.
func callerScope(exprs []ast.Expr, props valueMap) map[string]jsvalue.Value {
	var out map[string]jsvalue.Value
	for _, e := range exprs {
		WalkAST(e, func(n any) bool {
			id, ok := n.(*ast.Identifier)
			if !ok {
				return true
			}
			v, known := props[id.Name]
			if !known || v.ContainsUnknown() || v.IsFunction() {
				return true
			}
			if out == nil {
				out = make(map[string]jsvalue.Value)
			}
			out[id.Name] = v
			return true
		})
	}
	return out
}

// localJSXInit returns the initializer of a constant the component declares
// at the top of its body when it holds JSX and is never reassigned.
func (b *builder) localJSXInit(name string) (ast.Expr, bool) {
	return b.localInit(name, true)
}

func (b *builder) localInit(name string, needJSX bool) (ast.Expr, bool) {
	if b.isShadowed(name) {
		return nil, false
	}
	var init ast.Expr
	for _, stmt := range b.localFnBody {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok || vs.Kind == ast.VarVar {
			continue
		}
		for _, d := range vs.Decls {
			if d != nil && !d.IsDestructuring && d.Name == name && d.Init != nil {
				init = d.Init
				if vs.Kind != ast.VarConst && reassigned(b.localFnBody, name) {
					return nil, false
				}
			}
		}
	}
	if init == nil || (needJSX && !b.holdsJSX(init, 0)) {
		return nil, false
	}
	return unwrapTypeWrappers(init), true
}

// reassigned reports whether body assigns to name or to a part of it.
func reassigned(body []ast.Stmt, name string) bool {
	root := func(e ast.Expr) string {
		for {
			switch x := e.(type) {
			case *ast.Identifier:
				return x.Name
			case *ast.MemberExpr:
				e = x.Object
			case *ast.ParenExpr:
				e = x.Expr
			default:
				return ""
			}
		}
	}
	found := false
	WalkAST(body, func(n any) bool {
		switch x := n.(type) {
		case *ast.BinaryExpr:
			switch x.Op {
			case "=", "+=", "-=", "*=", "/=", "%=", "**=", "<<=", ">>=", ">>>=", "&=", "|=", "^=", "&&=", "||=", "??=":
				if root(x.Left) == name {
					found = true
				}
			}
		case *ast.UnaryExpr:
			if (x.Op == "++" || x.Op == "--" || x.Op == "delete") && root(x.Arg) == name {
				found = true
			}
		}
		return !found
	})
	return found
}

// holdsJSX reports whether init is JSX or reads a constant that holds JSX
// (`const rows = [{ v: label }]` where label is `<b/>`).
func (b *builder) holdsJSX(init ast.Expr, depth int) bool {
	if containsJSXNode(init) {
		return true
	}
	if depth > 4 {
		return false
	}
	found := false
	WalkAST(init, func(n any) bool {
		id, ok := n.(*ast.Identifier)
		if !ok || found {
			return !found
		}
		if v, ok := b.localInit(id.Name, false); ok && b.holdsJSX(v, depth+1) {
			found = true
		} else if v, ok := b.moduleInit(id.Name, false); ok && b.holdsJSX(v, depth+1) {
			found = true
		}
		return !found
	})
	return found
}
