package irtree

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
)

// React 19 document metadata.
//
// A <title>, <meta> or <link> rendered anywhere in a component is placed in
// the document <head>, like React 19 does, except inside <svg> (an SVG
// <title>), inside <Head> (already head content), with an itemProp attribute
// (microdata stays in place), and for <link rel="stylesheet"> without a
// precedence (React only hoists stylesheets it manages). Metadata inside a
// conditional branch or a list item stays in place for now: hoisting it would
// need the head to follow the branch at runtime. The hoisted element
// keeps its bindings: a reactive <title> updates document.title, a reactive
// <meta content> or <link href> updates the attribute. The elements are
// emitted through a MetaSlot named HoistMeta, which the emitter routes into
// the page head in tree order.

// HoistMeta is the MetaSlot component name of hoisted document metadata.
const HoistMeta = "hoist"

// TextContentAttr is the pseudo attribute of an AttrBinding that sets an
// element's text (a reactive <title>).
const TextContentAttr = "textContent"

// IsHoistableMetadata reports whether a lowercase <title>/<meta>/<link>
// element is hoisted into <head> (see the package comment above).
func IsHoistableMetadata(el *ast.JSXElement) bool {
	if el == nil || el.Opening == nil {
		return false
	}
	name := el.Opening.Name
	if name != "title" && name != "meta" && name != "link" {
		return false
	}
	var rel string
	precedence := false
	for _, attr := range el.Opening.Attributes {
		if attr == nil || attr.Spread {
			continue
		}
		switch attr.Name {
		case "itemProp", "itemprop":
			return false
		case "precedence":
			precedence = true
		case "rel":
			if s, ok := literalString(attr.Value); ok {
				rel = strings.ToLower(strings.TrimSpace(s))
			}
		}
	}
	if name == "link" && rel == "stylesheet" && !precedence {
		return false
	}
	return true
}

// hoistsMetadata reports whether el is hoisted at the current build position.
func (b *builder) hoistsMetadata(el *ast.JSXElement) bool {
	return b.svgDepth == 0 && b.metaDepth == 0 && b.branchDepth == 0 && IsHoistableMetadata(el)
}

// buildHoistedMetadata builds a hoisted <title>/<meta>/<link>.
func (b *builder) buildHoistedMetadata(el *ast.JSXElement, parentID string) []SlotNode {
	if el.Opening.Name == "title" {
		return []SlotNode{&MetaSlot{ComponentName: HoistMeta, Children: b.buildTitleSlots(el, parentID)}}
	}
	// The stylesheet ordering hint is consumed by the hoist, as in React.
	stripped := WithoutAttr(el, "precedence")
	return []SlotNode{&MetaSlot{ComponentName: HoistMeta, Children: b.buildStaticElementSlots(stripped, parentID)}}
}

// buildTitleSlots renders <title> as a single text value. A reactive title
// binds the element's text content: comment markers cannot live in <title>,
// whose content is plain text.
func (b *builder) buildTitleSlots(el *ast.JSXElement, parentID string) []SlotNode {
	expr := titleTextExpr(el.Children)
	if expr == nil {
		return []SlotNode{&StaticHTML{HTML: "<title></title>"}}
	}
	text, _ := childText(b.evalValue(expr))
	if !b.referencesSignal(expr) {
		return []SlotNode{&StaticHTML{HTML: "<title>" + escape.Text(text) + "</title>"}}
	}
	id := b.assignSlotID(joinSlotID(parentID, b.nextElementTag("title", parentID)))
	b.pendingAttrs = append(b.pendingAttrs, AttrBinding{
		ElementSlotID: id,
		AttrName:      TextContentAttr,
		ExprSource:    generateExprJS(expr, b.sigMap()),
		Initial:       text,
		InitialExpr:   expr,
	})
	return []SlotNode{&StaticHTML{HTML: `<title data-k="k:` + escape.HTMLAttr(string(id)) + `">` + escape.Text(text) + "</title>"}}
}

// titleTextExpr joins a <title>'s children into one string expression, or nil
// when it has none.
func titleTextExpr(children []ast.JSXChild) ast.Expr {
	var parts []ast.Expr
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXText:
			if c.Value != "" {
				parts = append(parts, &ast.Literal{Kind: ast.StringLit, Value: c.Value})
			}
		case *ast.JSXExprContainer:
			if c.Expression != nil {
				parts = append(parts, c.Expression)
			}
		}
	}
	switch len(parts) {
	case 0:
		return nil
	case 1:
		if _, ok := parts[0].(*ast.Literal); ok {
			return parts[0]
		}
		return &ast.BinaryExpr{Left: &ast.Literal{Kind: ast.StringLit, Value: ""}, Op: "+", Right: parts[0]}
	}
	var out ast.Expr = &ast.Literal{Kind: ast.StringLit, Value: ""}
	for _, p := range parts {
		out = &ast.BinaryExpr{Left: out, Op: "+", Right: p}
	}
	return out
}

// WithoutAttr returns el without the named attribute (a shallow copy).
func WithoutAttr(el *ast.JSXElement, name string) *ast.JSXElement {
	keep := make([]*ast.JSXAttr, 0, len(el.Opening.Attributes))
	for _, attr := range el.Opening.Attributes {
		if attr != nil && !attr.Spread && attr.Name == name {
			continue
		}
		keep = append(keep, attr)
	}
	if len(keep) == len(el.Opening.Attributes) {
		return el
	}
	opening := *el.Opening
	opening.Attributes = keep
	cloned := *el
	cloned.Opening = &opening
	return &cloned
}
