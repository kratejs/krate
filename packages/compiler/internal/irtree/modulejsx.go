package irtree

import (
	"strconv"

	"github.com/kratejs/krate/packages/compiler/ast"
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
	if b.declaredInComponent(name) || !containsJSXNode(init) {
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
	if depth > 8 {
		return nil
	}
	switch e := unwrapTypeWrappers(expr).(type) {
	case *ast.Identifier:
		init, ok := b.moduleJSXInit(e.Name)
		if !ok {
			return nil
		}
		if inner := b.resolveModuleJSX(init, depth+1); inner != nil {
			return inner
		}
		return init
	case *ast.MemberExpr:
		if e.Optional {
			return nil
		}
		obj := b.resolveModuleJSX(e.Object, depth+1)
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
