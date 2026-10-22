package irtree

import (
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// Build-time keys, keyed remounts and list diagnostics.

// foldListKey folds a key expression to its string form (React compares keys
// as strings). Nullish and unknown keys report false: the row is marked with
// its index and hydration adopts it by position.
func (b *builder) foldListKey(k ast.Expr) (string, bool) {
	v := b.evalValue(k)
	switch v.Kind() {
	case jsvalue.KindString, jsvalue.KindNumber, jsvalue.KindBool, jsvalue.KindBigInt:
		return v.ToString(), true
	}
	return "", false
}

// warnCodeAt records a coded warning once per source position.
func (b *builder) warnCodeAt(pos ast.Pos, code diag.Code, msg, hint string) {
	if b.locateOnly > 0 {
		return
	}
	d := b.diagAt(pos, diag.SeverityWarning, msg, hint).WithCode(code)
	for _, w := range b.warnings {
		if w.Code == d.Code && w.File == d.File && w.Line == d.Line && w.Col == d.Col {
			return
		}
	}
	b.warnings = append(b.warnings, d)
}

func (b *builder) warnListDuplicateKey(keyExpr ast.Expr, key string) {
	b.warnCodeAt(keyExpr.Pos(), diag.CodeListDuplicateKey,
		"encountered two children with the same key \""+key+"\"",
		"keys must be unique among the items of a list; use an id that differs per item (the runtime keeps the first occurrence on its row and renders the duplicate separately)")
}

// checkListKeys reports React's missing-key warning for lists that can change
// at runtime (their array reads a signal or is only known at runtime). Lists
// over build-time constants never reorder, so they are not reported.
func (b *builder) checkListKeys(call *ast.CallExpr, plan *listPlan) {
	arrow, ok := call.Args[0].(*ast.ArrowFn)
	if !ok {
		return
	}
	ret, _, ok := splitCallbackBody(arrow)
	if !ok {
		return
	}
	mem := call.Callee.(*ast.MemberExpr)
	dynamic := b.referencesSignal(mem.Object) || b.resolveArrayLiteralExpr(call) == nil
	if !dynamic {
		return
	}
	if plan.KeyHoles {
		b.warnCodeAt(ret.Pos(), diag.CodeListPartialKey,
			"this list callback returns a keyed element on some paths and an unkeyed one on others",
			"put the same key on every element the callback returns, e.g. key={item.id} on both branches")
		return
	}
	if plan.ReturnsElement && plan.KeyJS == "" {
		b.warnCodeAt(ret.Pos(), diag.CodeListMissingKey,
			"each child in a list should have a unique \"key\" prop",
			"add a stable key from the item, e.g. <li key={item.id}>; without one, rows are matched by index and their state (inputs, focus, component state) stays with the position when items are reordered, inserted or removed")
	}
}

// buildKeyedSlot lowers an element whose key reads reactive state, such as
// <Profile key={userId()} />, to a one-row keyed list: when the key changes
// the old instance unmounts (cleanups run) and a fresh one mounts with fresh
// state, React's "reset state with a key" pattern.
func (b *builder) buildKeyedSlot(el *ast.JSXElement, key ast.Expr, parentID string) []SlotNode {
	id := b.assignSlotID(b.nextDynamicSlot(parentID, "list"))
	stripped := elementWithoutKey(el)
	itemKey := "0"
	if folded, ok := b.foldListKey(key); ok {
		itemKey = "$" + folded
	}
	mark := b.markRowBindings()
	contents := b.buildSlotNodes(stripped, string(joinSlotIDKey(id, "0")))
	b.tagRowBindings(mark, id)
	if len(contents) == 0 {
		contents = []SlotNode{&StaticHTML{HTML: ""}}
	}
	keyJS := generateExprJS(key, nil)
	render := renderArrowFn(&ast.ArrowFn{Expression: true, Body: []ast.Stmt{&ast.ReturnStmt{Value: reactifyElement(stripped, true)}}}, nil)
	return []SlotNode{&ListSlot{
		ID:         id,
		ExprSource: "[" + keyJS + "].map(()=>" + generateExprJS(stripped, b.sigMap()) + ")",
		Items:      []*ListItem{{Key: itemKey, Contents: contents}},
		Keyed:      true,
		Signals:    b.collectSignalReads(el),
		Components: collectListComponents(el),
		Bind:       &ListBinding{Source: "[" + keyJS + "]", Key: "(k)=>k", Render: render, Accessor: true},
	}}
}

// elementWithoutKey returns a shallow copy of el without its key attribute.
func elementWithoutKey(el *ast.JSXElement) *ast.JSXElement {
	opening := *el.Opening
	opening.Attributes = nil
	for _, attr := range el.Opening.Attributes {
		if attr != nil && !attr.Spread && attr.Name == "key" {
			continue
		}
		opening.Attributes = append(opening.Attributes, attr)
	}
	cp := *el
	cp.Opening = &opening
	return &cp
}

// rowMark records how many element bindings the current component had before
// a list row was built.
type rowMark struct{ handlers, attrs, refs int }

func (b *builder) markRowBindings() rowMark {
	return rowMark{len(b.pendingHandlers), len(b.pendingAttrs), len(b.pendingRefs)}
}

// tagRowBindings tags the element bindings created while building a row with
// the list slot ID. When that list is bound by the list runtime, the row
// render binds its own elements, so the emitter drops these.
func (b *builder) tagRowBindings(m rowMark, list SlotID) {
	for i := m.handlers; i < len(b.pendingHandlers); i++ {
		b.pendingHandlers[i].InLists = append(b.pendingHandlers[i].InLists, list)
	}
	for i := m.attrs; i < len(b.pendingAttrs); i++ {
		b.pendingAttrs[i].InLists = append(b.pendingAttrs[i].InLists, list)
	}
	for i := m.refs; i < len(b.pendingRefs); i++ {
		b.pendingRefs[i].InLists = append(b.pendingRefs[i].InLists, list)
	}
}

// listBindingSources returns the JS of the runtime list bindings among slots
// (including lists inside conditional branches), for scope analysis.
func listBindingSources(slots []SlotNode) []string {
	var out []string
	for _, s := range slots {
		switch v := s.(type) {
		case *ListSlot:
			out = append(out, v.ExprSource)
			if v.Bind != nil {
				out = append(out, v.Bind.Source, v.Bind.Key, v.Bind.Render)
			}
		case *ConditionalSlot:
			out = append(out, listBindingSources(v.Consequent)...)
			out = append(out, listBindingSources(v.Alternate)...)
		}
	}
	return out
}
