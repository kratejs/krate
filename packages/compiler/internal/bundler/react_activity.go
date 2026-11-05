package bundler

import (
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// React 19.2 <Activity> and <ViewTransition>.
//
//   - `<Activity mode="visible" | "hidden">` becomes the built-in
//     `<KrateActivity>` slot: the children are always in the page, inside a
//     `display:contents` wrapper that is hidden when the mode is "hidden". The
//     components inside keep their state while hidden; their effects are cleaned
//     up on hiding and set up again on showing (see kbindActivity in the
//     runtime).
//   - `<ViewTransition name default>` becomes a fragment whose first element
//     child carries `data-view-transition` (the attribute the router turns
//     into `view-transition-name` around navigations) and, for a custom
//     `default` class, `data-view-transition-class`. Children that are not a
//     host element are wrapped in a `<div>` so there is a box to name.

// activityTag is the built-in tag the IR builder turns into an activity slot.
const activityTag = "KrateActivity"

// activitySupported and viewTransitionSupported list the props that have an
// effect; the others are reported once per element.
var (
	viewTransitionProps = map[string]bool{"name": true, "default": true, "key": true, "children": true}
	viewTransitionCBs   = map[string]bool{"onEnter": true, "onExit": true, "onUpdate": true, "onShare": true}
	viewTransitionEvts  = map[string]bool{"enter": true, "exit": true, "update": true, "share": true}
)

// lowerActivity renames <Activity> to the built-in tag and checks its props.
func (c *reactCtx) lowerActivity(el *ast.JSXElement) {
	el.Opening.Name = activityTag
	if el.Closing != nil {
		el.Closing.Name = activityTag
	}
	for _, attr := range el.Opening.Attributes {
		if attr == nil || attr.Spread {
			continue
		}
		switch attr.Name {
		case "mode", "key":
			if lit, ok := attr.Value.(*ast.Literal); ok && attr.Name == "mode" && lit.Value != "visible" && lit.Value != "hidden" {
				c.report(attr.Position, diag.SeverityWarning, diag.CodeReact19Prop,
					"<Activity mode=\""+lit.Value+"\"> is not a mode: use \"visible\" or \"hidden\"",
					"any other value is treated as visible")
			}
		default:
			c.report(attr.Position, diag.SeverityWarning, diag.CodeReact19Prop,
				"<Activity> has no `"+attr.Name+"` prop; it is ignored",
				"<Activity> takes mode=\"visible\" or mode=\"hidden\" and children")
		}
	}
}

// lowerViewTransition applies a <ViewTransition> to its first element child
// and returns the children as a fragment.
func (c *reactCtx) lowerViewTransition(el *ast.JSXElement, sc *scope) ast.Expr {
	var name, class ast.Expr
	none := false
	for _, attr := range el.Opening.Attributes {
		if attr == nil {
			continue
		}
		if attr.Spread {
			c.report(attr.Position, diag.SeverityWarning, diag.CodeReact19Prop,
				"<ViewTransition> props cannot be spread; they are ignored", "write name and default as plain props")
			continue
		}
		switch {
		case attr.Name == "name":
			name = attr.Value
		case attr.Name == "default":
			lit, ok := attr.Value.(*ast.Literal)
			switch {
			case ok && lit.Value == "none":
				none = true
			case ok && lit.Value == "auto":
			case ok:
				class = lit
			default:
				c.report(attr.Position, diag.SeverityWarning, diag.CodeReact19Prop,
					"<ViewTransition default> must be a string literal to take effect; it is ignored",
					"write default=\"slow-fade\" and style ::view-transition-group(*.slow-fade)")
			}
		case viewTransitionProps[attr.Name]:
		case viewTransitionCBs[attr.Name]:
			c.report(attr.Position, diag.SeverityWarning, diag.CodeReact19Prop,
				"<ViewTransition "+attr.Name+"> is never called: Krate does not report transition events",
				"use the View Transitions API directly with document.startViewTransition, or listen for navigation events")
		case viewTransitionEvts[attr.Name]:
			lit, ok := attr.Value.(*ast.Literal)
			if !ok || (lit.Value != "auto" && lit.Value != "none") {
				c.report(attr.Position, diag.SeverityWarning, diag.CodeReact19Prop,
					"<ViewTransition "+attr.Name+"> classes are not applied: Krate sets one view-transition-class from `default`",
					"put the class in default=\"...\" and select the event in CSS with ::view-transition-old(*.class) / ::view-transition-new(*.class)")
			}
		default:
			c.report(attr.Position, diag.SeverityWarning, diag.CodeReact19Prop,
				"<ViewTransition> has no `"+attr.Name+"` prop; it is ignored", "")
		}
	}
	if none {
		return c.lowerFragment(el, sc)
	}
	if name == nil {
		c.vtSeq++
		name = &ast.Literal{Kind: ast.StringLit, Value: "krate-vt-" + hashBytes([]byte(c.file))[:6] + "-" + strconv.Itoa(c.vtSeq)}
	}
	attrs := []*ast.JSXAttr{{Position: el.Position, Name: "data-view-transition", Value: name}}
	if class != nil {
		attrs = append(attrs, &ast.JSXAttr{Position: el.Position, Name: "data-view-transition-class", Value: class})
	}
	hosts := 0
	named := false
	hasContent := false
	for _, child := range el.Children {
		switch ch := child.(type) {
		case *ast.JSXElementChild:
			hasContent = true
			if ch.Element == nil || ch.Element.Opening == nil {
				continue
			}
			tag := ch.Element.Opening.Name
			if tag == "" || tag[0] < 'a' || tag[0] > 'z' || strings.Contains(tag, ".") {
				continue
			}
			hosts++
			if !named {
				named = true
				ch.Element.Opening.Attributes = append(ch.Element.Opening.Attributes, attrs...)
			}
		case *ast.JSXText:
			if strings.TrimSpace(ch.Value) != "" {
				hasContent = true
			}
		default:
			hasContent = true
		}
	}
	if hosts > 1 {
		c.report(el.Position, diag.SeverityWarning, diag.CodeReact19Prop,
			"<ViewTransition> has more than one element child: only the first is named",
			"wrap each element in its own <ViewTransition name=\"...\">")
	}
	if !named && hasContent {
		wrapper := &ast.JSXElement{
			Position: el.Position,
			Opening:  &ast.JSXOpening{Name: "div", Attributes: attrs},
			Children: el.Children,
			Closing:  &ast.JSXClosing{Name: "div"},
		}
		el.Children = []ast.JSXChild{&ast.JSXElementChild{Element: wrapper}}
	}
	return c.lowerFragment(el, sc)
}
