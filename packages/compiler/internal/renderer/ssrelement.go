package renderer

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/htmlspec"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// renderElement serializes an intrinsic (lowercase) element during SSREval.
// Attribute, text and void-element rules come from htmlspec through the
// shared irtree helpers, so this path matches the IR tree builder exactly.
func (e *SSREval) renderElement(el *ast.JSXElement, name string) string {
	e.addMarkupErrs(irtree.CheckElementMarkup("", el))

	var b strings.Builder
	b.WriteByte('<')
	b.WriteString(name)

	// dangerouslySetInnerHTML={{__html: "..."}} injects raw, pre-rendered HTML
	// (e.g. build-time markdown). It is never emitted as an attribute.
	attrs, expanded := e.expandSSRSpreadAttrs(el.Opening.Attributes)
	var rawInnerHTML string
	hasRawInnerHTML := false
	for _, attr := range attrs {
		if attr.Spread || attr.Name != "dangerouslySetInnerHTML" || attr.Value == nil {
			continue
		}
		if raw, ok := e.evalInnerHTMLValue(attr.Value); ok {
			rawInnerHTML = raw
			hasRawInnerHTML = true
		}
	}

	// <select value>: options below are marked selected from the selection.
	if name == "select" {
		sel, _ := irtree.SelectSelection(&ast.JSXElement{Opening: &ast.JSXOpening{Attributes: attrs}}, e.eval)
		e.selectStack = append(e.selectStack, sel)
		defer func() { e.selectStack = e.selectStack[:len(e.selectStack)-1] }()
	}
	inSelect := name == "option" && len(e.selectStack) > 0 && e.selectStack[len(e.selectStack)-1] != nil

	for _, attr := range attrs {
		if attr.Spread || htmlspec.IsReservedProp(attr.Name) || isEventAttr(attr.Name) {
			continue
		}
		if htmlspec.ContentValue(name, attr.Name) || htmlspec.SelectValueProp(name, attr.Name) {
			continue
		}
		if inSelect && attr.Name == "selected" {
			continue
		}
		b.WriteString(irtree.RenderAttr(name, attr, e.eval))
	}
	if inSelect && irtree.OptionSelected(e.selectStack[len(e.selectStack)-1], irtree.OptionValue(el, e.eval)) {
		b.WriteString(" selected")
	}

	if htmlspec.IsVoid(name) {
		b.WriteString(" />")
		return b.String()
	}

	if el.Opening.SelfClosing && !hasRawInnerHTML {
		// A self-closing element may still receive children through a forwarded
		// rest spread (`<Comp {...props}/>`), since React's props object carries
		// `children`. Inject the frame's children when a spread was expanded and
		// they are present.
		content := ""
		if expanded {
			if ch, ok := e.bindings["children"]; ok && ch != "" {
				content = ch
			}
		}
		if name == "textarea" {
			if v, ok := irtree.TextareaValue(&ast.JSXElement{Opening: &ast.JSXOpening{Attributes: attrs}}, e.eval); ok {
				content = irtree.PreserveLeadingNewline(name, v)
			}
		}
		b.WriteByte('>')
		b.WriteString(content)
		b.WriteString("</")
		b.WriteString(name)
		b.WriteByte('>')
		return b.String()
	}

	b.WriteByte('>')
	if hasRawInnerHTML {
		// The dangerouslySetInnerHTML value is pre-rendered markup - inject
		// it verbatim, ignoring children.
		b.WriteString(rawInnerHTML)
	} else {
		children := el.Children
		// React's props object carries `children`, so `<Comp {...props}/>`
		// forwards the call-site children. When this element has no explicit
		// children but a rest spread was expanded, inject the component
		// frame's children binding.
		if len(children) == 0 && expanded {
			if ch, ok := e.bindings["children"]; ok && ch != "" {
				b.WriteString(ch)
				b.WriteString("</")
				b.WriteString(name)
				b.WriteByte('>')
				return b.String()
			}
		}
		var content string
		if v, ok := irtree.TextareaValue(&ast.JSXElement{Opening: &ast.JSXOpening{Attributes: attrs}}, e.eval); ok && name == "textarea" {
			content = v
		} else {
			content = e.renderElementChildren(name, children)
		}
		b.WriteString(irtree.PreserveLeadingNewline(name, content))
	}
	b.WriteString("</")
	b.WriteString(name)
	b.WriteByte('>')
	return b.String()
}

// renderElementChildren renders the children of an intrinsic element. Text is
// escaped; script/style children are raw text with the closing tag sequence
// neutralized.
func (e *SSREval) renderElementChildren(tag string, children []ast.JSXChild) string {
	var b strings.Builder
	raw := htmlspec.IsRawText(tag)
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXText:
			if raw {
				b.WriteString(htmlspec.RawText(tag, c.Value))
			} else {
				b.WriteString(escape.Text(c.Value))
			}
		case *ast.JSXExprContainer:
			if raw {
				b.WriteString(htmlspec.RawText(tag, stripArraySep(e.eval(c.Expression))))
			} else {
				b.WriteString(e.escapeContainerValue(c.Expression))
			}
		case *ast.JSXElementChild:
			b.WriteString(e.evalJSX(c.Element))
		case *ast.JSXFragmentChild:
			b.WriteString(e.evalFragment(c.Fragment))
		}
	}
	return b.String()
}

// isEventAttr reports JSX event handler props (onClick, onChange, ...), which
// never serialize to HTML.
func isEventAttr(name string) bool {
	return len(name) > 2 && name[0] == 'o' && name[1] == 'n' && name[2] >= 'A' && name[2] <= 'Z'
}

// addMarkupErrs records markup diagnostics once per evaluator.
func (e *SSREval) addMarkupErrs(errs []error) {
	for _, err := range errs {
		dup := false
		for _, have := range e.errs {
			if have.Error() == err.Error() {
				dup = true
				break
			}
		}
		if !dup {
			e.errs = append(e.errs, err)
		}
	}
}
