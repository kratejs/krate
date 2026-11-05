package irtree

import (
	"github.com/kratejs/krate/packages/compiler/ast"
)

// ActivityTag is the tag the React rewrite gives <Activity>.
const ActivityTag = "KrateActivity"

// buildActivitySlot builds <KrateActivity mode={...}>children</KrateActivity>
// as a conditional slot with Activity set: its children are always in the
// page, and the test decides whether the wrapper shows them. The test is
// `mode !== "hidden"`, so a missing or unknown mode shows the children.
func (b *builder) buildActivitySlot(el *ast.JSXElement, parentID string) []SlotNode {
	var mode ast.Expr
	for _, attr := range el.Opening.Attributes {
		if attr != nil && !attr.Spread && attr.Name == "mode" && attr.Value != nil {
			mode = attr.Value
		}
	}
	var test ast.Expr = &ast.Literal{Kind: ast.BoolLit, Value: "true"}
	if mode != nil {
		test = &ast.BinaryExpr{
			Left:  &ast.ParenExpr{Expr: mode},
			Op:    "!==",
			Right: &ast.Literal{Kind: ast.StringLit, Value: "hidden"},
		}
	}
	id := b.assignSlotID(b.nextDynamicSlot(parentID, "act"))
	slot := &ConditionalSlot{
		ID:            id,
		TestJS:        generateExprJS(test, b.sigMap()),
		InitialActive: b.evalValue(test).ToBoolean(),
		Signals:       b.collectSignalReads(test),
		SVG:           b.svgDepth > 0,
		Activity:      true,
	}
	b.branchDepth++
	slot.Consequent = b.buildSlotNodes(&ast.JSXFragment{Position: el.Position, Children: el.Children}, string(id)+".c")
	b.branchDepth--
	if len(slot.Consequent) == 0 {
		slot.Consequent = []SlotNode{&StaticHTML{HTML: ""}}
	}
	return []SlotNode{slot}
}
