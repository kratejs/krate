package irtree

import (
	"reflect"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// checkServerComponentCall validates a call site of an async (server)
// component from a page that is otherwise built at build time. Such a call
// becomes its own request-time region whose props are fixed in the shell, so
// every prop must be known at build time and children cannot be passed.
// Reporting this is what keeps an unknown prop from silently rendering as an
// empty value per request.
func (b *builder) checkServerComponentCall(el *ast.JSXElement, attrs map[string]ast.Expr) {
	name := el.Opening.Name
	hint := "make the page component `async` so the whole page renders per request, or pass literal / module-constant props"
	for _, attr := range el.Opening.Attributes {
		if attr != nil && attr.Spread {
			b.serverComponentErr(el.Position, "server component <"+name+"> cannot receive spread props from a statically built page", hint)
			return
		}
	}
	for pn, pe := range attrs {
		if operandLeaks(pe, b.sigMap(), b.localProps) {
			b.serverComponentErr(el.Position, "prop `"+pn+"` of server component <"+name+"> is not known at build time", hint)
		}
	}
	for _, c := range el.Children {
		if t, ok := c.(*ast.JSXText); ok && strings.TrimSpace(t.Value) == "" {
			continue
		}
		b.serverComponentErr(el.Position, "server component <"+name+"> cannot receive children from a statically built page", hint)
		return
	}
}

func (b *builder) serverComponentErr(pos ast.Pos, msg, hint string) {
	b.buildErrs = append(b.buildErrs, b.diagAt(pos, diag.SeverityError, msg, hint).WithCode(diag.CodeServerComponentProps))
}

// reachesRuntimeComponent reports whether fn renders, directly or through
// other local components, a runtime-tier component (an async server component
// or an @runtime component). Results are memoized per builder.
func (b *builder) reachesRuntimeComponent(fn *ast.FnDecl) bool {
	if fn == nil || b.ann == nil {
		return false
	}
	if b.runtimeReach == nil {
		b.runtimeReach = map[*ast.FnDecl]bool{}
	}
	if v, ok := b.runtimeReach[fn]; ok {
		return v
	}
	// Mark in progress as false so recursive components terminate.
	b.runtimeReach[fn] = false
	found := false
	for _, name := range jsxElementNames(fn.Body) {
		if b.ann.ComponentTiers[name] == TierRuntime {
			found = true
			break
		}
		if child := b.functions[name]; child != nil && child != fn && b.reachesRuntimeComponent(child) {
			found = true
			break
		}
	}
	b.runtimeReach[fn] = found
	return found
}

// jsxElementNames returns the tag names of every JSX element anywhere under
// node (statements, nested functions, attributes and children).
func jsxElementNames(node any) []string {
	var names []string
	walkASTNodes(reflect.ValueOf(node), func(n any) {
		if el, ok := n.(*ast.JSXElement); ok && el.Opening != nil {
			names = append(names, el.Opening.Name)
		}
	})
	return names
}

var astPosType = reflect.TypeOf(ast.Pos{})

// walkASTNodes visits every pointer node reachable from v. The AST is a tree
// of pointers, slices and interfaces, so a reflective walk covers every node
// type without a hand-written visitor per construct.
func walkASTNodes(v reflect.Value, visit func(any)) {
	walkASTPruned(v, func(n any) bool { visit(n); return true })
}

// walkASTPruned is walkASTNodes where visit returning false skips the node's
// subtree.
func walkASTPruned(v reflect.Value, visit func(any) bool) {
	switch v.Kind() {
	case reflect.Interface:
		if !v.IsNil() {
			walkASTPruned(v.Elem(), visit)
		}
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		if !visit(v.Interface()) {
			return
		}
		walkASTPruned(v.Elem(), visit)
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walkASTPruned(v.Index(i), visit)
		}
	case reflect.Struct:
		if v.Type() == astPosType {
			return
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				walkASTPruned(v.Field(i), visit)
			}
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			walkASTPruned(v.MapIndex(k), visit)
		}
	}
}

// TopLevelAwait returns the first `await` evaluated by a module's top-level
// code (not inside a function), or nil. Krate evaluates module constants at
// build time, where a Promise cannot be awaited.
func TopLevelAwait(prog *ast.Program) *ast.AwaitExpr {
	if prog == nil {
		return nil
	}
	var found *ast.AwaitExpr
	walkASTPruned(reflect.ValueOf(prog.Body), func(n any) bool {
		if found != nil {
			return false
		}
		switch x := n.(type) {
		case *ast.AwaitExpr:
			found = x
			return false
		case *ast.ArrowFn, *ast.FnDecl, *ast.ClassDecl, *ast.ClassExpr:
			return false
		}
		return true
	})
	return found
}
