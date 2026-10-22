package irtree

import (
	"sort"
	"strconv"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// Context seen by call-site children.
//
// React resolves a context read against the providers above the reader in
// the rendered tree. Call-site children (`<Level><Leaf/></Level>`) are written
// in the caller but rendered where the component places `{props.children}`,
// so providers the component wraps around that placeholder apply to them.
// Krate builds call-site children in the caller's scope; the overlay records
// the providers active at the placeholder and is applied when the children are
// built. A constant provider value folds; a dynamic one is published by the
// component's hydration scope under a registry key and read back lazily by the
// children (`__krate_props["<id>#ctx:<Ctx>"]()`), since the value expression
// references the component's own locals.

// contextOverlay is the provider state at a component's children placeholder.
type contextOverlay struct {
	static   map[string]jsvalue.Value
	dynamic  map[string]ast.Expr
	initials map[string]jsvalue.Value
}

// contextRegistryKey names the published getter of a dynamic provider value.
func contextRegistryKey(id SlotID, ctx string) string {
	return string(id) + "#ctx:" + ctx
}

// contextRegistryRead is the expression reading a published provider value.
func contextRegistryRead(key string) ast.Expr {
	return &ast.CallExpr{Callee: &ast.MemberExpr{
		Object:   &ast.Identifier{Name: "__krate_props"},
		Property: &ast.Literal{Kind: ast.StringLit, Value: key},
		Computed: true,
	}}
}

// enterContextEntry records the context state a component's return starts
// with, so providers the component adds can be told apart from inherited ones.
func (b *builder) enterContextEntry() {
	b.entryContextValues = b.contextValues
	b.entryContextMarkers = b.activeContextMarkers()
}

// recordChildrenContext captures the providers active at a `{props.children}`
// placeholder of the component being built. Only the first placeholder is
// recorded (a component rendering its children twice sees one context).
func (b *builder) recordChildrenContext() {
	node := b.currentNode
	if node == nil || node.childrenContext != nil || b.locateOnly > 0 {
		return
	}
	ov := &contextOverlay{}
	ctxs := make([]string, 0, len(b.contextValues))
	for ctx := range b.contextValues {
		ctxs = append(ctxs, ctx)
	}
	sort.Strings(ctxs)
	for _, ctx := range ctxs {
		expr := b.contextValues[ctx]
		if b.entryContextValues[ctx] == expr {
			continue
		}
		key := contextRegistryKey(node.ID, ctx)
		js := generateExprJS(expr, b.sigMap())
		if js == "" {
			continue
		}
		node.ExtraVars = append(node.ExtraVars, "__krate_props["+strconv.Quote(key)+"]=function(){return("+js+")}")
		if ov.dynamic == nil {
			ov.dynamic = make(map[string]ast.Expr)
			ov.initials = make(map[string]jsvalue.Value)
		}
		ov.dynamic[ctx] = contextRegistryRead(key)
		if v := evalConstValue(expr, b.sigMap(), b.localProps); !v.ContainsUnknown() {
			ov.initials[ctx] = v
		}
	}
	for ctx, v := range b.activeContextMarkers() {
		if _, dyn := b.contextValues[ctx]; dyn {
			continue
		}
		if ov.static == nil {
			ov.static = make(map[string]jsvalue.Value)
		}
		ov.static[ctx] = v
	}
	node.childrenContext = ov
}

// applyChildrenContext installs a component's children overlay in the
// caller's scope while its call-site children are built. The returned
// function restores the caller's state.
func (b *builder) applyChildrenContext(ov *contextOverlay) func() {
	if ov == nil || (len(ov.static) == 0 && len(ov.dynamic) == 0) {
		return func() {}
	}
	savedValues, savedProps := b.contextValues, b.localProps
	values := cloneExprMap(savedValues)
	props := cloneStringMap(savedProps)
	for ctx, v := range ov.static {
		delete(values, ctx)
		props[contextMarkerPrefix+ctx] = v
	}
	for ctx, expr := range ov.dynamic {
		values[ctx] = expr
		if v, ok := ov.initials[ctx]; ok {
			props[contextMarkerPrefix+ctx] = v
		}
	}
	b.contextValues, b.localProps = values, props
	return func() { b.contextValues, b.localProps = savedValues, savedProps }
}

// callerExpansion drops the innermost expansion frame while a component's
// call-site children are built: they belong to the caller's JSX, so a child
// of the same component (`<Level><Level/></Level>`) is not recursion.
func (b *builder) callerExpansion() func() {
	saved := b.expansion
	if n := len(saved); n > 0 {
		b.expansion = saved[: n-1 : n-1]
	}
	return func() { b.expansion = saved }
}
