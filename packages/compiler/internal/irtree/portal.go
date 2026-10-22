package irtree

import (
	"reflect"
	"strconv"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
)

// Portals.
//
// `createPortal(children, container)` and `<Portal container>` lower to an
// inert anchor holding the portal content:
//
//	<template data-krate-portal="ID" data-krate-portal-target="#modal-root">...</template>
//
// The content is built in the component's scope (its bindings hydrate like any
// other), is not displayed by the server-rendered page (React does not render
// portals on the server), and is moved into the container by the runtime
// (packages/runtime/src/portal.ts) once the page's bindings are installed.
// Literal containers are encoded as a selector; any other container
// expression is registered from the component's hydration scope.

// portalCall matches `createPortal(children, container[, key])`.
func portalCall(expr ast.Expr) (children, container ast.Expr, ok bool) {
	call, isCall := expr.(*ast.CallExpr)
	if !isCall || len(call.Args) < 2 {
		return nil, nil, false
	}
	id, isID := call.Callee.(*ast.Identifier)
	if !isID || id.Name != "createPortal" {
		return nil, nil, false
	}
	return call.Args[0], call.Args[1], true
}

// portalSelector returns the CSS selector of a literal portal container:
// document.body ("body"), document.getElementById("x") ("#x") or
// document.querySelector("sel"). ok is false for any other expression.
func portalSelector(container ast.Expr) (string, bool) {
	if container == nil {
		return "body", true
	}
	if p, ok := container.(*ast.ParenExpr); ok {
		return portalSelector(p.Expr)
	}
	if mem, ok := container.(*ast.MemberExpr); ok && !mem.Computed {
		obj, _ := mem.Object.(*ast.Identifier)
		prop, _ := mem.Property.(*ast.Identifier)
		if obj != nil && prop != nil && obj.Name == "document" && prop.Name == "body" {
			return "body", true
		}
		return "", false
	}
	call, ok := container.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return "", false
	}
	mem, ok := call.Callee.(*ast.MemberExpr)
	if !ok || mem.Computed {
		return "", false
	}
	obj, _ := mem.Object.(*ast.Identifier)
	prop, _ := mem.Property.(*ast.Identifier)
	if obj == nil || prop == nil || obj.Name != "document" {
		return "", false
	}
	arg, ok := literalString(call.Args[0])
	if !ok || arg == "" {
		return "", false
	}
	switch prop.Name {
	case "getElementById":
		return "#" + cssIdent(arg), true
	case "querySelector":
		return arg, true
	}
	return "", false
}

// cssIdent escapes an element ID for use in an #id selector.
func cssIdent(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		isAlnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c >= 0x80
		if isAlnum && !(i == 0 && c >= '0' && c <= '9') {
			out = append(out, c)
			continue
		}
		out = append(out, '\\')
		out = append(out, strconv.FormatInt(int64(c), 16)...)
		out = append(out, ' ')
	}
	return string(out)
}

// buildPortal builds the anchor and content of a portal.
func (b *builder) buildPortal(content ast.Expr, container ast.Expr, pos ast.Pos, parentID string) []SlotNode {
	id := b.assignSlotID(b.nextDynamicSlot(parentID, "portal"))
	open := `<template data-krate-portal="` + escape.HTMLAttr(string(id)) + `"`
	if sel, ok := portalSelector(container); ok {
		if sel != "body" {
			open += ` data-krate-portal-target="` + escape.HTMLAttr(sel) + `"`
		}
	} else if js := generateExprJS(container, b.sigMap()); js != "" && b.currentNode != nil && b.currentNode.Tier == TierClient {
		b.currentNode.ExtraVars = append(b.currentNode.ExtraVars,
			"kratePortalTarget("+strconv.Quote(string(id))+",function(){return("+js+")})")
	} else {
		b.warnCompat(pos, diag.Code("KR8006"),
			"portal container `"+generateExprJS(container, nil)+"` cannot be resolved here; the content mounts into document.body",
			"pass document.body, document.getElementById(\"id\") or a container held by a client component")
	}
	var children []SlotNode
	switch c := content.(type) {
	case *ast.JSXFragment:
		children = b.buildFragmentSlots(c, string(id))
	default:
		children = b.exprChild(content, string(id), true)
	}
	out := make([]SlotNode, 0, len(children)+2)
	out = append(out, &StaticHTML{HTML: open + `>`})
	out = append(out, children...)
	out = append(out, &StaticHTML{HTML: `</template>`})
	return out
}

// containsPortal reports whether a component body renders a portal, which
// needs the IR path (the build-time evaluator cannot emit portal anchors).
func containsPortal(body []ast.Stmt) bool {
	found := false
	walkAST(reflect.ValueOf(body), func(n any) bool {
		if found {
			return false
		}
		if e, ok := n.(ast.Expr); ok {
			if _, _, ok := portalCall(e); ok {
				found = true
			}
		}
		if el, ok := n.(*ast.JSXElement); ok && el.Opening != nil && el.Opening.Name == "Portal" {
			found = true
		}
		return !found
	})
	return found
}

// walkAST visits every AST node pointer reachable from v (statements,
// expressions, JSX), depth first, while visit returns true.
func walkAST(v reflect.Value, visit func(any) bool) {
	switch v.Kind() {
	case reflect.Interface:
		if !v.IsNil() {
			walkAST(v.Elem(), visit)
		}
	case reflect.Pointer:
		if v.IsNil() || v.Elem().Kind() != reflect.Struct {
			return
		}
		if !visit(v.Interface()) {
			return
		}
		walkAST(v.Elem(), visit)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				walkAST(v.Field(i), visit)
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			walkAST(v.Index(i), visit)
		}
	}
}
