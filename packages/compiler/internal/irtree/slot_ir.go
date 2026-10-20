package irtree

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// buildSlotSlots lowers Krate's <Slot> primitive (shadcn's `asChild`): render the
// single child element with the Slot's props merged onto it. Supports an
// explicit literal child, `{children}` / `{props.children}`, and the common
// `<Slot {...props} />` form where children arrive through the spread.
func (b *builder) buildSlotSlots(el *ast.JSXElement, parentID string) []SlotNode {
	child := b.slotChildElement(el)
	if child == nil {
		return b.buildFragmentSlots(&ast.JSXFragment{Position: el.Position, Children: el.Children}, parentID)
	}
	return b.buildSlotNodes(b.mergeSlotElement(el, child), parentID)
}

func (b *builder) slotChildElement(el *ast.JSXElement) *ast.JSXElement {
	if child := singleSlotElement(el.Children); child != nil {
		return child
	}
	if len(el.Children) == 0 || isChildrenExprChildren(el.Children) {
		return singleSlotElement(b.callSiteChildren)
	}
	return nil
}

// singleSlotElement returns the sole element child, ignoring whitespace text.
func singleSlotElement(children []ast.JSXChild) *ast.JSXElement {
	var out *ast.JSXElement
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXElementChild:
			if out != nil {
				return nil
			}
			out = c.Element
		case *ast.JSXText:
			if strings.TrimSpace(c.Value) != "" {
				return nil
			}
		case *ast.JSXExprContainer:
			return nil
		case *ast.JSXFragmentChild:
			return nil
		}
	}
	return out
}

func isChildrenExprChildren(children []ast.JSXChild) bool {
	saw := false
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXText:
			if strings.TrimSpace(c.Value) != "" {
				return false
			}
		case *ast.JSXExprContainer:
			if saw {
				return false
			}
			saw = true
			switch e := c.Expression.(type) {
			case *ast.Identifier:
				if e.Name != "children" {
					return false
				}
			case *ast.MemberExpr:
				id, ok := e.Object.(*ast.Identifier)
				if !ok || id.Name != "props" {
					return false
				}
				prop, ok := e.Property.(*ast.Identifier)
				if !ok || prop.Name != "children" {
					return false
				}
			default:
				return false
			}
		default:
			return false
		}
	}
	return saw
}

// mergeSlotElement returns a copy of child with the Slot's attributes merged
// on. The child wins on duplicates; class values are concatenated.
func (b *builder) mergeSlotElement(slot, child *ast.JSXElement) *ast.JSXElement {
	cloned := *child
	if child.Opening == nil {
		return &cloned
	}
	opening := *child.Opening
	cloned.Opening = &opening

	slotAttrs, _ := b.expandSpreadAttrs(slot.Opening.Attributes)
	childNames := make(map[string]bool, len(opening.Attributes))
	for _, attr := range opening.Attributes {
		if attr != nil && !attr.Spread {
			childNames[attr.Name] = true
		}
	}

	merged := make([]*ast.JSXAttr, 0, len(opening.Attributes)+len(slotAttrs)+1)
	for _, attr := range slotAttrs {
		if attr == nil || attr.Spread || attr.Name == "" {
			continue
		}
		if attr.Name == "key" || attr.Name == "children" || attr.Name == "class" || attr.Name == "className" {
			continue
		}
		if childNames[attr.Name] {
			continue
		}
		cp := *attr
		merged = append(merged, &cp)
	}
	for _, attr := range opening.Attributes {
		if attr == nil {
			continue
		}
		if !attr.Spread && (attr.Name == "class" || attr.Name == "className") {
			continue
		}
		merged = append(merged, attr)
	}
	if classAttr := b.mergeSlotClass(slot, child, slotAttrs); classAttr != nil {
		merged = append(merged, classAttr)
	}
	opening.Attributes = merged
	return &cloned
}

func (b *builder) mergeSlotClass(slot, child *ast.JSXElement, slotAttrs []*ast.JSXAttr) *ast.JSXAttr {
	slotAttr := findAttr(slotAttrs, "class", "className")
	if slotAttr == nil {
		return nil
	}
	childAttr := findAttr(child.Opening.Attributes, "class", "className")
	if childAttr == nil {
		cp := *slotAttr
		cp.Name = "class"
		return &cp
	}
	slotValue := b.evalAttrValue(slotAttr.Value)
	childValue := b.evalAttrValue(childAttr.Value)
	if slotValue != "" && childValue != "" {
		return &ast.JSXAttr{
			Name:  "class",
			Value: &ast.Literal{Kind: ast.StringLit, Value: slotValue + " " + childValue},
		}
	}
	return &ast.JSXAttr{
		Name: "class",
		Value: &ast.BinaryExpr{
			Op: "+",
			Left: &ast.BinaryExpr{
				Op:    "+",
				Left:  slotAttr.Value,
				Right: &ast.Literal{Kind: ast.StringLit, Value: " "},
			},
			Right: childAttr.Value,
		},
	}
}

func findAttr(attrs []*ast.JSXAttr, names ...string) *ast.JSXAttr {
	for _, attr := range attrs {
		if attr == nil || attr.Spread {
			continue
		}
		for _, name := range names {
			if attr.Name == name {
				return attr
			}
		}
	}
	return nil
}
