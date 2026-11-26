package bundler

import (
	"reflect"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// lowerInnerHTMLProps rewrites the Solid-style content props of an intrinsic
// element to the forms the rest of the pipeline already handles:
//
//	<div innerHTML={html} />   ->  <div dangerouslySetInnerHTML={{ __html: html }} />
//	<p textContent={text} />   ->  <p>{text}</p>
//
// Components keep both names as ordinary props. An element that also has
// children is an error, as it is for dangerouslySetInnerHTML in React.
func (c *reactCtx) lowerInnerHTMLProps(prog *ast.Program) {
	walkNodes(reflect.ValueOf(prog.Body), func(n any) {
		el, ok := n.(*ast.JSXElement)
		if !ok || el.Opening == nil || !isIntrinsicTag(el.Opening.Name) {
			return
		}
		for i, attr := range el.Opening.Attributes {
			if attr == nil || attr.Spread || attr.Value == nil || (attr.Name != "innerHTML" && attr.Name != "textContent") {
				continue
			}
			if hasMeaningfulChildren(el.Children) {
				c.report(attr.Position, diag.SeverityError, diag.CodeContentPropChildren,
					"<"+el.Opening.Name+"> has both "+attr.Name+" and children",
					"keep one: "+attr.Name+" replaces the element's content, so remove the children or the prop")
				return
			}
			if attr.Name == "innerHTML" {
				el.Opening.Attributes[i] = &ast.JSXAttr{
					Position: attr.Position,
					Name:     "dangerouslySetInnerHTML",
					Value: &ast.ObjectExpr{
						Position:   attr.Position,
						Properties: []*ast.ObjectProp{{Key: "__html", Value: attr.Value}},
					},
				}
				return
			}
			el.Opening.Attributes = append(el.Opening.Attributes[:i:i], el.Opening.Attributes[i+1:]...)
			el.Children = []ast.JSXChild{&ast.JSXExprContainer{Expression: attr.Value}}
			if el.Opening.SelfClosing {
				el.Opening.SelfClosing = false
				el.Closing = &ast.JSXClosing{Name: el.Opening.Name}
			}
			return
		}
	})
}

// isIntrinsicTag reports a lowercase element name such as div or my-element.
func isIntrinsicTag(name string) bool {
	return name != "" && name[0] >= 'a' && name[0] <= 'z' && !strings.Contains(name, ".")
}

// hasMeaningfulChildren reports children other than whitespace between lines.
func hasMeaningfulChildren(children []ast.JSXChild) bool {
	for _, ch := range children {
		if t, ok := ch.(*ast.JSXText); ok && strings.TrimSpace(t.Value) == "" &&
			(t.Value == "" || strings.ContainsAny(t.Value, "\n\r")) {
			continue
		}
		return true
	}
	return false
}
